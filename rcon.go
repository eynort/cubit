package main

// RCON: la consola del servidor de Minecraft, para cuando el servidor es
// tuyo (el local de compose.yaml). Con ella el panel manda comandos de
// servidor (/gamemode, /time, /op…) y hace op a los dueños.
//
//	MCBOT_RCON=minecraft:25575 MCBOT_RCON_PASSWORD=… cubit …
//
// Sin MCBOT_RCON, nada de esto: el bot es un jugador más.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

var rconMu sync.Mutex // (una orden a la vez)

func rconAddr() string { return os.Getenv("MCBOT_RCON") }

// rconCmd manda un comando a la consola del servidor y devuelve lo que dice.
func rconCmd(cmd string) (string, error) {
	out, err := rconCmds(cmd)
	if err != nil {
		return "", err
	}
	return out[0], nil
}

// rconCmds manda varios comandos por la misma conexión.
func rconCmds(cmds ...string) ([]string, error) {
	addr := rconAddr()
	if addr == "" {
		return nil, errors.New("sin RCON (MCBOT_RCON)")
	}
	rconMu.Lock()
	defer rconMu.Unlock()
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	if err := rconWrite(c, 1, 3, rconPassword()); err != nil {
		return nil, err
	}
	id, _, err := rconRead(c)
	if err != nil {
		return nil, err
	}
	if id == -1 {
		return nil, errors.New("RCON: contraseña incorrecta")
	}
	out := make([]string, len(cmds))
	for i, cmd := range cmds {
		if err := rconWrite(c, int32(i+2), 2, strings.TrimPrefix(strings.TrimSpace(cmd), "/")); err != nil {
			return nil, err
		}
		_, body, err := rconRead(c)
		if err != nil {
			return nil, err
		}
		out[i] = stripMCColors(body)
	}
	return out, nil
}

// rconPassword: la de server.properties si el bot la ve (MCBOT_SERVER_DIR;
// es la que vale), si no MCBOT_RCON_PASSWORD.
func rconPassword() string {
	if p, err := readServerProps(); err == nil && p["rcon.password"] != "" {
		return p["rcon.password"]
	}
	return os.Getenv("MCBOT_RCON_PASSWORD")
}

func rconWrite(w io.Writer, id, typ int32, body string) error {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, int32(10+len(body)))
	_ = binary.Write(&b, binary.LittleEndian, id)
	_ = binary.Write(&b, binary.LittleEndian, typ)
	b.WriteString(body)
	b.Write([]byte{0, 0})
	_, err := w.Write(b.Bytes())
	return err
}

func rconRead(r io.Reader) (id int32, body string, err error) {
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

// stripMCColors quita los códigos § de los colores.
func stripMCColors(s string) string {
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

// opOwners: con RCON, los dueños son op en el servidor (para /gamemode…).
// Reintenta un rato: al arrancar todo junto, el servidor tarda en abrirlo.
func opOwners(owners []string) {
	if rconAddr() == "" {
		return
	}
	go func() {
		for try := 0; try < 20; try++ {
			ok := true
			for _, o := range owners {
				if o == "*" || !validName.MatchString(o) {
					continue
				}
				out, err := rconCmd("op " + o)
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
		log.Printf("RCON: no responde en %s (no hago op a los dueños)", rconAddr())
	}()
}
