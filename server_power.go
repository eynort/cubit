package main

// Apagar y encender el servidor de pruebas (el local de compose.yaml) desde
// el panel, sin tocar Docker: Docker lo vuelve a arrancar siempre (restart:
// unless-stopped, que es lo que usa el reinicio), así que para apagarlo se
// deja una marca en su carpeta (powerMarker) y se le manda "stop"; al volver a
// arrancar, el contenedor ve la marca y espera sin levantar Minecraft hasta
// que se quita (compose.yaml: entrypoint). Encender = quitar la marca.
//
// Además, a dónde está conectado el bot: al servidor de pruebas, o a otro
// cualquiera que puso el jugador (Ajustes → Servidor).

import (
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	powerMarker   = ".mcbot-apagado"
	bootTimesFile = "arranque_servidor.txt" // lo que tardó en arrancar la última vez (s)
)

// lastBootTook: lo que tardó en arrancar la última vez que se encendió (o,
// sin medida, lo del último reinicio).
func lastBootTook() time.Duration {
	if b, err := os.ReadFile(dataFile(bootTimesFile)); err == nil {
		if s, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64); err == nil && s >= 1 && s < restartGiveUp.Seconds() {
			return time.Duration(s * float64(time.Second))
		}
	}
	return lastRestartTook()
}

func powerMarkerPath() string { return filepath.Join(serverDir(), powerMarker) }

// serverOff: la marca de apagado está puesta.
func serverOff() bool {
	if serverDir() == "" {
		return false
	}
	_, err := os.Stat(powerMarkerPath())
	return err == nil
}

var srvPower struct {
	sync.Mutex
	since time.Time // el último apagar/encender pedido
	what  string    // "apagar" o "encender"
}

// powerState: "on", "off", "stopping" (apagándose: la consola aún contesta)
// o "starting" (encendiéndose: aún no contesta). online: la consola contesta.
func powerState(online bool) string {
	switch off := serverOff(); {
	case off && online:
		return "stopping"
	case off:
		return "off"
	case !online && pendingPower("encender"):
		return "starting"
	}
	return "on"
}

func pendingPower(what string) bool {
	srvPower.Lock()
	defer srvPower.Unlock()
	return srvPower.what == what && time.Since(srvPower.since) < restartGiveUp
}

// stopServer apaga el servidor de pruebas (hasta que se encienda).
func stopServer() error {
	if serverDir() == "" {
		return bi("only the test server (the local Docker one) can be stopped from here", "solo el servidor de pruebas (el local de Docker) se puede apagar desde aquí")
	}
	if serverOff() {
		return bi("it is already off", "ya está apagado")
	}
	srvRestart.Lock()
	busy := !srvRestart.since.IsZero()
	srvRestart.Unlock()
	if busy {
		return errRestarting
	}
	if err := os.WriteFile(powerMarkerPath(), []byte("apagado desde el panel del bot: bórralo (o Encender en el panel) para que arranque\n"), 0o644); err != nil {
		return err
	}
	srvPower.Lock()
	srvPower.since, srvPower.what = time.Now(), "apagar"
	srvPower.Unlock()
	log.Print("[panel] apago el servidor de pruebas")
	go func() {
		_, _ = rconCmd("say El servidor se apaga en 5 segundos")
		time.Sleep(warnDelay)
		_, _ = rconCmd("stop")
	}()
	return nil
}

// startServer enciende el servidor de pruebas (quita la marca) y avisa al
// bot de que vuelva a probar a entrar ya, sin esperar a su siguiente intento.
func startServer() error {
	if serverDir() == "" {
		return bi("only the test server (the local Docker one) can be started from here", "solo el servidor de pruebas (el local de Docker) se puede encender desde aquí")
	}
	if !serverOff() {
		return bi("it is already on", "ya está encendido")
	}
	if err := os.Remove(powerMarkerPath()); err != nil {
		return err
	}
	srvPower.Lock()
	srvPower.since, srvPower.what = time.Now(), "encender"
	srvPower.Unlock()
	log.Printf("[panel] enciendo el servidor de pruebas (la última vez tardó %s en arrancar)", lastBootTook().Round(time.Second))
	start := time.Now()
	go func() { // cuánto tarda (para la próxima cuenta atrás) y, en cuanto conteste, el bot entra
		for i := 0; i < int(restartGiveUp/time.Second); i++ {
			time.Sleep(time.Second)
			if _, err := rconCmd("list"); err == nil {
				took := time.Since(start)
				_ = os.WriteFile(dataFile(bootTimesFile), []byte(strconv.FormatFloat(took.Seconds(), 'f', 1, 64)+"\n"), 0o644)
				log.Printf("[panel] el servidor de pruebas está en línea (tardó %s)", took.Round(time.Second))
				serverUpHook()
				return
			}
		}
	}()
	return nil
}

// ---- dónde está el bot ----

// testServerAddr: el servidor de pruebas visto desde el bot (MCBOT_TEST_SERVER,
// o el host de la consola con el puerto de juego).
func testServerAddr() string {
	if a := os.Getenv("MCBOT_TEST_SERVER"); a != "" {
		return a
	}
	if r := rconAddr(); r != "" && serverDir() != "" {
		return hostOf(r)
	}
	return ""
}

// botOnTestServer: el bot está puesto para entrar en el servidor de pruebas.
func botOnTestServer() bool {
	t := testServerAddr()
	if t == "" {
		return false
	}
	h, p := splitAddr(serverAddr)
	th, tp := splitAddr(t)
	return strings.EqualFold(h, th) && p == tp
}

// splitAddr: host y puerto (25565 si no lo lleva).
func splitAddr(a string) (string, string) {
	if h, p, err := net.SplitHostPort(a); err == nil {
		return h, p
	}
	return strings.Trim(a, "[]"), "25565"
}

// target: lo que ve el panel de a dónde está conectado el bot.
type target struct {
	Kind      string `json:"kind"`                // "test" (el de pruebas) o "external" (otro)
	Addr      string `json:"addr"`                // a dónde está puesto
	Connected bool   `json:"connected"`           // dentro ahora mismo
	TestAddr  string `json:"test_addr,omitempty"` // el de pruebas ("" = no hay)
}

func currentTarget() target {
	t := target{Kind: "external", Addr: serverAddr, TestAddr: testServerAddr()}
	if botOnTestServer() {
		t.Kind = "test"
	}
	if b := current.Load(); b != nil {
		t.Connected = b.ctx.Err() == nil && !b.connectedAt.IsZero()
	}
	return t
}

// ---- volver a entrar ya ----

var reconnectNow = make(chan struct{}, 1)

// serverUpHook: el servidor de pruebas acaba de encenderse (el hub lo cambia:
// avisa a sus bots).
var serverUpHook = func() {
	if botOnTestServer() {
		reconnectSoon()
	}
}

// reconnectSoon: si el bot está esperando para volver a entrar, que lo
// intente ya.
func reconnectSoon() {
	select {
	case reconnectNow <- struct{}{}:
	default:
	}
}
