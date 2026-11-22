package hub

// ViaProxy en el hub: qué versión tiene cada servidor (Server List Ping, con
// caché), si hace falta ViaProxy para entrar, y —si se da la ruta a su .jar—
// el proceso de ViaProxy (uno solo para todos los servidores).
//
// El ajuste es uno (--viaproxy o MCBOT_VIAPROXY):
//   - host:puerto: un ViaProxy que ya está en marcha (el servicio "viaproxy" de
//     compose.yaml, o uno a mano). El hub solo se lo dice a los bots.
//   - ruta a ViaProxy-<versión>.jar: el hub lanza "java -jar" cuando algún
//     servidor lo necesita, escuchando solo en 127.0.0.1.

import (
	"context"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot"

	"cubit/internal/mc"
	"cubit/internal/viaproxy"
)

// ViaProxy: el ajuste (vacío = sin ViaProxy). Lo pone main antes de Run.
var ViaProxy string

// Estados de ServerView.Via.
const (
	ViaNone        = ""            // misma versión (o aún no se sabe)
	ViaOK          = "ok"          // otra versión y ViaProxy disponible
	ViaUnavailable = "unavailable" // otra versión y no hay ViaProxy (o no responde)
)

type verInfo struct {
	st   mc.Status
	err  error
	at   time.Time
	busy bool
}

var vi = struct {
	mu   sync.Mutex
	seen map[string]verInfo // por dirección
	cmd  *exec.Cmd
	up   bool // el proceso sigue en marcha
}{seen: map[string]verInfo{}}

const verTTL = 5 * time.Minute

// viaSetting: host:puerto de ViaProxy si el ajuste lo es, o el del que lanza el hub.
func viaSetting() (addr, jar string) {
	v := strings.TrimSpace(ViaProxy)
	if v == "" {
		v = os.Getenv("MCBOT_VIAPROXY")
	}
	switch {
	case v == "":
		return "", ""
	case viaproxy.IsJar(v):
		return viaproxy.DefaultBind, v
	}
	return v, ""
}

// viaReachable: ViaProxy acepta conexiones.
func viaReachable(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 1500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// peek: lo último que se sabe de la versión de addr, sin bloquear; si falta o
// caducó, lo pregunta en segundo plano.
func peek(addr string) (verInfo, bool) {
	vi.mu.Lock()
	defer vi.mu.Unlock()
	v, ok := vi.seen[addr]
	if (!ok || time.Since(v.at) > verTTL) && !v.busy {
		v.busy = true
		vi.seen[addr] = v
		go func() { refresh(context.Background(), addr, 6*time.Second) }()
	}
	return v, ok && !v.at.IsZero()
}

// refresh pregunta ahora (bloquea hasta timeout) y guarda el resultado.
func refresh(ctx context.Context, addr string, timeout time.Duration) verInfo {
	st, err := mc.Ping(ctx, addr, timeout)
	v := verInfo{st: st, err: err, at: time.Now()}
	vi.mu.Lock()
	vi.seen[addr] = v
	vi.mu.Unlock()
	return v
}

// viaState: el estado para el panel de un servidor ya sondeado.
func viaState(v verInfo) (detected, via string) {
	if v.err != nil {
		return "", ViaNone
	}
	detected = v.st.Version
	if detected == "" {
		detected = v.st.Name
	}
	addr, jar := viaSetting()
	switch viaproxy.Decide(bot.ProtocolVersion, v.st, firstNonEmpty(addr, jar)).Mode {
	case viaproxy.Via:
		if jar == "" && !viaReachable(addr) {
			return detected, ViaUnavailable
		}
		return detected, ViaOK
	case viaproxy.Unavailable:
		return detected, ViaUnavailable
	}
	return detected, ViaNone
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// serverVersion: lo que enseña el panel de un servidor externo (sin bloquear).
func serverVersion(addr string) (detected, via string) {
	v, ok := peek(addr)
	if !ok {
		return "", ViaNone
	}
	return viaState(v)
}

// probeNow: como serverVersion pero preguntando ahora (al dar de alta un servidor).
func probeNow(ctx context.Context, addr string) (detected, via string) {
	return viaState(refresh(ctx, addr, 4*time.Second))
}

// viaFor: la dirección de ViaProxy que se le da a un bot que va a addr ("" si
// no hace falta o no hay). Con .jar, lo lanza si el servidor es de otra versión.
func (h *hub) viaFor(addr string) string {
	va, jar := viaSetting()
	if va == "" {
		return ""
	}
	if jar == "" {
		return va
	}
	v, ok := peek(addr)
	if !ok || v.err != nil {
		return va // (no se sabe aún: el bot lo comprueba él; si hace falta y ya está en marcha, entra)
	}
	if viaproxy.Decide(bot.ProtocolVersion, v.st, va).Mode == viaproxy.Via {
		h.startVia(jar, va)
	}
	return va
}

// startVia lanza ViaProxy (una vez) con el hub como dueño: acaba con él.
func (h *hub) startVia(jar, bind string) {
	vi.mu.Lock()
	defer vi.mu.Unlock()
	if vi.up {
		return
	}
	prog, args := viaproxy.Config{Jar: jar, Bind: bind}.Command()
	ctx := h.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, prog, args...)
	cmd.Dir = os.TempDir()
	if d, err := os.UserConfigDir(); err == nil {
		dir := d + string(os.PathSeparator) + "cubit-viaproxy" // (aquí guarda ViaProxy su viaproxy.yml y cuentas)
		if os.MkdirAll(dir, 0o700) == nil {
			cmd.Dir = dir
		}
	}
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		log.Printf("hub: no pude lanzar ViaProxy (%s %s): %v", prog, jar, err)
		return
	}
	vi.cmd, vi.up = cmd, true
	log.Printf("hub: ViaProxy lanzado (pid %d) en %s", cmd.Process.Pid, bind)
	go func() {
		_ = cmd.Wait()
		vi.mu.Lock()
		vi.up = false
		vi.mu.Unlock()
		log.Printf("hub: ViaProxy terminó")
	}()
	// (el bot reintenta solo si ViaProxy aún no escucha)
}
