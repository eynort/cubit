// Cubit: bot para Minecraft Java 26.2 (protocolo 776) en un servidor offline
// cualquiera, solo con lo vanilla (el servidor, el nombre y los dueños: config.go).
//
// La conexión y el bucle de paquetes viven aquí; los eventos se implementan en events.go.
//
//	go run . [--name <nombre>] [--owner <jugador>] <host[:puerto]>
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/google/uuid"
	"io"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/msg"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/playerlist"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"
	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/data/version"
	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"

	"cubit/automation"
	"cubit/combat"
	"cubit/internal/hub"
	"cubit/internal/rcon"
	"cubit/pathing"
	"cubit/safety"
)

var hubFlag = flag.String("hub", "", "modo hub: el panel en esta dirección (ej: :8080), siempre encendido, para varios bots (cada uno en bots/<id>/); los bots se crean y se arrancan desde el panel")

var (
	dataFlag = flag.String("data", os.Getenv("CUBIT_DATA"), "carpeta de datos (hub.json, bots/…, ajustes): se crea si no existe y el programa trabaja en ella; por defecto, la carpeta actual (también CUBIT_DATA)")
	openFlag = flag.Bool("open", false, "con --hub: abre el panel en el navegador; si ya hay un hub en esa dirección, solo lo abre")
)

// Bot agrupa todo lo que los eventos necesitan para actuar.
type Bot struct {
	Client  *bot.Client
	Player  *basic.Player
	Chat    *msg.Manager
	Players *playerlist.PlayerList

	World     *world.World              // bloques cargados
	Entities  *entities.Manager         // entidades a la vista
	Screens   *screen.Manager           // inventario
	Ctl       *control.Controller       // el tick del jugador: teclas, físicas, movimiento
	Walker    *pathing.Walker           // caminar / seguir con pathfinding (pathing/)
	Hands     *act.Hands                // golpear, comer, picar, colocar
	Combat    *combat.Fighter           // módulo de pelea
	Builder   *pathing.HandsBuilder     // pone bloques para el pathfinding
	Tasks     *automation.Runner        // las tareas de automatización (talar…)
	Lava      *safety.Lava              // el reflejo de la lava (manda sobre todo)
	Fall      *safety.Fall              // el del cubo de agua en una caída (después de la lava)
	Fire      *safety.Fire              // el de apagarse con el cubo de agua
	lastDeath atomic.Pointer[deathSpot] // dónde murió llevando cosas (para volver a recogerlas)
	lastCarry atomic.Int64              // la última vez (UnixNano) que llevaba algo en el inventario
	work      workState                 // el trabajo largo en curso (para retomarlo tras morir)

	noRetry atomic.Bool // no volver a entrar (el login falló)
	resume  *carryOver  // lo que había que retomar de la conexión anterior (reconnect.go)

	connectedAt time.Time

	lastPos     atomic.Pointer[basic.Position] // posición al final del último tick
	lastTick    atomic.Pointer[tickInfo]       // qué hacía el bot en el último tick
	corrections atomic.Int64                   // correcciones de posición del servidor
	lastAttrs   string                         // los atributos de la última línea ATTR (--logs)
	lastInv     map[string]int                 // el inventario de la última línea INV (--logs)

	ctx  context.Context // vive mientras el bot está conectado
	quit func()          // desconecta y sale (orden "quit")
}

