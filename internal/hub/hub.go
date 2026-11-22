// Package hub: el hub (--hub :8080): el panel web siempre encendido, aunque no haya ningún
// bot en marcha, para varios bots a la vez. Cada bot es este mismo programa
// en su propio proceso, con su carpeta (bots/<id>/: bot.json, datos/…) y su
// panel interno en 127.0.0.1; el hub los arranca, los para, los vuelve a
// arrancar si se caen, y reenvía el panel de cada uno en /b/<id>/…
//
//	/                   el panel (el mismo panel.html)
//	/api/state          los bots, el servidor de pruebas, si hace falta el asistente
//	/api/bots           crear un bot (POST)
//	/api/bots/<id>/…    start, stop, open, delete (POST; delete lo borra del todo)
//	/b/<id>/…           el panel de ese bot (json, log, cmd, settings…)
//	/server…            el servidor de pruebas (internal/mcserver)
//	/avatar/<nombre>    la cabeza de su skin (internal/avatar)
//
// Ligero (sin Docker): vale igual en un móvil, cada bot es un proceso más.
package hub

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"cubit/brand"
	"cubit/internal/ansi"
	"cubit/internal/avatar"
	"cubit/internal/config"
	"cubit/internal/i18n"
	"cubit/internal/mc"
	"cubit/internal/mcserver"
	"cubit/internal/rcon"
	"cubit/internal/web"
)

const (
	hubFile = "hub.json"
	botsDir = "bots"
)

// hubConfig es hub.json.
type hubConfig struct {
	Schema        int            `json:"schema"` // el formato (config.HubSchema)
	Token         string         `json:"token"`
	Bots          []string       `json:"bots"`                      // los ids, en orden
	Run           []string       `json:"run"`                       // los que estaban en marcha (se arrancan al empezar)
	Last          string         `json:"last,omitempty"`            // el último que se abrió en el panel
	Servers       []hubServer    `json:"servers,omitempty"`         // los externos (servers.go)
	NoUpdateCheck bool           `json:"no_update_check,omitempty"` // no mirar si hay versión nueva (por defecto sí)
	Discord       *discordConfig `json:"discord,omitempty"`         // la integración con Discord (discord.go; con el token, 0600)
}

type hubBot struct {
	id string

	mu      sync.Mutex
	cmd     *exec.Cmd
	port    int
	want    bool // tiene que estar en marcha (si se cae, se vuelve a arrancar)
	again   bool // se para para volver a arrancar enseguida (moverlo de servidor)
	started time.Time
	fails   int
	tail    []string // las últimas líneas de su salida (por qué no arranca)
	proxy   *httputil.ReverseProxy
}

type hub struct {
	mu   sync.Mutex
	cfg  hubConfig
	bots map[string]*hubBot
	ctx  context.Context
	opts Options

	discord *discordMgr
	events  broker   // los eventos de los bots (api.go)
	up      *updater // las actualizaciones (update.go)
}

// Options: lo que el hub toma del programa que lo arranca.
type Options struct {
	DefaultOwner string // el dueño que propone el asistente (--owner)
	Open         bool   // abrir el panel en el navegador (--open); si ya hay un hub, solo lo abre y sale
	Version      string // la versión de Cubit en marcha (vacío o "dev": compilación de desarrollo)
}

func (h *hub) save() {
	b, _ := json.MarshalIndent(h.cfg, "", "  ")
	tmp := hubFile + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err == nil {
		_ = os.Rename(tmp, hubFile)
	}
}

