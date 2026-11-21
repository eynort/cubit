package main

// El bot y el servidor de pruebas (internal/mcserver): lo que el paquete
// necesita saber de este bot. El hub (internal/hub) cambia IsBotName y OnUp
// por los suyos al arrancar.

import (
	"strings"

	"cubit/internal/mcserver"
)

func init() {
	mcserver.DataFile = dataFile
	mcserver.IsBotName = func(n string) bool { return strings.EqualFold(n, botName) }
	mcserver.Bot = func() (string, bool) {
		connected := false
		if b := current.Load(); b != nil {
			connected = b.ctx.Err() == nil && !b.connectedAt.IsZero()
		}
		return serverAddr, connected
	}
	mcserver.OnUp = func() {
		if botOnTestServer() {
			reconnectSoon()
		}
	}
}

// botOnTestServer: el bot está puesto para entrar en el servidor de pruebas.
func botOnTestServer() bool { return mcserver.IsTestServer(serverAddr) }

// ---- volver a entrar ya ----

var reconnectNow = make(chan struct{}, 1)

// reconnectSoon: si el bot está esperando para volver a entrar, que lo
// intente ya.
func reconnectSoon() {
	select {
	case reconnectNow <- struct{}{}:
	default:
	}
}
