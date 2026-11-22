package main

// --demo: probar el bot en 10 segundos sin servidor. Arranca un servidor de
// Minecraft falso dentro del propio programa (un mundo plano, el de los tests
// del kit), entra en él y abre la consola y el panel web. En ese mundo no
// pasa nada por sí solo (no hay mobs ni se rompen bloques): es para ver la
// consola, el autocompletado, el panel y moverse (/goto 10 ~ 10, /status…).
// Todo lo que guarda va a una carpeta temporal.

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/mj41/go-mc26-kit/bot/fakeserver"
)

var demoFlag = flag.Bool("demo", false, "probar el bot sin servidor: uno falso dentro del programa (mundo plano), con la consola y el panel en http://127.0.0.1:8080")

func runDemo() {
	s, err := fakeserver.Listen()
	if err != nil {
		log.Fatalf("--demo: %v", err)
	}
	s.KeepAlive = 15 * time.Second // (como un servidor de verdad: sin esto el bot se desconecta a los 20 s)
	s.World.Fill(-6, 64, 8, 6, 66, 8, "minecraft:stone_bricks") // un muro para ver /goto rodearlo
	s.World.Fill(4, 64, -3, 6, 64, -1, "minecraft:oak_planks")
	dir := filepath.Join(os.TempDir(), "cubit-demo")
	_ = os.MkdirAll(dir, 0o755)
	if err := os.Chdir(dir); err != nil {
		log.Fatalf("--demo: %v", err)
	}
	set := func(name, v string) { _ = flag.Set(name, v) }
	set("server", s.Addr)
	set("name", "Demo_Bot")
	set("owner", "Tester")
	set("reconnect", "false")
	if *monitorFlag == "" {
		set("monitor", "127.0.0.1:8080")
	}
	log.Printf("demo: servidor de prueba en %s (datos en %s). Prueba /help, /status, /goto 0 ~ 14, /inventory; Ctrl+C para salir", s.Addr, dir)
	go func() { // Tester saluda al entrar
		for i := 0; i < 100 && len(s.Players()) == 0; i++ {
			time.Sleep(100 * time.Millisecond)
		}
		time.Sleep(2 * time.Second)
		_ = s.Chat("Tester", "hola bot! prueba a escribir /help en tu consola")
	}()
}
