package main

import (
	"fmt"
	"log"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26-kit/bot/msg"
	"github.com/mj41/go-mc26/chat"

	"cubit/automation"
)

// Aquí van tus eventos. Desde cualquiera de ellos tienes:
//   b.Chat.SendMessage("hola")    enviar un mensaje al chat
//   b.Chat.SendCommand("time set day")  ejecutar un comando (sin "/")
//   b.Player.Position()           posición actual
//   b.Player.Status()             vida, comida, experiencia
//   b.Players                     lista de jugadores (tab)
//   b.Combat.Fighting()           si está en una pelea (no lo interrumpas)
// Si un handler devuelve un error, se registra en el log y el bot sigue conectado.

// onJoin se llama cuando el bot ya está dentro del mundo.
func (b *Bot) onJoin() error {
	log.Print("dentro del servidor")
	return nil
}

// onDisconnect se llama cuando el servidor expulsa al bot.
func (b *Bot) onDisconnect(reason chat.Message) error {
	return bot.DisconnectErr(reason) // termina el bucle de main
}

// onHealthChange se llama cuando cambian la vida o la comida.
func (b *Bot) onHealthChange(health float32, food int32, saturation float32) error {
	return nil
}

// onDeath se llama al morir.
func (b *Bot) onDeath() error {
	p := b.Player.Position()
	dim := string(b.Player.Spawn.Dimension)
	log.Printf("he muerto en %.0f %.0f %.0f, reapareciendo", p.X, p.Y, p.Z)
	_ = dim
	if b.carriedSomething() {
		// solo si llevaba algo (una muerte con el inventario vacío no tira
		// nada: no se cambia el sitio al que volver por esta)
		b.lastDeath.Store(&deathSpot{x: p.X, y: p.Y, z: p.Z, dim: dim, at: time.Now()})
	}
	b.Combat.ForgetFights() // la zona y a quién protege siguen al reaparecer
	working := b.workAtDeath()
	if b.Tasks != nil && b.Tasks.Status() != "" {
		if working {
			log.Print("tarea parada: he muerto (la retomo al volver)")
		} else {
			log.Print("tarea parada: he muerto")
		}
		go b.Tasks.Stop() // espera a que acabe: no aquí, que esto es la lectura del servidor
	}
	if working { // de vuelta al trabajo (y a por lo que tiré, si fue cerca)
		go b.resumeWork(b.lastDeath.Load())
	} else if d := b.lastDeath.Load(); d != nil {
		go b.backToDeath(*d)
	}
	return b.Player.Respawn()
}

// onTeleported se llama cuando el servidor mueve al bot. El controlador
// (b.Ctl) ya lo confirma: NO llames a AcceptTeleportation aquí, el servidor
// toma una segunda confirmación como movimiento inválido y te desconecta.
//
// Un teletransporte a pocos bloques de donde estaba es el servidor (o su
// anticheat) rechazando un movimiento: se avisa, porque si se repite al
// saltar es que el servidor no acepta esos saltos.
func (b *Bot) onTeleported(x, y, z float64, yaw, pitch float32, flags, teleportID int32) error {
	session.Trace("TELEPORT id=%d a=%.4f,%.4f,%.4f yaw=%.1f pitch=%.1f flags=%d", teleportID, x, y, z, yaw, pitch, flags)
	if flags != 0 || b.Ctl == nil || !b.Ctl.State().Loaded {
		return nil // relativo (no es una corrección) o aún entrando
	}
	last := b.lastPos.Load()
	if last == nil {
		return nil
	}
	if d := math.Sqrt((x-last.X)*(x-last.X) + (y-last.Y)*(y-last.Y) + (z-last.Z)*(z-last.Z)); d < 8 && d > 0.01 {
		b.corrections.Add(1)
		desc := ""
		if ti := b.lastTick.Load(); ti != nil {
			desc = fmt.Sprintf(" — yo estaba en %.2f %.2f %.2f, suelo=%v choque=%v corriendo=%v vel=%.2f,%.2f,%.2f teclas=%s",
				last.X, last.Y, last.Z, ti.onGround, ti.collided, ti.sprinting, ti.vel[0], ti.vel[1], ti.vel[2], keys(ti.input))
		}
		log.Printf("el servidor me corrigió la posición (%.1f bloques, a %.2f %.2f %.2f)%s", d, x, y, z, desc)
		if what := b.Walker.Rejected(); what != "" {
			log.Print("  → ", what)
		}
	}
	return nil
}

// toBot: un mensaje del chat va para el bot (lo nombra: "Steve_Bot",
// "steve", "bot"). Los demás son conversaciones de otros: no son una
// respuesta a lo que preguntó.
func toBot(text string) bool {
	t := strings.ToLower(text)
	name := strings.ToLower(botName)
	if name != "" && strings.Contains(t, name) {
		return true
	}
	if i := strings.IndexAny(name, "_-"); i >= 4 && strings.Contains(t, name[:i]) {
		return true
	}
	return botWord.MatchString(t)
}