// Run: el modo hub, con el panel en addr (no vuelve hasta Ctrl-C).
func Run(addr string, opts Options) {
	if opts.Open && openRunning(addr) {
		log.Print("hub: ya había uno en marcha: abro el panel en el navegador")
		return
	}
	h := &hub{bots: map[string]*hubBot{}, opts: opts}
	h.initUpdates()
	if h.rollbackAtStart() {
		return // (solo vuelve si no se pudo ejecutar el binario de antes)
	}
	h.migrateConfigs()
	if b, err := os.ReadFile(hubFile); err == nil {
		if err := json.Unmarshal(b, &h.cfg); err != nil {
			log.Fatalf("%s: %v", hubFile, err)
		}
	}
	h.migrate()
	h.cfg.Schema = config.HubSchema
	if h.cfg.Token == "" {
		b := make([]byte, 12)
		_, _ = rand.Read(b)
		h.cfg.Token = hex.EncodeToString(b)
	}
	// los que hay en bots/ aunque no estén en la lista (copiados a mano)
	if ents, err := os.ReadDir(botsDir); err == nil {
		for _, e := range ents {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !slices.Contains(h.cfg.Bots, e.Name()) {
				if _, err := os.Stat(filepath.Join(botsDir, e.Name(), "bot.json")); err == nil {
					h.cfg.Bots = append(h.cfg.Bots, e.Name())
				}
			}
		}
	}
	for _, id := range h.cfg.Bots {
		h.bots[id] = &hubBot{id: id}
	}
	h.adoptServers()
	h.save()

	mcserver.IsBotName = func(n string) bool {
		for _, id := range h.ids() {
			if strings.EqualFold(h.info(id).Name, n) {
				return true
			}
		}
		return false
	}
	mcserver.OnUp = h.serverUp
	if mcserver.EnableNative(".") { // sin Docker: el servidor de pruebas lo lleva el hub (se instala desde el panel)
		defer mcserver.Shutdown()
	}

	ln, err := net.Listen("tcp", addr) // (antes que los bots: si no se puede, no se arranca nada)
	if err != nil {
		log.Fatalf("--hub: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	h.ctx, h.up.stop = ctx, stop
	h.startDiscord()
	current.Store(h)
	defer current.CompareAndSwap(h, nil)
	go h.pollEvents(ctx)
	for _, id := range h.cfg.Run {
		if b := h.bots[id]; b != nil {
			_ = h.start(b)
		}
	}
	host := ln.Addr().String()
	if hh, port, err := net.SplitHostPort(host); err == nil && (hh == "::" || hh == "0.0.0.0") {
		host = net.JoinHostPort(web.LocalIP(), port)
	}
	log.Printf("hub: panel en http://%s/?t=%s (%d bots)", host, h.cfg.Token, len(h.cfg.Bots))
	if opts.Open {
		openBrowser(localURL(addr, h.cfg.Token))
	}
	srv := &http.Server{Handler: h.routes(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	go h.up.chk.Loop(ctx)
	go h.confirmUpdate(ln.Addr().String())
	<-ctx.Done()
	log.Print("hub: paro los bots")
	h.mu.Lock()
	run := []string{}
	for _, id := range h.cfg.Bots {
		if b := h.bots[id]; b != nil {
			b.mu.Lock()
			if b.want {
				run = append(run, id)
			}
			b.mu.Unlock()
		}
	}
	h.cfg.Run = run // (se vuelven a arrancar la próxima vez)
	h.save()
	bots := make([]*hubBot, 0, len(h.bots))
	for _, b := range h.bots {
		bots = append(bots, b)
	}
	h.mu.Unlock()
	var wg sync.WaitGroup
	for _, b := range bots {
		wg.Add(1)
		go func() { defer wg.Done(); h.stopProc(b) }()
	}
	wg.Wait()
	_ = srv.Close()
	h.up.exit(h.opts.Version) // (si se pidió: vuelve a arrancar, ya con el binario nuevo)
}

// migrate: un bot.json suelto (de antes del hub) pasa a bots/<nombre>/, con
// sus datos; su clave del panel se queda (la URL de siempre sigue valiendo).
func (h *hub) migrate() {
	b, err := os.ReadFile("bot.json")
	if err != nil {
		return
	}
	var old config.Bot
	if json.Unmarshal(b, &old) != nil || old.Name == "" {
		return
	}
	id := h.newID(old.Name)
	dir := filepath.Join(botsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("hub: no pude migrar bot.json: %v", err)
		return
	}
	for _, f := range []string{"bot.json", "datos", ".lang", "logs", "minecraft-bot.log", "cubit.log"} {
		if _, err := os.Stat(f); err == nil {
			if err := os.Rename(f, filepath.Join(dir, f)); err != nil {
				log.Printf("hub: migrar %s: %v", f, err)
			}
		}
	}
	if h.cfg.Token == "" {
		h.cfg.Token = old.PanelToken
	}
	h.cfg.Bots = append(h.cfg.Bots, id)
	h.cfg.Run = append(h.cfg.Run, id) // (estaba en marcha: sigue)
	log.Printf("hub: el bot %s pasa a %s/", old.Name, dir)
}

var idChars = regexp.MustCompile(`[^a-z0-9_]+`)

// newID: un id de carpeta libre para un bot llamado name.
func (h *hub) newID(name string) string {
	base := idChars.ReplaceAllString(strings.ToLower(name), "")
	if base == "" {
		base = "bot"
	}
	id := base
	for n := 2; ; n++ {
		if _, err := os.Stat(filepath.Join(botsDir, id)); errors.Is(err, os.ErrNotExist) && !slices.Contains(h.cfg.Bots, id) {
			return id
		}
		id = fmt.Sprintf("%s%d", base, n)
	}
}

func (h *hub) ids() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.cfg.Bots)
}

func (h *hub) bot(id string) *hubBot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.bots[id]
}

// info lee el bot.json de un bot (lo que importa para el panel).
func (h *hub) info(id string) config.Bot {
	var c config.Bot
	if b, err := os.ReadFile(filepath.Join(botsDir, id, "bot.json")); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

// ---- procesos ----

func (b *hubBot) running() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cmd != nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// start arranca el bot (si no lo está) y lo vigila.
func (h *hub) start(b *hubBot) error {
	b.mu.Lock()
	b.want = true
	if b.cmd != nil {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()
	h.remember(b.id, true)
	return h.spawn(b)
}

func (h *hub) spawn(b *hubBot) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	port, err := freePort()
	if err != nil {
		return err
	}
	args := []string{"--config", "bot.json", "--monitor", fmt.Sprintf("127.0.0.1:%d", port)}
	if h.premiumFor(h.info(b.id).Server) {
		args = append(args, "--microsoft") // (servidor premium: con cuenta de Microsoft)
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Join(botsDir, b.id)
	cmd.Env = append(os.Environ(), "MCBOT_PANEL_TOKEN="+h.cfg.Token)
	if va := h.viaFor(h.info(b.id).Server); va != "" {
		cmd.Env = append(cmd.Env, "MCBOT_VIAPROXY="+va) // (servidores de otra versión)
	}
	cmd.Stdin = nil
	ownGroup(cmd) // (Ctrl-C en la terminal es para el hub: él los para en orden)
	out, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	px := httputil.NewSingleHostReverseProxy(target)
	px.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		i18n.HTTPMsg(w, r, http.StatusBadGateway, "the bot is not answering (starting or stopped)", "el bot no responde (está arrancando o se ha parado)")
	}
	b.mu.Lock()
	b.cmd, b.port, b.started, b.proxy = cmd, port, time.Now(), px
	b.mu.Unlock()
	name := h.info(b.id).Name
	log.Printf("hub: arranco %s (bots/%s)", name, b.id)
	go func() { // su salida: a la del hub, con su nombre, y las últimas líneas
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			l := ansi.Strip(sc.Text())
			fmt.Fprintf(os.Stdout, "[%s] %s\n", name, l)
			b.mu.Lock()
			b.tail = append(b.tail, l)
			if len(b.tail) > 30 {
				b.tail = b.tail[len(b.tail)-30:]
			}
			b.mu.Unlock()
		}
	}()
	go h.watch(b, cmd)
	h.events.publish(Event{Bot: b.id, Name: name, Type: "bot", Status: "started"})
	return nil
}

// watch espera a que el proceso acabe; si tenía que seguir, lo vuelve a arrancar.
func (h *hub) watch(b *hubBot, cmd *exec.Cmd) {
	err := cmd.Wait()
	h.events.publish(Event{Bot: b.id, Name: h.info(b.id).Name, Type: "bot", Status: "stopped"})
	b.mu.Lock()
	if b.cmd == cmd {
		b.cmd, b.proxy = nil, nil
	}
	want := b.want && h.ctx.Err() == nil
	quick := time.Since(b.started) < 30*time.Second
	if quick {
		b.fails++
	} else {
		b.fails = 0
	}
	fails := b.fails
	b.mu.Unlock()
	if h.ctx.Err() != nil {
		return // (el hub se está parando: los que estaban en marcha siguen apuntados)
	}
	b.mu.Lock()
	again := b.again
	b.again = false
	b.mu.Unlock()
	if again { // (se paró para cambiar de servidor: otra vez ya)
		_ = h.spawn(b)
		return
	}
	if err == nil { // salió él (/quit): no se vuelve a arrancar
		b.mu.Lock()
		b.want = false
		b.mu.Unlock()
		h.remember(b.id, false)
		log.Printf("hub: %s se ha parado", b.id)
		return
	}
	if !want {
		return
	}
	wait := time.Duration(min(fails, 6)) * 5 * time.Second
	log.Printf("hub: %s se ha caído (%v): lo vuelvo a arrancar en %s", b.id, err, wait)
	select {
	case <-h.ctx.Done():
		return
	case <-time.After(wait):
	}
	b.mu.Lock()
	retry := b.want && b.cmd == nil
	b.mu.Unlock()
	if retry {
		_ = h.spawn(b)
	}
}

// stop para el bot (y no se vuelve a arrancar).
func (h *hub) stop(b *hubBot) {
	b.mu.Lock()
	b.want = false
	b.mu.Unlock()
	h.remember(b.id, false)
	h.stopProc(b)
}

func (h *hub) stopProc(b *hubBot) {
	b.mu.Lock()
	cmd := b.cmd
	b.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt) // (sale limpio: guarda caminos, cierra la conexión)
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		if !b.running() {
			return
		}
	}
	_ = cmd.Process.Kill()
}

