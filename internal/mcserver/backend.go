package mcserver

// Dos formas de tener el servidor de pruebas, con la misma lógica de encender,
// apagar, reiniciar y mundo nuevo (power.go, restart.go, world.go) porque las
// dos se gobiernan igual: la marca de apagado, "stop" por RCON y volver a
// arrancar cuando no hay marca.
//
//	docker  el de compose.yaml: Docker lo reinicia y su entrypoint espera
//	        mientras esté la marca (MCBOT_SERVER_DIR / MCBOT_RCON los pone compose)
//	native  sin Docker: el hub baja Java y el servidor oficial a <datos>/server
//	        y lo lleva él como proceso hijo; su supervisor (native.go) hace lo
//	        que Docker (arrancar de nuevo salvo que esté la marca)
//
// Se elige solo: con MCBOT_SERVER_DIR o MCBOT_RCON es Docker; si no, y el
// programa es un hub (EnableNative), nativo.

import (
	"os"
)

type backend interface {
	Kind() string // "docker" o "native"
	Dir() string  // la carpeta con server.properties ("" = aún no hay servidor)
	Ready() bool  // se puede encender (instalado y con la EULA aceptada)
}

type dockerBackend struct{}

func (dockerBackend) Kind() string  { return "docker" }
func (dockerBackend) Dir() string   { return os.Getenv("MCBOT_SERVER_DIR") }
func (b dockerBackend) Ready() bool { return b.Dir() != "" }

// selectKind: qué servidor toca según el entorno (lo que se prueba): "docker",
// "native" (si el programa lo permite: hub) o "" (ninguno: un servidor de fuera).
func selectKind(getenv func(string) string, nativeAllowed bool) string {
	if getenv("MCBOT_SERVER_DIR") != "" || getenv("MCBOT_RCON") != "" {
		return "docker"
	}
	if nativeAllowed && getenv("MCBOT_NO_NATIVE") == "" {
		return "native"
	}
	return ""
}

// activeBackend: el que vale ahora (nil = no hay servidor de pruebas propio).
func activeBackend() backend {
	switch selectKind(os.Getenv, nat.enabled()) {
	case "docker":
		return dockerBackend{}
	case "native":
		return nativeBackend{}
	}
	return nil
}

// nativeActive: el servidor de pruebas es el nativo (esté instalado o no).
func nativeActive() bool { return selectKind(os.Getenv, nat.enabled()) == "native" }

// needsSetup: el servidor nativo aún no está instalado (o no se aceptó la EULA).
func needsSetup() error {
	if nativeActive() && !(nativeBackend{}).Ready() {
		return errNeedsSetup
	}
	return nil
}
