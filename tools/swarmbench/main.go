// swarmbench mide cuánto cuesta cada bot de más si varios comparten un
// proceso (la idea del modo compartido del hub: docs/PERFORMANCE.md). Arranca
// el servidor falso del kit con -radius chunks alrededor del spawn y mete -n
// clientes del kit con las mismas piezas que monta un bot de Cubit (mundo,
// entidades, inventario, controlador, físicas, walker de pathing/), en un solo
// proceso; mide la memoria con 1 cliente y con n.
//
//	go run ./tools/swarmbench -n 10 -radius 6
//
// Es una cota inferior: no lleva los módulos de pelea ni de tareas ni el
// panel de cada bot (poca memoria, sobre todo goroutines y estructuras).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26-kit/bot/control"
	"github.com/mj41/go-mc26-kit/bot/entities"
	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26-kit/bot/msg"
	"github.com/mj41/go-mc26-kit/bot/physics"
	"github.com/mj41/go-mc26-kit/bot/playerlist"
	"github.com/mj41/go-mc26-kit/bot/screen"
	"github.com/mj41/go-mc26-kit/bot/world"

	"cubit/internal/memtune"
	"cubit/pathing"
)

var (
	nFlag      = flag.Int("n", 10, "clientes en el proceso")
	radiusFlag = flag.Int("radius", 6, "chunks alrededor del spawn que manda el servidor falso")
	waitFlag   = flag.Duration("wait", 8*time.Second, "espera tras entrar cada tanda (que lleguen los chunks)")
	gcFlag     = flag.Bool("tune", true, "aplicar internal/memtune (GOGC 50) como un bot")
)

func main() {
	flag.Parse()
	log.SetFlags(0)
	if *gcFlag {
		memtune.Apply(0)
	}
	s, err := fakeserver.Listen()
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	s.World.Radius = *radiusFlag
	s.MaxPlayers = *nFlag
	s.KeepAlive = 10 * time.Second

	base := measure()
	fmt.Printf("sin clientes:        %s\n", base)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	join(ctx, s.Addr, 0)
	time.Sleep(*waitFlag)
	one := measure()
	fmt.Printf("1 cliente:           %s\n", one)
	for i := 1; i < *nFlag; i++ {
		join(ctx, s.Addr, i)
	}
	time.Sleep(*waitFlag)
	all := measure()
	fmt.Printf("%d clientes:         %s\n", *nFlag, all)
	if *nFlag > 1 {
		k := float64(*nFlag - 1)
		fmt.Printf("por cliente de más:  RSS %.1f MB · montón vivo %.1f MB · goroutines %.0f\n",
			(all.rss-one.rss)/k, (all.live-one.live)/k, float64(all.goroutines-one.goroutines)/k)
	}
}

// join mete el cliente i con las piezas de un bot.
func join(ctx context.Context, addr string, i int) {
	c := bot.NewClient()
	c.Auth.Name = fmt.Sprintf("Swarm%d", i)
	p := basic.NewPlayer(c, basic.DefaultSettings, basic.EventsListener{})
	players := playerlist.New(c)
	msg.New(c, p, players, msg.EventsHandler{})
	w := world.NewWorld(c, p, world.EventsListener{})
	w.SkipLight = true // (como el bot)
	screens := screen.NewManager(c, screen.EventsListener{})
	entities.New(c, entities.Events{})
	ctl := control.New(c, p, w)
	tags := physics.ClientTags{T: &c.Tags}
	mover := &physics.Mover{World: w, Tags: tags, Food: func() int { return p.Status().Food }}
	walker := pathing.New(w, p, tags)
	hands := &act.Hands{C: c, World: w, Screens: screens}
	ctl.Act = hands.Act
	ctl.Think = func(s *control.State, bp *basic.Player) { walker.Think(s, bp) }
	ctl.Step = mover.Step
	if err := c.JoinServer(addr); err != nil {
		log.Fatalf("cliente %d: %v", i, err)
	}
	go func() { _ = c.HandleGame() }()
	go func() { _ = ctl.Run(ctx) }()
}

type mem struct {
	rss, live, heapSys float64
	goroutines         int
}

func (m mem) String() string {
	return fmt.Sprintf("RSS %.1f MB · montón vivo %.1f MB · montón del sistema %.1f MB · goroutines %d", m.rss, m.live, m.heapSys, m.goroutines)
}

// measure: tras una recolección completa y devolver lo libre (lo que queda es lo vivo).
func measure() mem {
	debug.FreeOSMemory()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return mem{rss: rssMB(), live: float64(ms.HeapAlloc) / (1 << 20), heapSys: float64(ms.HeapSys-ms.HeapReleased) / (1 << 20), goroutines: runtime.NumGoroutine()}
}

func rssMB() float64 {
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	var size, rss uint64
	if _, err := fmt.Sscan(string(b), &size, &rss); err != nil {
		return 0
	}
	return float64(rss*uint64(os.Getpagesize())) / (1 << 20)
}