// remember: si el bot estaba en marcha (para arrancarlo con el hub).
func (h *hub) remember(id string, run bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cfg.Run = slices.DeleteFunc(h.cfg.Run, func(s string) bool { return s == id })
	if run {
		h.cfg.Run = append(h.cfg.Run, id)
	}
	h.save()
}

// serverUp: el servidor de pruebas acaba de encenderse: los bots que van a él
// vuelven a entrar ya.
func (h *hub) serverUp() {
	for _, id := range h.ids() {
		b := h.bot(id)
		if b == nil || !b.running() || !mcserver.OnTestServer(h.info(id).Server) {
			continue
		}
		b.mu.Lock()
		port := b.port
		b.mu.Unlock()
		req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/reconnect?t=%s", port, h.cfg.Token), nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}
}

// ---- lo que ve el panel ----

// BotState: un bot tal como lo ve el panel.
type BotState struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Server      string   `json:"server"`          // la dirección
	ServerID    string   `json:"server_id"`       // su servidor en la lista ("test" = el de pruebas)
	Login       *msLogin `json:"login,omitempty"` // servidor premium: el código para vincular la cuenta
	OnTest      bool     `json:"on_test"`         // va al servidor de pruebas
	Owners      []string `json:"owners"`
	Running     bool     `json:"running"`   // el proceso está en marcha
	Connected   bool     `json:"connected"` // dentro del servidor
	Health      float32  `json:"health"`
	Task        string   `json:"task"`
	Queue       []string `json:"queue"`           // las órdenes esperando a que acabe la tarea (/queue)
	QueuePaused bool     `json:"queue_paused"`    // la cola está en pausa (fallo o /stop)
	Error       string   `json:"error,omitempty"` // por qué no arranca (sus últimas líneas)
	Avatar      string   `json:"avatar"`
}

