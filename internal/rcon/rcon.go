// Package rcon: la consola del servidor de Minecraft, para cuando el servidor es
// tuyo (el local de compose.yaml). Con ella el panel manda comandos de
// servidor (/gamemode, /time, /op…) y hace op a los dueños.
//
//	MCBOT_RCON=minecraft:25575 MCBOT_RCON_PASSWORD=… cubit …
//
// Sin MCBOT_RCON, nada de esto: el bot es un jugador más.
package rcon

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cubit/internal/mc"
	"cubit/internal/properties"
)

var mu sync.Mutex // (una orden a la vez)

// override: la consola del servidor nativo (sin Docker), que el hub arranca él
// mismo (internal/mcserver/native.go). MCBOT_RCON, si está, manda.
var override struct {
	sync.Mutex
	addr, password string
}

// SetOverride pone (o quita, con "") la consola de un servidor que lleva el propio programa.
func SetOverride(addr, password string) {
	override.Lock()
	override.addr, override.password = addr, password
	override.Unlock()
}

// Addr: la dirección de la consola (MCBOT_RCON; "" = sin RCON).
func Addr() string {
	if a := os.Getenv("MCBOT_RCON"); a != "" {
		return a
	}
	override.Lock()
	defer override.Unlock()
	return override.addr
}

// Cmd manda un comando a la consola del servidor y devuelve lo que dice.
func Cmd(cmd string) (string, error) {
	out, err := Cmds(cmd)
	if err != nil {
		return "", err
	}
	return out[0], nil
}

// Cmds manda varios comandos por la misma conexión.
func Cmds(cmds ...string) ([]string, error) {
	addr := Addr()
	if addr == "" {
		return nil, errors.New("sin RCON (MCBOT_RCON)")
	}
	mu.Lock()
	defer mu.Unlock()
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	if err := write(c, 1, 3, password()); err != nil {
		return nil, err
	}
	id, _, err := read(c)
	if err != nil {
		return nil, err
	}
	if id == -1 {
		return nil, errors.New("RCON: contraseña incorrecta")
	}
	out := make([]string, len(cmds))
	for i, cmd := range cmds {
		if err := write(c, int32(i+2), 2, strings.TrimPrefix(strings.TrimSpace(cmd), "/")); err != nil {
			return nil, err
		}
		_, body, err := read(c)
		if err != nil {
			return nil, err
		}
		out[i] = stripColors(body)
	}
	return out, nil
}

// password: la de server.properties si el bot la ve (MCBOT_SERVER_DIR;
// es la que vale), si no MCBOT_RCON_PASSWORD.
func password() string {
	if dir := os.Getenv("MCBOT_SERVER_DIR"); dir != "" {
		if p, err := properties.Read(filepath.Join(dir, "server.properties")); err == nil && p["rcon.password"] != "" {
			return p["rcon.password"]
		}
	}
	if p := os.Getenv("MCBOT_RCON_PASSWORD"); p != "" {
		return p
	}
	override.Lock()
	defer override.Unlock()
	return override.password
}

func write(w io.Writer, id, typ int32, body string) error {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, int32(10+len(body)))
	_ = binary.Write(&b, binary.LittleEndian, id)
	_ = binary.Write(&b, binary.LittleEndian, typ)
	b.WriteString(body)
	b.Write([]byte{0, 0})
	_, err := w.Write(b.Bytes())
	return err
}

func read(r io.Reader) (id int32, body string, err error) {
	var n int32
	if err = binary.Read(r, binary.LittleEndian, &n); err != nil {
		return
	}
	if n < 10 || n > 1<<20 {
		return 0, "", fmt.Errorf("RCON: paquete raro (%d bytes)", n)
	}
	buf := make([]byte, n)
	if _, err = io.ReadFull(r, buf); err != nil {
		return
	}
	id = int32(binary.LittleEndian.Uint32(buf))
	return id, string(bytes.TrimRight(buf[8:], "\x00")), nil
}

// stripColors quita los códigos § de los colores.
func stripColors(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if strings.HasPrefix(s[i:], "§") {
			i += len("§") // (y el carácter del color)
			continue
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

var (
	knownMu sync.Mutex
	known   []string
)

// Known: los últimos dueños que se pidió hacer op (el servidor nativo, al
// arrancar por primera vez, se los hace).
func Known() []string {
	knownMu.Lock()
	defer knownMu.Unlock()
	return append([]string(nil), known...)
}

// OpOwners: con RCON, los dueños son op en el servidor (para /gamemode…).
// Reintenta un rato: al arrancar todo junto, el servidor tarda en abrirlo.
func OpOwners(owners []string) {
	knownMu.Lock()
	known = append([]string(nil), owners...)
	knownMu.Unlock()
	if Addr() == "" {
		return
	}
	go func() {
		for try := 0; try < 20; try++ {
			ok := true
			for _, o := range owners {
				if o == "*" || !mc.ValidName.MatchString(o) {
					continue
				}
				out, err := Cmd("op " + o)
				if err != nil {
					ok = false
					break
				}
				if out != "" && !strings.Contains(out, "Nothing changed") {
					log.Printf("[servidor] %s", out)
				}
			}
			if ok {
				return
			}
			time.Sleep(15 * time.Second)
		}
		log.Printf("RCON: no responde en %s (no hago op a los dueños)", Addr())
	}()
}