func main() {
	flag.Parse()
	if *genDocsFlag != "" {
		if err := genDocs(*genDocsFlag); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *dataFlag != "" {
		if err := os.MkdirAll(*dataFlag, 0o755); err != nil {
			log.Fatal("--data: ", err)
		}
		if err := os.Chdir(*dataFlag); err != nil { // (todo lo demás usa rutas relativas a la carpeta de datos)
			log.Fatal("--data: ", err)
		}
	}
	if *hubFlag != "" {
		hub.Run(*hubFlag, hub.Options{DefaultOwner: *ownerFlag, Open: *openFlag}) // (el panel para varios bots; no vuelve hasta Ctrl-C)
		return
	}
	if *demoFlag {
		runDemo() // (pone el servidor y la cuenta de la demo)
	}
	if err := loadConfig(); err != nil {
		log.Fatal(err)
	}
	applyMemLimit()
	rcon.OpOwners(remoteOwners) // (con RCON: los dueños, op en el servidor)
	if *logsFlag {
		s, err := openSession()
		if err != nil {
			log.Fatal("--logs: ", err)
		}
		session = s
		log.SetOutput(io.MultiWriter(os.Stderr, s))
		log.Printf("--logs: guardando el log y la traza de movimiento en %s", s.path)
	}
	// Ctrl-C cierra la conexión limpiamente.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	appQuit = stop
	err := runForever(ctx, stop)
	if e := pathMemory.Save(dataFile(pathMemoryFile)); e != nil {
		log.Printf("memoria de caminos: no se guardó: %v", e)
	}
	if session != nil {
		log.Printf("--logs: sesión guardada en %s", session.path)
		session.Close()
	}
	if restartRequested.Load() {
		restartSelf() // (ajustes del panel)
	}
	if err != nil {
		log.Fatal(err)
	}
}

// run es una conexión: crea el bot, entra y juega hasta que se corta.
// prev es el bot de la conexión anterior (su trabajo y su cola se retoman).
// again: merece la pena volver a entrar (no se salió a propósito ni falló
// el login).
func run(ctx context.Context, quit func(), prev *Bot) (b *Bot, again bool, err error) {
	conn, cancel := context.WithCancel(ctx) // lo de esta conexión acaba con ella
	defer cancel()
	b = &Bot{Client: bot.NewClient(), ctx: conn, quit: quit}
	if err := b.setAuth(ctx); err != nil {
		return b, false, err
	}

	// basic.Player es obligatorio: responde los keep-alive del servidor.
	settings := basic.DefaultSettings
	settings.ViewDistance = viewDistance() // menos chunks, menos memoria (--view)
	b.Player = basic.NewPlayer(b.Client, settings, basic.EventsListener{
		GameStart:    b.onJoin,
		Disconnect:   b.onDisconnect,
		HealthChange: b.onHealthChange,
		Death:        b.onDeath,
		Teleported:   b.onTeleported,
	})
	b.Players = playerlist.New(b.Client)
	b.Chat = msg.New(b.Client, b.Player, b.Players, msg.EventsHandler{
		SystemChat: b.onSystemChat,
		PlayerChat: b.onPlayerChat,
	})
	b.setupModules()
	b.watchPlayers()
	b.watchSeen()
	b.watchThrown() // recoger lo que le tiran (y ponérselo, si es equipo mejor)
	b.inherit(prev) // la cola y el trabajo de la conexión anterior
	current.Store(b)

	addr := serverAddr
	if a := os.Getenv("MCBOT_ADDR"); a != "" {
		addr = a // otro servidor (de pruebas: go run ./tools/fakesrv)
	}
	log.Printf("conectando a %s como %s", addr, b.Client.Auth.Name)
	if err := b.Client.JoinServer(addr); err != nil {
		return b, true, fmt.Errorf("no se pudo entrar: %w", err)
	}
	defer b.Client.Close()
	defer b.markInterrupted() // (antes de cancel: el trabajo se retoma al volver)
	log.Print("login correcto")
	b.connectedAt = time.Now()

	// Al cancelar ctx se cierra la conexión, y eso termina HandleGame.
	go func() {
		<-conn.Done()
		b.Client.Close()
	}()

	// El controlador mueve al jugador cada tick; los módulos deciden en sus goroutines.
	go func() {
		if err := b.Ctl.Run(conn); err != nil && conn.Err() == nil {
			log.Print("control: ", err)
		}
	}()
	go func() {
		if err := b.Combat.Run(conn); err != nil && conn.Err() == nil {
			log.Print("pelea: ", err)
		}
	}()
	go b.resumeAfterReconnect()

	for {
		err := b.Client.HandleGame() // solo devuelve con error
		var kicked bot.DisconnectErr
		var handlerErr bot.PacketHandlerError
		switch {
		case ctx.Err() != nil:
			log.Print("desconectado")
			return b, false, nil
		case b.noRetry.Load():
			return b, false, nil // (el login falló: repetir no sirve)
		case errors.As(err, &kicked):
			reason := chat.Message(kicked).ClearString()
			log.Print("expulsado: ", reason)
			if finalKick.MatchString(reason) {
				log.Print("no vuelvo a entrar: eso no se arregla esperando")
				return b, false, nil
			}
			return b, true, nil
		case errors.As(err, &handlerErr):
			log.Print(err) // falló un handler; la conexión sigue viva
		default:
			return b, true, fmt.Errorf("conexión perdida: %w", err)
		}
	}
}

// pathMemory: las rutas que salieron bien y las trabas (pathing/memory.go),
// en caminos.json: se carga al conectar la primera vez y se guarda cada
// minuto y al salir.
var (
	pathMemory     = pathing.NewMemory()
	pathMemoryOnce sync.Once
)

const pathMemoryFile = "caminos.json"

func loadPathMemory() *pathing.Memory {
	pathMemoryOnce.Do(func() {
		if err := pathMemory.Load(dataFile(pathMemoryFile)); err != nil {
			log.Printf("memoria de caminos: %v (empiezo sin ella)", err)
		} else if n := pathMemory.Routes(); n > 0 {
			log.Printf("memoria de caminos: %d rutas, %d trabas", n, pathMemory.Snags())
		}
		go func() {
			for range time.Tick(time.Minute) {
				_ = pathMemory.Save(dataFile(pathMemoryFile))
			}
		}()
	})
	return pathMemory
}

// setupModules crea el mundo, las físicas, el movimiento y los módulos.
func (b *Bot) setupModules() {
	b.World = world.NewWorld(b.Client, b.Player, world.EventsListener{})
	b.World.SkipLight = true // el bot no mira la luz: es 3/4 de la memoria de cada chunk
	b.Screens = screen.NewManager(b.Client, screen.EventsListener{})
	totals := b.itemTotals
	carriedSource.Store(&totals) // el autocompletado de /drop
	b.Entities = entities.New(b.Client, entities.Events{})
	b.Ctl = control.New(b.Client, b.Player, b.World)

	tags := physics.ClientTags{T: &b.Client.Tags}
	mover := &physics.Mover{World: b.World, Tags: tags, Food: func() int { return b.Player.Status().Food }}
	b.Walker = pathing.New(b.World, b.Player, tags)
	b.Walker.SetMemory(loadPathMemory()) // (la misma en cada reconexión)
	b.Hands = &act.Hands{C: b.Client, World: b.World, Screens: b.Screens}
	// puentes y torres con los bloques que lleve (después de crear las manos: las usa)
	b.Builder = &pathing.HandsBuilder{Screens: b.Screens, Hands: b.Hands, Ctl: b.Ctl, Ctx: b.ctx}
	b.Walker.SetBuilder(b.Builder)

	b.Fire = &safety.Fire{Client: b.Client, Player: b.Player, World: b.World, Mover: mover, Screens: b.Screens, Hands: b.Hands,
		Logf: func(format string, args ...any) { log.Printf("[fuego] "+format, args...) }}
	b.Fire.Register()
	b.Fall = &safety.Fall{World: b.World, Mover: mover, Screens: b.Screens, Hands: b.Hands,
		Logf: func(format string, args ...any) { log.Printf("[caída] "+format, args...) }}
	b.Lava = &safety.Lava{World: b.World, Mover: mover, Stop: b.Walker.Stop,
		Logf:       func(format string, args ...any) { log.Printf("[lava] "+format, args...) },
		Extinguish: func(p world.BlockPos) { b.Hands.Dig(p, nil) }}

	b.Combat = combat.New(combat.Deps{
		Client: b.Client, Player: b.Player, Entities: b.Entities, Players: b.Players,
		Screens: b.Screens, World: b.World, Hands: b.Hands, Ctl: b.Ctl, Walker: b.Walker, Mover: mover,
		NameOf: nameOf,
		// la armadura de oro no se cambia por otra mejor en el Nether (ni yendo a él): los piglins
		KeepGold: func() bool { return b.Tasks != nil && b.Tasks.KeepGold() },
		// libre (sin tarea, sin tirar sus cosas): con la mejor arma en la mano
		Idle: func() bool { return b.Tasks != nil && b.Tasks.Status() == "" && !b.workResuming() && !dropping() },
		Ordered: func() bool {
			return currentMove() != "" || detour.Load() || dropping() || b.Tasks != nil && (b.Tasks.Status() != "" || b.workResuming())
		},
		// los dueños: aunque vengan armados, no se les planta cara
		Friend: func(id uuid.UUID) bool { return allowedRemote(nameOf(id)) || hubSibling(nameOf(id)) }, // (y los otros bots del panel)
		Ally:   func(id uuid.UUID) bool { return hubSibling(nameOf(id)) },
		// con qué encerrarse con bloques para curarse contra un jugador (bunker.go)
		Builder: b.Builder,
	})

	b.Lava.Scoop = b.Combat.ScoopLava // (lava encima: quitarla con el cubo, al instante)
	b.Combat.Reflex = func() bool { return b.Fire.Active() || b.Fall.Active() }
	b.Fire.BeforeUse = b.Combat.ReleaseHands
	b.Combat.Burning = b.Fire.Burning                         // ardiendo sin cubo: al agua que haya cerca
	b.Fire.Calm = func() bool { return !b.Combat.Fighting() } // (rellenar el cubo vacío, fuera de pelea)

	b.Tasks = automation.New(automation.Deps{
		Client: b.Client, Player: b.Player, World: b.World, Entities: b.Entities, Screens: b.Screens,
		Hands: b.Hands, Ctl: b.Ctl, Walker: b.Walker, Builder: b.Builder,
		Fighting:    b.Combat.Fighting,
		HasFood:     b.Combat.HasFood,
		Escaping:    b.Lava.Escaping,
		StopOthers:  b.cancelMove,
		StorageFile: dataFile("almacen.json"),
		BuildFile:   dataFile("construccion.json"),
		Say: func(text string) {
			// (el servidor rechaza los de más de 256: entero o nada)
			if err := b.Chat.SendMessage(shortChat(text, 250)); err != nil {
				log.Printf("chat: %v", err)
			}
		},
		Lang:    lang,
		Trace:   func(format string, args ...any) { session.Trace(format, args...) },
		OnEnd:   func(name string, err error) { go b.onTaskEnd(name, err) },
		OnStall: b.onTaskStall,
		Problem: noteProblem,
	})

	b.Ctl.Act = b.Hands.Act
	// primero los reflejos (la lava); luego la pelea, que fuera de combate le pasa el control al walker
	b.Ctl.Think = func(s *control.State, bp *basic.Player) {
		if s.Ticks%20 == 0 && bp.Status().Health > 0 && b.carrying() {
			b.lastCarry.Store(time.Now().UnixNano())
		}
		if b.Lava.Think(s, bp) {
			return
		}
		if b.Fall.Think(s, bp) {
			return
		}
		if b.Fire.Think(s, bp) {
			return
		}
		b.Combat.Think(s, bp)
	}
	b.Ctl.Step = mover.Step
	// Tras un teletransporte el cliente vanilla de 26.2 manda la confirmación y
	// justo después su posición (ClientPacketListener.handleMovePlayer:
	// AcceptTeleportation + MovePlayer.PosRot). La librería solo manda la
	// confirmación (en 26.3 la posición va dentro de ella). Un anticheat que
	// simula el movimiento espera ese paquete: sin él sigue corrigiendo.
	// Prioridad 40: después de que basic aplique el teletransporte (100) y el
	// controlador lo confirme (50).
	if version.ProtocolVersion < 777 {
		b.Client.Events.AddListener(bot.PacketHandler{
			Priority: 40, ID: packetid.ClientboundPlayPlayerPosition,
			F: func(pk.Packet) error {
				pos := b.Player.Position()
				m := play.MovePlayerPosRot{X: pk.Double(pos.X), Y: pk.Double(pos.Y), Z: pk.Double(pos.Z), YRot: pk.Float(pos.Yaw), XRot: pk.Float(pos.Pitch)}
				return b.Client.Conn.WritePacket(pk.Marshal(m.PacketID(), m))
			},
		})
	}
	b.Ctl.Ticked = func(s *control.State, bp *basic.Player) {
		p := bp.Position()
		b.lastPos.Store(&p)
		ti := tickInfo{input: s.Input, onGround: s.OnGround, collided: s.HorizontalCollision, sprinting: s.Sprinting, tick: s.Ticks}
		if pl := mover.Player(); pl != nil {
			ti.vel = [3]float64{pl.Vel.X, pl.Vel.Y, pl.Vel.Z}
		}
		b.lastTick.Store(&ti)
		b.trace(s, bp, ti)
	}
}

// trace escribe en el archivo de --logs el estado de este tick: en movimiento
// cada tick, quieto uno por segundo.
func (b *Bot) trace(s *control.State, bp *basic.Player, ti tickInfo) {
	if session == nil {
		return
	}
	// los atributos de movimiento, al cambiar (¿manda el servidor los modificadores de los efectos?)
	var attrs []string
	for _, name := range []string{"minecraft:movement_speed", "minecraft:jump_strength", "minecraft:step_height", "minecraft:gravity"} {
		if a, ok := bp.Attribute(name); ok {
			mods := ""
			for _, m := range a.Modifiers {
				mods += fmt.Sprintf(" %s(op%d %.4f)", m.ID, m.Operation, float64(m.Amount))
			}
			attrs = append(attrs, fmt.Sprintf("%s base=%.4f valor=%.4f%s", strings.TrimPrefix(name, "minecraft:"), a.Base, a.Value(), mods))
		}
	}
	if line := strings.Join(attrs, " | "); line != b.lastAttrs {
		b.lastAttrs = line
		session.Trace("ATTR %s", line)
	}
	b.traceInventory()
	moving := s.Input != (control.Input{}) || !s.OnGround || ti.vel[0]*ti.vel[0]+ti.vel[2]*ti.vel[2] > 1e-6
	if !moving && s.Ticks%20 != 0 {
		return
	}
	p := bp.Position()
	st := bp.Status()
	var eff []string // los efectos activos (los de un beacon cambian el salto y la velocidad)
	for name, e := range st.Effects {
		eff = append(eff, fmt.Sprintf("%s:%d", strings.TrimPrefix(name, "minecraft:"), e.Amplifier+1))
	}
	sort.Strings(eff)
	task := ""
	if b.Tasks != nil {
		task = b.Tasks.Status()
	}
	session.Trace("TICK t=%d pos=%.4f,%.4f,%.4f vel=%.4f,%.4f,%.4f yaw=%.1f pitch=%.1f suelo=%v choque=%v corriendo=%v teclas=%s vida=%.1f comida=%d bloques=%d efectos=[%s] manos=%v tarea=%q camino=[%s]",
		s.Ticks, p.X, p.Y, p.Z, ti.vel[0], ti.vel[1], ti.vel[2], p.Yaw, p.Pitch, s.OnGround, s.HorizontalCollision, s.Sprinting,
		keys(s.Input), st.Health, st.Food, b.Walker.BuildBlocks(), strings.Join(eff, ","), b.Hands.Busy(), task, b.Walker.Debug())
}

// tickInfo es lo que hacía el bot en un tick (para entender una corrección del servidor).
type tickInfo struct {
	input              control.Input
	onGround, collided bool
	sprinting          bool
	vel                [3]float64
	tick               uint64
}