var botWord = regexp.MustCompile(`\bbot\b`)

// onSystemChat recibe los mensajes del sistema (entradas, salidas, comandos...).
func (b *Bot) onSystemChat(m chat.Message, overlay bool) error {
	if overlay { // overlay = texto encima de la hotbar
		return nil
	}
	text := m.ClearString()
	log.Print("[sistema] ", text)
	emitEvent(apiEvent{Type: "system", Text: text})
	return nil
}

// onPlayerChat recibe los mensajes de los jugadores.
func (b *Bot) onPlayerChat(m msg.PlayerChat) error {
	if m.Sender == b.Client.UUID {
		return nil // ignora sus propios mensajes
	}
	log.Printf("<%s> %s", m.Name, m.Content)
	kind := "chat"
	if m.Type == "minecraft:msg_command_incoming" {
		kind = "whisper"
	}
	emitEvent(apiEvent{Type: kind, From: m.Name, Text: m.Content})
	if b.Tasks != nil && (m.Type == "minecraft:msg_command_incoming" || toBot(m.Content)) {
		b.Tasks.NoteChat() // una respuesta a lo que preguntó el bot (storageStuck)
	}
	noteCoords(m.Sender, m.Content)                 // por si son coordenadas (para /goto)
	if m.Type == "minecraft:msg_command_incoming" { // un /msg al bot: una orden
		go b.onWhisper(m.Name, m.Content)
	}
	return nil
}

// backToDeath: tras reaparecer, vuelve a donde murió a recoger sus cosas
// (las cosas tiradas desaparecen a los 5 minutos). Solo en la misma
// dimensión; una orden del operador lo cancela, como cualquier goto.
func (b *Bot) backToDeath(d deathSpot) {
	x, y, z, dim := d.x, d.y, d.z, d.dim
	// esperar a reaparecer (la vida vuelve) y a que carguen los chunks
	for i := 0; i < 100; i++ {
		time.Sleep(200 * time.Millisecond)
		if b.Player.Status().Health > 0 && b.Ctl.State().Loaded {
			break
		}
	}
	time.Sleep(2 * time.Second)
	if time.Since(d.at) > 4*time.Minute {
		log.Print("lo que tiré al morir ya habrá desaparecido (5 minutos): no vuelvo")
		b.lastDeath.Store(nil)
		return
	}
	if string(b.Player.Spawn.Dimension) != dim {
		log.Printf("morí en otra dimensión (%s): no puedo volver andando a por mis cosas", dim)
		return
	}
	p := b.Player.Position()
	// lo tirado dura 5 minutos: corriendo (5,6 bloques/s) son unos 1700 bloques,
	// y por el camino se tarda más. Más lejos (en los logs: a 28 000 bloques) no se llega
	if far := math.Hypot(x-p.X, z-p.Z); far > 1000 {
		log.Printf("morí a %.0f bloques de aquí: mis cosas desaparecen antes de que llegue, no vuelvo", far)
		b.lastDeath.Store(nil)
		return
	}
	log.Printf("vuelvo a donde morí (%.0f %.0f %.0f, a %.0f bloques) a recoger mis cosas", x, y, z, math.Hypot(x-p.X, z-p.Z))
	ctx, cancel := b.startMove(b.ctx)
	gen := setMoveKind("goto")
	defer cancel()
	defer clearMove(gen)
	stop := func() bool { return time.Since(d.at) > 5*time.Minute }
	if !b.gotoCoords(ctx, x, z, stop) {
		if stop() {
			log.Print("ya pasaron 5 minutos desde que morí: lo que tiré ya no está")
			b.lastDeath.Store(nil)
			return
		}
		if ctx.Err() == nil {
			log.Print("no consigo volver a donde morí")
		}
		return
	}
	clearMove(gen)
	log.Print("en el sitio donde morí: recojo lo que haya")
	b.lastDeath.Store(nil)
	b.Tasks.Start(b.ctx, "recoger", automation.PickUp(12))
}

// deathSpot es donde murió el bot llevando cosas (y cuándo).
type deathSpot struct {
	x, y, z float64
	dim     string
	at      time.Time
}

// carriedSomething: llevaba algo en el inventario hace nada (lo que se cae
// al morir; el servidor puede haberlo vaciado ya al avisar de la muerte).
func (b *Bot) carriedSomething() bool {
	return time.Since(time.Unix(0, b.lastCarry.Load())) < 3*time.Second || b.carrying()
}

// carrying: lleva algo en el inventario ahora.
func (b *Bot) carrying() bool {
	b.Screens.Lock()
	defer b.Screens.Unlock()
	for i := 5; i <= 45 && i < len(b.Screens.Inventory.Slots); i++ {
		if b.Screens.Inventory.Slots[i].Count > 0 {
			return true
		}
	}
	return false
}
