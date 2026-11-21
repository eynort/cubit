package main

// Órdenes por mensaje privado: lo que un jugador autorizado (remoteOwners) le
// manda al bot por /msg se ejecuta como si se escribiera en su consola, y el
// bot le contesta por privado. "yo"/"me" en la orden es quien la manda:
// "/follow yo", "/guard yo", "/goto yo". Lo que no parece una orden (y lo
// que manda cualquier otro jugador) lo contesta la IA (ai.go), si está.

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"unicode/utf8"

	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
)

func allowedRemote(name string) bool {
	for _, o := range remoteOwners {
		if o == "*" || strings.EqualFold(o, name) {
			return true
		}
	}
	return false
}

// onWhisper ejecuta un privado como orden de consola, si quien lo manda puede.
func (b *Bot) onWhisper(from, text string) {
	if strings.EqualFold(from, botName) || strings.EqualFold(from, "me") || strings.EqualFold(from, "yo") || strings.EqualFold(from, "mi") {
		return // el eco de un privado que mandó el propio bot
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if isCoordsReply(from, text) {
		return // la respuesta a "¿dónde estás?" de /goto (ya anotada), no una orden
	}
	if !allowedRemote(from) {
		log.Printf("ignoro la orden por /msg de %s (no está en remoteOwners): %s", from, text)
		return
	}
	line := remoteLine(text, from)
	log.Printf("[orden de %s por /msg] %s", from, line)
	reply := "ok: " + line
	if err := asOrderFrom(from, func() error { return b.run(line) }); err != nil {
		reply = "error: " + err.Error()
		log.Print(err)
	}
	b.whisper(from, reply)
}

// looksLikeCommand: el privado es una orden directa (sin pasar por la IA):
// empieza por "/", o empieza por una orden del bot y no tiene más palabras que
// argumentos ("stop", "follow yo", "goto 100 64 200", "say hola a todos").
func looksLikeCommand(text string) bool {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "/") {
		return true
	}
	f := strings.Fields(text)
	if len(f) == 0 {
		return false
	}
	c, ok := commands[strings.ToLower(f[0])]
	if !ok || strings.EqualFold(f[0], "ai") {
		return false
	}
	if n := len(c.args); n > 0 && c.args[n-1].rest {
		return true
	}
	return len(f)-1 <= len(c.args)
}

// remoteLine convierte el privado en una línea de consola: con "/" delante
// (un privado siempre es una orden) y "yo"/"me" cambiados por quien la manda
// donde la orden espera un jugador (/follow yo, /guard me…).
func remoteLine(text, from string) string {
	f := strings.Fields(strings.TrimPrefix(text, "/"))
	if len(f) == 0 {
		return "/"
	}
	if c, ok := commands[strings.ToLower(f[0])]; ok {
		for i := 1; i < len(f) && i <= len(c.args); i++ {
			if c.args[i-1].kind != argPlayer && !c.args[i-1].orPlayer {
				continue
			}
			switch strings.ToLower(f[i]) {
			case "yo", "me", "@s":
				f[i] = from
			}
		}
	}
	return "/" + strings.Join(f, " ")
}

// whisper le manda un privado a un jugador.
func (b *Bot) whisper(to, text string) {
	if len(text) > 200 {
		cut := 200
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut-- // sin partir una letra (á, ñ…)
		}
		text = text[:cut] + "…"
	}
	c := play.ChatCommand{Command: pk.String(fmt.Sprintf("msg %s %s", to, text))}
	if err := b.Client.Conn.WritePacket(pk.Marshal(c.PacketID(), c)); err != nil {
		log.Print("msg: ", err)
	}
}

// onlyNumbers: "120 -450", "120, 64, -450".
var onlyNumbers = regexp.MustCompile(`^[\s\d.,;:xyzXYZ~-]+$`)

// isCoordsReply: son coordenadas y, o se le preguntó a ese jugador dónde está
// (/goto), o el mensaje no tiene nada más que números.
func isCoordsReply(from, text string) bool {
	if _, _, ok := parseCoords(text); !ok {
		return false
	}
	if onlyNumbers.MatchString(text) {
		return true
	}
	travel.Lock()
	_, asked := travel.asked[strings.ToLower(from)]
	travel.Unlock()
	return asked
}