// State: /api/state, lo que ve el panel (del hub o de un bot solo).
type State struct {
	Mode       string       `json:"mode"` // "hub" o "single" (un bot solo, --monitor)
	Bots       []BotState   `json:"bots"`
	Servers    []ServerView `json:"servers"`
	Last       string       `json:"last,omitempty"`
	TestServer struct {
		Available bool   `json:"available"`
		Addr      string `json:"addr,omitempty"`
		Power     string `json:"power,omitempty"` // on, off, stopping, starting
	} `json:"test_server"`
	Update   *UpdateView `json:"update,omitempty"` // versión nueva (update.go)
	Setup    bool        `json:"setup"`            // nada en marcha (o ningún bot): el asistente
	Defaults struct {    // para el asistente
		Owner string `json:"owner"`
	} `json:"defaults"`
}

func (h *hub) state() State {
	st := State{Mode: "hub", Bots: []BotState{}}
	h.mu.Lock()
	st.Last = h.cfg.Last
	h.mu.Unlock()
	anyRunning := false
	for _, id := range h.ids() {
		c := h.info(id)
		b := h.bot(id)
		bs := BotState{Queue: []string{}, ID: id, Name: c.Name, Server: c.Server, ServerID: h.serverOf(c.Server), OnTest: mcserver.OnTestServer(c.Server), Owners: c.Owners, Avatar: avatar.URL(c.Name, h.premiumFor(c.Server))}
		if b != nil {
			b.mu.Lock()
			bs.Running = b.cmd != nil
			port := b.port
			if b.fails > 0 && len(b.tail) > 0 {
				bs.Error = strings.Join(b.tail[max(0, len(b.tail)-3):], "\n")
			}
			bs.Login = loginFrom(b.tail)
			b.mu.Unlock()
			if bs.Running {
				anyRunning = true
				h.peek(port, &bs)
			}
		}
		if bs.Owners == nil {
			bs.Owners = []string{}
		}
		st.Bots = append(st.Bots, bs)
	}
	if t := mcserver.TestServerAddr(); t != "" {
		st.TestServer.Available, st.TestServer.Addr = true, t
		st.TestServer.Power = "on"
		if mcserver.Off() {
			st.TestServer.Power = mcserver.PowerState(false)
		} else if _, err := rcon.Cmd("list"); err != nil {
			st.TestServer.Power = mcserver.PowerState(false)
		}
	}
	st.Servers = h.servers()
	st.Setup = len(st.Bots) == 0 // (sin bots: el asistente; con bots, la lista aunque estén apagados)
	_ = anyRunning
	st.Defaults.Owner = h.opts.DefaultOwner
	busy := 0
	for _, b := range st.Bots {
		if b.Running && b.Task != "" {
			busy++
		}
	}
	st.Update = h.updateView(busy)
	return st
}

