package mcserver

// El servidor de pruebas sin Docker: el hub lo lleva como proceso hijo.
//
//	<datos>/server/            server.jar, server.properties, eula.txt, el mundo…
//	<datos>/server/java/       el Java que baja install.go
//	<datos>/native.json        la contraseña de RCON (al azar, no sale en el panel) y la memoria
//
// El supervisor hace lo que hace Docker con el contenedor: mientras el servidor
// esté listo y no esté la marca de apagado (power.go), lo mantiene arrancado.
// "stop" (apagar, reiniciar, mundo nuevo) lo manda el panel por RCON, igual que
// con Docker, y el supervisor lo vuelve a levantar o no según la marca.
//
//	MCBOT_SERVER_XMX=2G    la memoria del servidor (1G por defecto)
//	MCBOT_NO_NATIVE=1      no usar el servidor nativo
//	MCBOT_TEST_PORT        el puerto de juego (25565); MCBOT_SERVER_RCON_PORT el de RCON (25575)

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"cubit/internal/i18n"
	"cubit/internal/properties"
	"cubit/internal/rcon"
)

// EULAURL: la licencia que el jugador acepta (nunca se acepta sola).
const EULAURL = "https://aka.ms/MinecraftEULA"

const (
	defaultXmx      = "1G"
	defaultRCONPort = 25575
	nativeCfgFile   = "native.json"
)

var errNeedsSetup = i18n.Errorf("the test server is not set up yet: install it from the Server tab", "el servidor de pruebas aún no está instalado: instálalo en la pestaña Servidor")

type nativeCfg struct {
	RCONPassword string `json:"rcon_password"`
	Xmx          string `json:"xmx,omitempty"`
}

// nat: el estado del servidor nativo (todo detrás de mu).
var nat nativeState

type nativeState struct {
	mu      sync.Mutex
	on      bool
	dir     string // <datos>/server
	cfgPath string
	cfg     nativeCfg
	ctx     context.Context
	cancel  context.CancelFunc
	loop    chan struct{} // se cierra al acabar el supervisor
	inst    installState
	proc    procState
	logs    ring
	opped   bool // ya se hizo op a los dueños en esta vida del hub
	fresh   bool // se acaba de instalar: al responder, avisa a los bots
}

func (n *nativeState) enabled() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.on
}

type procState struct {
	running bool
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	done    chan struct{}
	started time.Time
}

// nativeBackend: ver backend.go.
type nativeBackend struct{}

func (nativeBackend) Kind() string  { return "native" }
func (b nativeBackend) Ready() bool { return nativeReady() }
func (b nativeBackend) Dir() string {
	if !nativeReady() {
		return ""
	}
	nat.mu.Lock()
	defer nat.mu.Unlock()
	return nat.dir
}

// supPoll: cada cuánto mira el supervisor si toca arrancar.
var supPoll = 2 * time.Second

// javaOverride: en las pruebas, un "java" falso (programa y argumentos de delante).
var javaOverride []string

// EnableNative activa el servidor nativo en dataDir/server si el entorno no es
// el de Docker (ver backend.go). El hub lo llama al arrancar; Shutdown, al salir.
func EnableNative(dataDir string) bool {
	if selectKind(os.Getenv, true) != "native" {
		return false
	}
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		abs = dataDir
	}
	nat.mu.Lock()
	if nat.on {
		nat.mu.Unlock()
		return true
	}
	nat.on, nat.dir, nat.cfgPath = true, filepath.Join(abs, "server"), filepath.Join(abs, nativeCfgFile)
	nat.ctx, nat.cancel = context.WithCancel(context.Background())
	ctx := nat.ctx
	loop := make(chan struct{})
	nat.loop = loop
	nat.mu.Unlock()
	if err := loadNativeCfg(); err != nil {
		log.Printf("[servidor] %s: %v", nativeCfgFile, err)
	}
	applyRCONOverride()
	go func() { defer close(loop); superviseLoop(ctx) }()
	return true
}

