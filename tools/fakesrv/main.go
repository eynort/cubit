// fakesrv es un servidor de Minecraft falso en local (el de go-mc26-kit), para
// probar el bot sin entrar al servidor de verdad: un mundo llano, modo offline.
//
//	go run ./tools/fakesrv            # imprime la dirección
//	MCBOT_ADDR=<dirección> go run .   # el bot entra ahí
//	go run . <dirección>              # (o así)
//
// Con -kick-after echa al bot a los tantos segundos (para probar que vuelve a entrar). Con -demo
// hace de jugador "Tester" que le manda órdenes por chat (el modo --demo).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
)

var (
	kickFlag = flag.Duration("kick-after", 0, "echar al bot pasado este tiempo (0 = nunca)")
	quiet    = flag.Bool("q", false, "sin log")
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