// peek: el estado de un bot en marcha (su /json), rápido.
func (h *hub) peek(port int, bs *BotState) {
	c := http.Client{Timeout: 600 * time.Millisecond}
	resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/json?t=%s", port, h.cfg.Token))
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var p struct {
		Connected bool     `json:"connected"`
		Health    float32  `json:"health"`
		Task      string   `json:"task"`
		Queue     []string `json:"queue"`
		Paused    bool     `json:"queue_paused"`
	}
	if json.NewDecoder(resp.Body).Decode(&p) == nil {
		bs.Connected, bs.Health, bs.Task = p.Connected, p.Health, p.Task
		bs.Queue, bs.QueuePaused = p.Queue, p.Paused
	}
}

// newBotReq: lo que pide el asistente para crear un bot.
type newBotReq struct {
	Name     string   `json:"name"`
	Server   string   `json:"server"`    // una dirección (se guarda como servidor si no está)
	ServerID string   `json:"server_id"` // o uno de la lista ("test" = el de pruebas); manda sobre server
	Owners   []string `json:"owners"`
	Lang     string   `json:"lang"`
	Start    bool     `json:"start"`
	TestOn   bool     `json:"start_test_server"` // encender el de pruebas si está apagado
}

func (h *hub) create(r newBotReq) (string, error) {
	r.Name = strings.TrimSpace(r.Name)
	if !mc.ValidName.MatchString(r.Name) {
		return "", i18n.Errorf("invalid name %q: 3-16 letters, digits or _", "nombre %q inválido: 3-16 letras, números o _", r.Name)
	}
	for _, id := range h.ids() {
		if strings.EqualFold(h.info(id).Name, r.Name) {
			return "", i18n.Errorf("there is already a bot called %s", "ya hay un bot que se llama %s", r.Name)
		}
	}
	if r.ServerID != "" {
		s, ok := h.server(r.ServerID)
		if !ok {
			return "", i18n.Errorf("there is no server %s", "no hay ningún servidor %s", r.ServerID)
		}
		r.Server = s.Addr
	}
	if r.Server = strings.TrimSpace(r.Server); r.Server == "" {
		if r.Server = mcserver.TestServerAddr(); r.Server == "" {
			return "", i18n.Errorf("missing server (there is no test server)", "falta el servidor (no hay servidor de pruebas)")
		}
	}
	if h.serverOf(r.Server) == "" {
		if _, err := h.addServer(hubServer{Name: r.Server, Addr: r.Server}); err != nil {
			return "", err
		}
	}
	var owners []string
	for _, o := range r.Owners {
		if o = strings.TrimSpace(o); o != "" && !slices.Contains(owners, o) {
			if o != "*" && !mc.ValidName.MatchString(o) {
				return "", i18n.Errorf("invalid owner %q", "dueño %q inválido", o)
			}
			owners = append(owners, o)
		}
	}
	c := config.Bot{Schema: config.BotSchema, Server: r.Server, Name: r.Name, Owners: owners, Seeded: true}
	h.mu.Lock()
	id := h.newID(r.Name)
	h.mu.Unlock()
	dir := filepath.Join(botsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "bot.json"), append(b, '\n'), 0o600); err != nil {
		return "", err
	}
	if r.Lang == "es" || r.Lang == "en" {
		_ = os.WriteFile(filepath.Join(dir, i18n.LangFile), []byte(r.Lang+"\n"), 0o644)
	}
	h.mu.Lock()
	h.cfg.Bots = append(h.cfg.Bots, id)
	h.bots[id] = &hubBot{id: id}
	h.cfg.Last = id
	h.save()
	h.mu.Unlock()
	log.Printf("hub: bot nuevo %s (bots/%s) → %s", r.Name, id, r.Server)
	if owners != nil {
		rcon.OpOwners(owners)
	}
	if r.TestOn && mcserver.OnTestServer(r.Server) && mcserver.Off() {
		_ = mcserver.Start()
	}
	if r.Start {
		if err := h.start(h.bot(id)); err != nil {
			return id, err
		}
	}
	return id, nil
}