// Shutdown: al salir el hub, para el servidor con "stop" (y lo mata si no sale).
func Shutdown() {
	nat.mu.Lock()
	if !nat.on {
		nat.mu.Unlock()
		return
	}
	cancel, loop := nat.cancel, nat.loop
	done, stdin, running := nat.proc.done, nat.proc.stdin, nat.proc.running
	nat.mu.Unlock()
	cancel()
	defer func() { <-loop }()
	if !running {
		return
	}
	log.Print("[servidor] paro el servidor de pruebas")
	if _, err := rcon.Cmd("stop"); err != nil && stdin != nil {
		_, _ = io.WriteString(stdin, "stop\n")
	}
	select {
	case <-done:
	case <-time.After(shutdownWait):
		killProc()
		<-done
	}
}

var shutdownWait = 30 * time.Second

func killProc() {
	nat.mu.Lock()
	cmd := nat.proc.cmd
	nat.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// ---- configuración ----

var reXmx = regexp.MustCompile(`^[1-9][0-9]{0,5}[MmGg]$`)

func loadNativeCfg() error {
	nat.mu.Lock()
	defer nat.mu.Unlock()
	if b, err := os.ReadFile(nat.cfgPath); err == nil {
		if err := json.Unmarshal(b, &nat.cfg); err != nil {
			return err
		}
	}
	if nat.cfg.RCONPassword == "" {
		nat.cfg.RCONPassword = randomAlnum(24)
		b, _ := json.MarshalIndent(nat.cfg, "", "  ")
		if err := os.WriteFile(nat.cfgPath, append(b, '\n'), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func randomAlnum(n int) string {
	const chars = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	out := make([]byte, n)
	for i := range out {
		k, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		out[i] = chars[k.Int64()]
	}
	return string(out)
}

func nativeXmx() string {
	if v := os.Getenv("MCBOT_SERVER_XMX"); reXmx.MatchString(v) {
		return v
	}
	nat.mu.Lock()
	defer nat.mu.Unlock()
	if reXmx.MatchString(nat.cfg.Xmx) {
		return nat.cfg.Xmx
	}
	return defaultXmx
}

func envPort(name string, def int) int {
	if p, err := strconv.Atoi(os.Getenv(name)); err == nil && p > 0 && p < 65536 {
		return p
	}
	return def
}

func rconPort() int { return envPort("MCBOT_SERVER_RCON_PORT", defaultRCONPort) }

// applyRCONOverride: con el servidor listo, la consola es la suya.
func applyRCONOverride() {
	if !nativeReady() {
		rcon.SetOverride("", "")
		return
	}
	nat.mu.Lock()
	pw := nat.cfg.RCONPassword
	nat.mu.Unlock()
	rcon.SetOverride(net.JoinHostPort("127.0.0.1", strconv.Itoa(rconPort())), pw)
}

// ---- lo instalado ----

func nativeDir() string {
	nat.mu.Lock()
	defer nat.mu.Unlock()
	return nat.dir
}

// javaPath: el ejecutable de Java instalado ("" = no hay).
func javaPath() string {
	if len(javaOverride) > 0 {
		return javaOverride[0]
	}
	exe := "java"
	if runtime.GOOS == "windows" {
		exe = "java.exe"
	}
	for _, rel := range []string{filepath.Join("bin", exe), filepath.Join("Contents", "Home", "bin", exe)} {
		p := filepath.Join(nativeDir(), "java", rel)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func eulaAccepted() bool {
	p, err := properties.Read(filepath.Join(nativeDir(), "eula.txt"))
	return err == nil && p["eula"] == "true"
}

func nativeInstalled() bool {
	if javaPath() == "" {
		return false
	}
	st, err := os.Stat(filepath.Join(nativeDir(), "server.jar"))
	return err == nil && !st.IsDir()
}

// nativeReady: instalado y con la EULA aceptada.
func nativeReady() bool {
	if !nat.enabled() {
		return false
	}
	return nativeInstalled() && eulaAccepted()
}

// ensureProps: server.properties la primera vez (con lo que pide un servidor de
// pruebas del bot) y, siempre, la consola RCON con la contraseña de native.json.
func ensureProps(seed string) error {
	path := filepath.Join(nativeDir(), "server.properties")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte(defaultProps(seed)), 0o644); err != nil {
			return err
		}
	}
	nat.mu.Lock()
	pw := nat.cfg.RCONPassword
	nat.mu.Unlock()
	return properties.Update(path, map[string]string{
		"enable-rcon":           "true",
		"rcon.port":             strconv.Itoa(rconPort()),
		"rcon.password":         pw,
		"broadcast-rcon-to-ops": "false",
		"server-port":           strconv.Itoa(TestJoinPort()),
	})
}

func defaultProps(seed string) string {
	lines := []string{
		"#Minecraft server properties (Cubit)",
		"online-mode=false",
		"enforce-secure-profile=false",
		"motd=Cubit test server",
		"difficulty=easy",
		"gamemode=survival",
		"view-distance=8",
		"simulation-distance=6",
		"spawn-protection=0",
		"max-players=20",
		"level-seed=" + strings.NewReplacer("\n", "", "\r", "", "\\", "").Replace(seed),
	}
	return strings.Join(lines, "\n") + "\n"
}

// ---- el supervisor ----

// superviseLoop hace de Docker: arranca el servidor mientras esté listo y no
// esté la marca de apagado; si se cae, lo vuelve a arrancar (con espera si
// se cae nada más empezar, para no dar vueltas).
func superviseLoop(ctx context.Context) {
	fails := 0
	for ctx.Err() == nil {
		if !nativeReady() || nat.instBusy() || markerPresent() {
			sleepCtx(ctx, supPoll)
			continue
		}
		took, clean := runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if !clean && took < 20*time.Second {
			fails++
			wait := min(time.Duration(fails)*5*time.Second, time.Minute)
			if supPoll < time.Second {
				wait = supPoll
			}
			log.Printf("[servidor] el servidor de pruebas se paró a los %s: lo intento de nuevo en %s (mira sus logs)", took.Round(time.Second), wait)
			sleepCtx(ctx, wait)
		} else {
			fails = 0
			sleepCtx(ctx, supPoll)
		}
	}
}

func markerPresent() bool {
	_, err := os.Stat(powerMarkerPath())
	return err == nil
}

func sleepCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// runOnce lleva el servidor hasta que sale; devuelve lo que duró y si salió limpio (stop).
func runOnce(ctx context.Context) (time.Duration, bool) {
	if err := ensureProps(""); err != nil {
		log.Printf("[servidor] server.properties: %v", err)
		return 0, false
	}
	applyRCONOverride()
	var cmd *exec.Cmd
	args := []string{"-Xmx" + nativeXmx(), "-jar", "server.jar", "nogui"}
	if len(javaOverride) > 0 {
		cmd = exec.Command(javaOverride[0], append(append([]string{}, javaOverride[1:]...), args...)...)
	} else {
		cmd = exec.Command(javaPath(), args...)
	}
	cmd.Dir = nativeDir()
	setProcAttr(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, false
	}
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	start := time.Now()
	if err := cmd.Start(); err != nil {
		log.Printf("[servidor] no pude arrancar Java: %v", err)
		nat.logs.add("no se pudo arrancar Java: " + err.Error())
		return 0, false
	}
	done := make(chan struct{})
	nat.mu.Lock()
	nat.proc = procState{running: true, cmd: cmd, stdin: stdin, done: done, started: start}
	nat.mu.Unlock()
	log.Printf("[servidor] arranco el servidor de pruebas (java -Xmx%s, pid %d)", nativeXmx(), cmd.Process.Pid)
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			nat.logs.add(sc.Text())
		}
	}()
	go announceWhenUp(ctx, done)
	err = cmd.Wait()
	_ = pw.Close()
	nat.mu.Lock()
	nat.proc.running = false
	nat.mu.Unlock()
	close(done)
	log.Printf("[servidor] el servidor de pruebas ha salido (%v)", err)
	return time.Since(start), err == nil
}

// announceWhenUp: cuando la consola contesta por primera vez, hace op a los
// dueños (una vez por vida del hub) y, si es la primera vez tras instalarlo,
// avisa a los bots de que entren.
func announceWhenUp(ctx context.Context, done <-chan struct{}) {
	for i := 0; i < int(restartGiveUp/time.Second); i++ {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
		if _, err := rcon.Cmd("list"); err != nil {
			continue
		}
		nat.mu.Lock()
		opped, fresh := nat.opped, nat.fresh
		nat.opped, nat.fresh = true, false
		nat.mu.Unlock()
		if !opped {
			rcon.OpOwners(rcon.Known())
		}
		if fresh {
			OnUp()
		}
		return
	}
}

// ---- el registro ----

// ring: las últimas líneas que ha dicho el servidor.
type ring struct {
	mu    sync.Mutex
	lines []string
}

const ringMax = 500

func (r *ring) add(l string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, l)
	if len(r.lines) > ringMax {
		r.lines = append([]string(nil), r.lines[len(r.lines)-ringMax:]...)
	}
}

func (r *ring) tail(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	l := r.lines
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return append([]string{}, l...)
}

// serverLogs: lo último que ha dicho el servidor (lo de su proceso, o si no ha
// corrido en esta vida del hub, el final de logs/latest.log).
func serverLogs(n int) []string {
	if l := nat.logs.tail(n); len(l) > 0 {
		return l
	}
	dir := serverDir()
	if dir == "" {
		return []string{}
	}
	b, err := os.ReadFile(filepath.Join(dir, "logs", "latest.log"))
	if err != nil {
		return []string{}
	}
	if len(b) > 64<<10 {
		b = b[len(b)-64<<10:]
	}
	ls := strings.Split(strings.TrimRight(string(b), "\r\n"), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return ls
}

// ---- el estado que ve el panel ----

type nativeStatus struct {
	State    string `json:"state"` // none (sin instalar), installing, error, ready
	Ready    bool   `json:"ready"`
	EULAURL  string `json:"eula_url"`
	Version  string `json:"version"`
	Xmx      string `json:"xmx"`
	Step     string `json:"step,omitempty"` // resolve, server, java, extract, files
	StepN    int    `json:"step_n,omitempty"`
	StepOf   int    `json:"step_of,omitempty"`
	Done     int64  `json:"done,omitempty"`
	Total    int64  `json:"total,omitempty"`
	Java     int    `json:"java,omitempty"`
	Err      string `json:"error,omitempty"`
	JavaMB   int    `json:"java_mb"`   // lo que se calcula que baja (aprox.)
	ServerMB int    `json:"server_mb"` // idem
	Running  bool   `json:"running"`   // el proceso está vivo
}

func nativeStatusFor(lang func(error) string) *nativeStatus {
	if !nativeActive() {
		return nil
	}
	st := &nativeStatus{EULAURL: EULAURL, Version: serverVersion(), Xmx: nativeXmx(), JavaMB: 50, ServerMB: 60}
	st.Ready = nativeReady()
	st.State = "none"
	if st.Ready {
		st.State = "ready"
	}
	nat.mu.Lock()
	in := nat.inst
	st.Running = nat.proc.running
	nat.mu.Unlock()
	if in.busy {
		st.State = "installing"
	} else if in.err != nil && !st.Ready {
		st.State = "error"
	}
	if in.busy || in.err != nil {
		st.Step, st.StepN, st.StepOf, st.Done, st.Total, st.Java = in.step, in.stepN, in.stepOf, in.done, in.total, in.java
	}
	if in.err != nil && !in.busy {
		st.Err = lang(in.err)
	}
	return st
}

func (n *nativeState) instBusy() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.inst.busy
}
