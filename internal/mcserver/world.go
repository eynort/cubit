package mcserver

// Un mundo nuevo para el servidor de pruebas, con la semilla que se diga o una
// al azar. Es un reinicio (restart.go) con un paso más en medio: con el
// servidor parado, la carpeta del mundo pasa a <mundo>.anterior (solo se
// guarda el último, por si acaso) y level-seed se cambia en server.properties.
//
// Para que Docker no lo vuelva a arrancar antes de tiempo se usa la misma
// marca que al apagarlo (power.go): el contenedor espera mientras está.

import (
	"crypto/rand"
	"encoding/binary"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"cubit/internal/i18n"
	"cubit/internal/rcon"
)

// srvWorld: se está rehaciendo el mundo (la marca de apagado es suya).
var srvWorld atomic.Bool

// worldExtra: lo que tarda de más generar un mundo nuevo frente a un reinicio.
const worldExtra = 25 * time.Second

// worldSettle: con la consola ya cerrada, lo que se espera a que el servidor
// acabe de guardar y salga antes de mover su carpeta.
var worldSettle = 8 * time.Second

// randomSeed: una semilla al azar (un número, como las que salen en /seed).
func randomSeed() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return strconv.FormatInt(int64(binary.LittleEndian.Uint64(b[:])), 10)
}

// checkSeed: la semilla tal cual se guarda ("" = al azar). Minecraft acepta un
// número o un texto (que convierte en número).
func checkSeed(seed string) (string, error) {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return randomSeed(), nil
	}
	if len(seed) > 64 || !utf8.ValidString(seed) || strings.ContainsAny(seed, "\r\n\\") {
		return "", i18n.Errorf("the seed: a number or a text of one line, 64 characters at most", "la semilla: un número o un texto de una línea, de 64 caracteres como mucho")
	}
	return seed, nil
}

// levelDir: la carpeta del mundo (level-name de server.properties, "world"
// si no se dice).
func levelDir() (string, error) {
	p, err := readServerProps()
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(p["level-name"])
	if name == "" {
		name = "world"
	}
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return "", i18n.Errorf("level-name in server.properties is not a plain folder name", "level-name de server.properties no es un nombre de carpeta normal")
	}
	return filepath.Join(serverDir(), name), nil
}

// swapWorld aparta el mundo (a .anterior, quitando el anterior de antes) y
// pone la semilla nueva. Con el servidor parado.
func swapWorld(seed string) error {
	dir, err := levelDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err == nil {
		bak := dir + ".anterior"
		if err := os.RemoveAll(bak); err != nil {
			return err
		}
		if err := os.Rename(dir, bak); err != nil {
			return err
		}
	}
	if err := writeServerProps(map[string]string{"level-seed": seed}); err != nil {
		return err
	}
	log.Printf("[panel] mundo nuevo con la semilla %s (el anterior queda en %s.anterior)", seed, filepath.Base(dir))
	return nil
}

// newWorld rehace el mundo del servidor de pruebas con esa semilla ("" = al
// azar) y devuelve la que usa. Apagado, lo deja hecho y apagado; encendido,
// avisa, lo para, lo cambia y lo vuelve a arrancar.
func newWorld(seed string) (string, error) {
	if serverDir() == "" {
		return "", i18n.Errorf("only the test server (the local Docker one) can get a new world", "solo el servidor de pruebas (el local de Docker) puede tener un mundo nuevo")
	}
	seed, err := checkSeed(seed)
	if err != nil {
		return "", err
	}
	if _, err := levelDir(); err != nil {
		return "", err
	}
	srvRestart.Lock()
	busy := !srvRestart.since.IsZero()
	srvRestart.Unlock()
	if busy || srvWorld.Load() {
		return "", errRestarting
	}
	if Off() {
		if pendingPower("apagar") {
			return "", i18n.Errorf("wait until it has stopped", "espera a que termine de apagarse")
		}
		return seed, swapWorld(seed)
	}
	if _, err := rcon.Cmd("say Mundo nuevo en 5 segundos: el servidor se reinicia"); err != nil {
		return "", err
	}
	if err := os.WriteFile(powerMarkerPath(), []byte("mundo nuevo desde el panel: se quita sola en cuanto está hecho\n"), 0o644); err != nil {
		return "", err
	}
	srvWorld.Store(true)
	srvRestart.Lock()
	srvRestart.since, srvRestart.phase, srvRestart.expect = time.Now(), phaseWarn, lastRestartTook()+worldExtra
	srvRestart.backAt, srvRestart.failed = time.Time{}, ""
	srvRestart.Unlock()
	log.Printf("[panel] mundo nuevo en el servidor de pruebas (semilla %s)", seed)
	go watchRestart(func() {
		defer srvWorld.Store(false)
		time.Sleep(worldSettle)
		if err := swapWorld(seed); err != nil {
			log.Printf("[panel] no pude rehacer el mundo (%v): arranca con el de antes", err)
		}
		if err := os.Remove(powerMarkerPath()); err != nil && !os.IsNotExist(err) {
			log.Printf("[panel] no pude quitar %s: %v (bórrala para que arranque)", powerMarker, err)
		}
	})
	return seed, nil
}