// remove elimina el bot: lo para y borra su carpeta (configuración, memoria,
// registros). No se puede deshacer.
func (h *hub) remove(id string) error {
	b := h.bot(id)
	if b == nil {
		return i18n.Errorf("there is no bot %s", "no hay ningún bot %s", id)
	}
	name := h.info(id).Name
	h.stop(b)
	dir := filepath.Join(botsDir, id)
	if filepath.Dir(dir) != botsDir || id == "" || strings.ContainsAny(id, `/\.`) {
		return i18n.Errorf("invalid bot id", "id de bot inválido") // (por si acaso: nunca fuera de bots/)
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	h.mu.Lock()
	h.cfg.Bots = slices.DeleteFunc(h.cfg.Bots, func(s string) bool { return s == id })
	h.cfg.Run = slices.DeleteFunc(h.cfg.Run, func(s string) bool { return s == id })
	delete(h.bots, id)
	if h.cfg.Last == id {
		h.cfg.Last = ""
	}
	h.save()
	h.mu.Unlock()
	log.Printf("hub: bot %s eliminado (bots/%s borrada)", name, id)
	return nil
}

// ---- rutas ----

func (h *hub) routes() http.Handler {
	ok := web.KeyCheck(h.cfg.Token, "missing key: open the URL shown in the hub log (…/?t=…)", "falta la clave: abre la URL que sale en el log del hub (…/?t=…)")
	jsonOut := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux := http.NewServeMux()
	brand.Routes(mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if !ok(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(web.PanelHTML)
	})
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		if ok(w, r) {
			jsonOut(w, h.state())
		}
	})
	mux.HandleFunc("/api/bots", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet { // la lista (API pública)
			if ok(w, r) {
				jsonOut(w, h.state().Bots)
			}
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "GET, POST", http.StatusMethodNotAllowed)
			return
		}
		if !ok(w, r) {
			return
		}
		var req newBotReq
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
			i18n.HTTPMsg(w, r, http.StatusBadRequest, "unreadable request", "petición ilegible")
			return
		}
		id, err := h.create(req)
		if err != nil && id == "" {
			i18n.HTTPError(w, r, http.StatusBadRequest, err)
			return
		}
		res := map[string]string{"id": id}
		if err != nil {
			res["error"] = i18n.ErrText(r, err)
		}
		jsonOut(w, res)
	})
	mux.HandleFunc("/api/bots/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet { // estado de uno y su log (API pública)
			if !ok(w, r) {
				return
			}
			id, action, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/bots/"), "/")
			if !h.apiGet(w, r, id, action, jsonOut) {
				http.NotFound(w, r)
			}
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "GET, POST", http.StatusMethodNotAllowed)
			return
		}
		if !ok(w, r) {
			return
		}
		id, action, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/bots/"), "/")
		b := h.bot(id)
		if b == nil {
			i18n.HTTPMsg(w, r, http.StatusNotFound, "there is no bot "+id, "no hay ningún bot "+id)
			return
		}
		var err error
		switch action {
		case "start":
			err = h.start(b)
		case "stop":
			h.stop(b)
		case "open": // el que se está viendo en el panel (para la próxima vez)
			h.mu.Lock()
			h.cfg.Last = id
			h.save()
			h.mu.Unlock()
		case "delete":
			err = h.remove(id)
		case "command":
			h.apiCommand(w, r, id)
			return
		case "move":
			var req struct {
				ServerID string `json:"server_id"`
			}
			if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); e != nil {
				i18n.HTTPMsg(w, r, http.StatusBadRequest, "unreadable request", "petición ilegible")
				return
			}
			err = h.moveBot(id, req.ServerID)
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			i18n.HTTPError(w, r, http.StatusBadRequest, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// el panel de cada bot
	mux.HandleFunc("/b/", func(w http.ResponseWriter, r *http.Request) {
		if !ok(w, r) {
			return
		}
		id, rest, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/b/"), "/")
		b := h.bot(id)
		if b == nil {
			i18n.HTTPMsg(w, r, http.StatusNotFound, "there is no bot "+id, "no hay ningún bot "+id)
			return
		}
		b.mu.Lock()
		px := b.proxy
		b.mu.Unlock()
		if px == nil {
			i18n.HTTPMsg(w, r, http.StatusServiceUnavailable, "the bot is off", "el bot está apagado")
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + rest
		q := r2.URL.Query()
		q.Set("t", h.cfg.Token)
		r2.URL.RawQuery = q.Encode()
		px.ServeHTTP(w, r2)
	})
	mux.HandleFunc("/avatar/", func(w http.ResponseWriter, r *http.Request) {
		if ok(w, r) {
			avatar.Serve(w, r, strings.TrimPrefix(r.URL.Path, "/avatar/"))
		}
	})
	mcserver.Routes(mux, ok)
	h.serverRoutesAPI(mux, ok)
	h.discordRoutes(mux, ok)
	h.apiRoutes(mux, ok)
	h.updateRoutes(mux, ok, jsonOut)
	return mux
}
