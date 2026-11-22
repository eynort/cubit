// fakesrv es un servidor de Minecraft falso en local (el de go-mc26-kit), para
// probar el bot sin entrar al servidor de verdad: un mundo llano, modo offline.
//
//	go run ./tools/fakesrv            # imprime la dirección
//	MCBOT_ADDR=<dirección> go run .   # el bot entra ahí
//	go run . <dirección>              # (o así)
//
// Con -kick-after echa al bot a los tantos segundos (para probar que vuelve a entrar).
// Con -console lee órdenes de la entrada estándar y apunta en el log lo que el bot
// escribe en el chat y los comandos que manda:
//
//	chat <jugador> <texto>   un jugador dice algo en el chat público (p. ej. el dueño)
//	sys <texto>              un mensaje del servidor (p. ej. «Please login with /login <password>»)
//	kick                     echar al bot
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

var (
	kickFlag = flag.Duration("kick-after", 0, "echar al bot pasado este tiempo (0 = nunca)")
	quiet    = flag.Bool("q", false, "sin log")
	console  = flag.Bool("console", false, "órdenes por la entrada estándar (chat, sys, kick) y el chat del bot en el log")
)

func main() {
	flag.Parse()
	s, err := fakeserver.Listen()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer s.Close()
	s.KeepAlive = 15 * time.Second                       // (como un servidor de verdad: sin esto el bot se desconecta a los 20 s)
	s.World.Fill(3, 64, -2, 6, 64, 2, "minecraft:stone") // un escalón para probar
	fmt.Println(s.Addr)
	if *quiet {
		log.SetOutput(new(bytes.Buffer))
	}
	if *console {
		s.Hook = logOutgoing
		go readConsole(s)
	}
	go watch(s)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}

// watch atiende a cada jugador que entra.
func watch(s *fakeserver.Server) {
	var last *fakeserver.Player
	for {
		time.Sleep(200 * time.Millisecond)
		ps := s.Players()
		if len(ps) == 0 || ps[0] == last {
			continue
		}
		p := ps[0]
		last = p
		log.Printf("entra %s", p.Name)
		if *kickFlag > 0 {
			time.AfterFunc(*kickFlag, func() {
				log.Printf("echo a %s", p.Name)
				_ = p.Kick("Servidor reiniciándose")
			})
		}
	}
}

// logOutgoing apunta lo que el bot escribe en el chat y los comandos que manda.
func logOutgoing(p fakeserver.Packet) {
	switch p.ID {
	case packetid.ServerboundPlayChat:
		var c play.Chat
		if p.Scan(&c) == nil {
			log.Printf("chat del bot: %s", c.Message)
		}
	case packetid.ServerboundPlayChatCommand, packetid.ServerboundPlayChatCommandSigned:
		var c play.ChatCommand
		if p.Scan(&c) == nil {
			log.Printf("comando del bot: /%s", c.Command)
		}
	}
}

// readConsole: las órdenes de -console.
func readConsole(s *fakeserver.Server) {
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		verb, rest, _ := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		ps := s.Players()
		if len(ps) == 0 {
			log.Print("no hay nadie dentro")
			continue
		}
		var err error
		switch verb {
		case "chat":
			from, text, _ := strings.Cut(rest, " ")
			err = ps[0].Chat(from, text)
		case "sys":
			err = ps[0].Send(play.SystemChat{Content: types.Text(chat.Text(rest))})
		case "kick":
			err = ps[0].Kick("Servidor reiniciándose")
		default:
			log.Printf("¿%s? (chat <jugador> <texto> | sys <texto> | kick)", verb)
		}
		if err != nil {
			log.Print(err)
		}
	}
}
