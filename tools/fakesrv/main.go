// fakesrv es un servidor de Minecraft falso en local (el de go-mc26-kit), para
// probar el bot sin entrar al servidor de verdad: un mundo llano, modo offline.
//
//	go run ./tools/fakesrv            # imprime la dirección
//	MCBOT_ADDR=<dirección> go run .   # el bot entra ahí
//	go run . <dirección>              # (o así)
//
// Con -auth hace de plugin de login: pide /register (y /login a quien ya se
// registró) y no deja jugar hasta que se lo manda. Con -kick-after echa al
// bot a los tantos segundos (para probar que vuelve a entrar). Con -demo
// hace de jugador "Tester" que le manda órdenes por chat (el modo --demo).
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
	"github.com/mj41/go-mc26/chat"
	"github.com/mj41/go-mc26/data/packetid"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

var (
	authFlag = flag.Bool("auth", false, "hacer de plugin de login (/register y /login)")
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
	var mu sync.Mutex
	passwords := map[string]string{} // nombre → contraseña (lo que guardaría el plugin)
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
		go func() {
			if *kickFlag > 0 {
				time.AfterFunc(*kickFlag, func() {
					log.Printf("echo a %s", p.Name)
					_ = p.Kick("Servidor reiniciándose")
				})
			}
			if !*authFlag {
				return
			}
			mu.Lock()
			pass, registered := passwords[p.Name]
			mu.Unlock()
			seen := 0
			for i := 0; i < 300; i++ { // 60 s para entrar
				if i%25 == 0 {
					if registered {
						say(p, "Usa /login <contraseña>")
					} else {
						say(p, "Usa /register <contraseña> <contraseña>")
					}
				}
				cmds := p.Got(packetid.ServerboundPlayChatCommand)
				for ; seen < len(cmds); seen++ {
					var c play.ChatCommand
					if cmds[seen].Scan(&c) != nil {
						continue
					}
					f := strings.Fields(string(c.Command))
					log.Printf("%s: /%s ***", p.Name, f[0])
					switch {
					case !registered && f[0] == "register" && len(f) == 3 && f[1] == f[2]:
						mu.Lock()
						passwords[p.Name] = f[1]
						mu.Unlock()
						say(p, "Te has registrado correctamente")
						return
					case registered && f[0] == "login" && len(f) == 2 && f[1] == pass:
						say(p, "Sesión iniciada")
						return
					case registered && f[0] == "login":
						say(p, "Contraseña incorrecta")
					}
				}
				time.Sleep(200 * time.Millisecond)
			}
			_ = p.Kick("Tiempo de login agotado")
		}()
	}
}

func say(p *fakeserver.Player, text string) {
	_ = p.Send(play.SystemChat{Content: types.Text(chat.Text(text))})
}
