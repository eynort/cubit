package mcserver

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"cubit/internal/properties"
)

func worldDir(t *testing.T) string {
	dir := t.TempDir()
	t.Setenv("MCBOT_SERVER_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte("motd=Cubit\nlevel-seed=\nrcon.password=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mk := func(p string) {
		if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk("world/region")
	return dir
}

func seedOf(t *testing.T, dir string) string {
	p, err := properties.Read(filepath.Join(dir, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	return p["level-seed"]
}

func TestCheckSeed(t *testing.T) {
	for in, ok := range map[string]bool{"123": true, "-4172144997902289642": true, "mi mundo": true, " 7 ": true, "a\nb": false, strings.Repeat("x", 65): false} {
		if _, err := checkSeed(in); (err == nil) != ok {
			t.Errorf("%q: error %v", in, err)
		}
	}
	a, _ := checkSeed("")
	b, _ := checkSeed("")
	if a == "" || a == b {
		t.Errorf("al azar: %q y %q", a, b)
	}
	if s, _ := checkSeed(" 7 "); s != "7" {
		t.Errorf("sin espacios: %q", s)
	}
}

// TestNewWorldOff: apagado, el mundo pasa a world.anterior (solo el último) y
// la semilla queda puesta; sigue apagado.
func TestNewWorldOff(t *testing.T) {
	dir := worldDir(t)
	if err := os.WriteFile(filepath.Join(dir, powerMarker), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := newWorld("123"); err != nil {
		t.Fatal(err)
	}
	if seedOf(t, dir) != "123" {
		t.Errorf("semilla %q", seedOf(t, dir))
	}
	if _, err := os.Stat(filepath.Join(dir, "world.anterior", "region")); err != nil {
		t.Errorf("no está la copia: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "world")); !os.IsNotExist(err) {
		t.Errorf("el mundo viejo sigue: %v", err)
	}
	if !Off() {
		t.Error("lo encendió")
	}
	// otra vez: el mundo de esta vez (lo generaría el servidor) sustituye a la copia
	_ = os.MkdirAll(filepath.Join(dir, "world", "nuevo"), 0o755)
	seed, err := newWorld("")
	if err != nil {
		t.Fatal(err)
	}
	if seed == "" || seedOf(t, dir) != seed {
		t.Errorf("al azar: %q / %q", seed, seedOf(t, dir))
	}
	if _, err := os.Stat(filepath.Join(dir, "world.anterior", "nuevo")); err != nil {
		t.Errorf("la copia no es la del último mundo: %v", err)
	}
}

// fakeServer: una consola RCON que, al "stop", se cierra (el servidor parado)
// y vuelve en cuanto no está la marca de apagado (Docker lo arranca).
type fakeServer struct {
	t      *testing.T
	addr   string
	marker string
	mu     sync.Mutex
	ln     net.Listener
	cmds   []string
}

func (f *fakeServer) listen() {
	ln, err := net.Listen("tcp", f.addr)
	if err != nil {
		f.t.Errorf("escuchar: %v", err)
		return
	}
	f.mu.Lock()
	f.ln, f.addr = ln, ln.Addr().String()
	f.mu.Unlock()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
}

func (f *fakeServer) serve(c net.Conn) {
	defer c.Close()
	for {
		var n int32
		if binary.Read(c, binary.LittleEndian, &n) != nil {
			return
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(c, buf); err != nil {
			return
		}
		id := int32(binary.LittleEndian.Uint32(buf))
		body := string(bytes.TrimRight(buf[8:], "\x00"))
		var out bytes.Buffer
		_ = binary.Write(&out, binary.LittleEndian, int32(10))
		_ = binary.Write(&out, binary.LittleEndian, id)
		_ = binary.Write(&out, binary.LittleEndian, int32(0))
		out.Write([]byte{0, 0})
		_, _ = c.Write(out.Bytes())
		if binary.LittleEndian.Uint32(buf[4:]) == 2 {
			f.mu.Lock()
			f.cmds = append(f.cmds, body)
			f.mu.Unlock()
			if body == "stop" {
				f.mu.Lock()
				_ = f.ln.Close()
				f.mu.Unlock()
				go func() { // Docker: arranca en cuanto no hay marca
					time.Sleep(500 * time.Millisecond)
					for {
						if _, err := os.Stat(f.marker); os.IsNotExist(err) {
							f.listen()
							return
						}
						time.Sleep(100 * time.Millisecond)
					}
				}()
				return
			}
		}
	}
}

// TestNewWorldOnline: encendido, avisa, lo para, cambia el mundo con el
// servidor parado y lo deja arrancar; mientras, no se ve como apagado.
func TestNewWorldOnline(t *testing.T) {
	dir := worldDir(t)
	defer func(d time.Duration) { worldSettle = d }(worldSettle)
	worldSettle = 0
	f := &fakeServer{t: t, addr: "127.0.0.1:0", marker: filepath.Join(dir, powerMarker)}
	f.listen()
	t.Setenv("MCBOT_RCON", f.addr)
	seed, err := newWorld("99")
	if err != nil || seed != "99" {
		t.Fatalf("%q %v", seed, err)
	}
	if Off() {
		t.Error("se ve apagado mientras se rehace")
	}
	if _, err := newWorld("1"); err != errRestarting {
		t.Errorf("dos a la vez: %v", err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for currentRestart().Active && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
	}
	if st := currentRestart(); st.Active || st.Failed != "" {
		t.Fatalf("no volvió: %+v", st)
	}
	if seedOf(t, dir) != "99" {
		t.Errorf("semilla %q", seedOf(t, dir))
	}
	if _, err := os.Stat(filepath.Join(dir, "world.anterior", "region")); err != nil {
		t.Errorf("no está la copia: %v", err)
	}
	if _, err := os.Stat(f.marker); !os.IsNotExist(err) {
		t.Errorf("la marca se quedó: %v", err)
	}
	f.mu.Lock()
	cmds := strings.Join(f.cmds, " | ")
	f.mu.Unlock()
	if !strings.Contains(cmds, "say ") || !strings.Contains(cmds, "stop") {
		t.Errorf("comandos: %s", cmds)
	}
}
