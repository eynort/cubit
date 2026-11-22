package hub

// La integración con Discord (apagada por defecto). El dueño pega en el panel
// el token de su bot de Discord, el canal del puente y los usuarios que pueden
// dar órdenes. Con eso el hub:
//
//   - se conecta al Gateway (con reintentos y espera creciente);
//   - atiende /bot <nombre> <orden>, /bots y /status <nombre> (solo los
//     usuarios permitidos);
//   - lleva al canal lo que los bots dicen en el chat del juego y lo que dicen
//     los jugadores, con el nombre del bot delante, en lotes (1 mensaje/s).
//
// El token vive solo en hub.json (0600, fuera de git); nunca se escribe en el
// log ni sale entero por la API (se enmascara).
//
//	GET  /api/discord   → ajustes (token enmascarado) y estado de la conexión
//	POST /api/discord   {enabled, token?, clear_token?, channel_id, allowed}

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"cubit/internal/i18n"
)

// discordConfig: lo que se guarda en hub.json.
type discordConfig struct {
	Enabled   bool     `json:"enabled"`
	Token     string   `json:"token,omitempty"`
	ChannelID string   `json:"channel_id,omitempty"`
	Allowed   []string `json:"allowed,omitempty"`
}

// DiscordView: lo que ve el panel (nunca el token entero).
type DiscordView struct {
	Enabled   bool     `json:"enabled"`
	TokenSet  bool     `json:"token_set"`
	TokenMask string   `json:"token_mask"`
	ChannelID string   `json:"channel_id"`
	Allowed   []string `json:"allowed"`
	State     string   `json:"state"` // off, connecting, connected, error
	Error     string   `json:"error,omitempty"`
}

// maskToken: "••••••••abcd" (solo las 4 últimas, y solo si el token es largo).
func maskToken(t string) string {
	if t == "" {
		return ""
	}
	if len(t) < 16 {
		return "••••••••"
	}
	return "••••••••" + t[len(t)-4:]
}

var snowflake = regexp.MustCompile(`^\d{15,25}$`)

// parseIDs: ids de Discord separados por comas, espacios o saltos de línea.
func parseIDs(list []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, raw := range list {
		for _, id := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t' || r == '\r'
		}) {
			if !snowflake.MatchString(id) {
				return nil, i18n.Errorf("«%s» no parece un ID de Discord (son 17-20 cifras)", "«%s» doesn't look like a Discord ID (it's 17-20 digits)", id)
			}
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out, nil
}

// ---- la sesión de Discord (una interfaz, para probar con un doble) ----

// dcInteraction: una orden con barra, ya sin tipos de Discord.
type dcInteraction struct {
	UserID  string
	Command string            // bot, bots, status
	Options map[string]string // name, command
	Reply   func(text string, ephemeral bool) error
}

// dcSession: lo que se usa de Discord.
type dcSession interface {
	// Open se conecta y avisa de los cambios de conexión y de cada orden.
	Open(onState func(connected bool), onCmd func(dcInteraction)) error
	// RegisterCommands da de alta las órdenes con barra (en el servidor del canal).
	RegisterCommands(channelID string) error
	Send(channelID, text string) error
	Close() error
}

// ---- lo que el gestor necesita del hub ----

type dcBot struct {
	ID, Name, Task     string
	Running, Connected bool
	Health             float32
}

type dcHub interface {
	dcBots() []dcBot
	runCommand(id, line string) error
}

// ---- la cola de salida: lotes y como mucho 1 mensaje por segundo ----

const (
	dcMsgMax   = 1900 // Discord admite 2000
	dcQueueMax = 300
)

type dcOutbox struct {
	mu      sync.Mutex
	lines   []string
	dropped int
}

func (o *dcOutbox) add(line string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.lines) >= dcQueueMax {
		o.lines = o.lines[1:]
		o.dropped++
	}
	o.lines = append(o.lines, line)
}

// cut corta s a n bytes sin partir una letra.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "…"
}

// next saca el siguiente mensaje (varias líneas juntas hasta dcMsgMax); "" si no hay nada.
func (o *dcOutbox) next() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	var sb strings.Builder
	if o.dropped > 0 {
		fmt.Fprintf(&sb, "…(%d lines skipped, too much chatter)\n", o.dropped)
		o.dropped = 0
	}
	n := 0
	for _, l := range o.lines {
		l = cut(l, dcMsgMax-10)
		if sb.Len()+len(l)+1 > dcMsgMax && n > 0 {
			break
		}
		sb.WriteString(l)
		sb.WriteByte('\n')
		n++
	}
	o.lines = o.lines[n:]
	return strings.TrimRight(sb.String(), "\n")
}

// ---- el gestor ----

type discordMgr struct {
	hub     dcHub
	save    func(discordConfig)
	factory func(token string) dcSession
	every   time.Duration // entre mensajes (1 s)

	mu     sync.Mutex
	cfg    discordConfig
	state  string
	errMsg string
	sess   dcSession
	stop   chan struct{}
	out    dcOutbox
}

func newDiscordMgr(h dcHub, cfg discordConfig, save func(discordConfig), factory func(string) dcSession) *discordMgr {
	return &discordMgr{hub: h, cfg: cfg, save: save, factory: factory, every: time.Second, state: "off"}
}

func (m *discordMgr) view() DiscordView {
	m.mu.Lock()
	defer m.mu.Unlock()
	return DiscordView{Enabled: m.cfg.Enabled, TokenSet: m.cfg.Token != "", TokenMask: maskToken(m.cfg.Token),
		ChannelID: m.cfg.ChannelID, Allowed: append([]string{}, m.cfg.Allowed...), State: m.state, Error: m.errMsg}
}

// update aplica los ajustes nuevos (token vacío = dejar el que hay) y reconecta.
func (m *discordMgr) update(enabled bool, token string, clearToken bool, channel string, allowed []string) error {
	ids, err := parseIDs(allowed)
	if err != nil {
		return err
	}
	channel = strings.TrimSpace(channel)
	if channel != "" && !snowflake.MatchString(channel) {
		return i18n.Errorf("el ID del canal no parece de Discord (son 17-20 cifras)", "the channel ID doesn't look like a Discord ID (17-20 digits)")
	}
	token = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(token), "Bot "))
	if token != "" && (strings.ContainsAny(token, " \n\t\"") || len(token) < 20) {
		return i18n.Errorf("ese token no parece de un bot de Discord", "that doesn't look like a Discord bot token")
	}
	m.mu.Lock()
	if clearToken {
		m.cfg.Token = ""
	}
	if token != "" {
		m.cfg.Token = token
	}
	m.cfg.Enabled = enabled && m.cfg.Token != ""
	m.cfg.ChannelID, m.cfg.Allowed = channel, ids
	cfg := m.cfg
	m.mu.Unlock()
	m.save(cfg)
	m.restart()
	return nil
}

// restart para la conexión que haya y, si está activado, arranca otra.
func (m *discordMgr) restart() {
	m.mu.Lock()
	if m.stop != nil {
		close(m.stop)
		m.stop = nil
	}
	if m.sess != nil {
		s := m.sess
		m.sess = nil
		go s.Close()
	}
	if !m.cfg.Enabled || m.cfg.Token == "" {
		m.state, m.errMsg = "off", ""
		m.mu.Unlock()
		return
	}
	m.state, m.errMsg = "connecting", ""
	stop := make(chan struct{})
	m.stop = stop
	cfg := m.cfg
	m.mu.Unlock()
	go m.run(cfg, stop)
}

func (m *discordMgr) setState(stop chan struct{}, st, msg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stop != stop {
		return // una conexión vieja
	}
	m.state, m.errMsg = st, msg
}

// redact quita el token de un texto (por si un error lo trae).
func redact(s, token string) string {
	if token != "" {
		s = strings.ReplaceAll(s, token, "***")
	}
	return s
}

// run: conecta con espera creciente (2 s … 5 min) y vacía la cola cada segundo.
func (m *discordMgr) run(cfg discordConfig, stop chan struct{}) {
	backoff := 2 * time.Second
	for {
		s := m.factory(cfg.Token)
		err := s.Open(func(up bool) {
			if up {
				m.setState(stop, "connected", "")
			} else {
				m.setState(stop, "connecting", "")
			}
		}, func(in dcInteraction) { m.handle(in) })
		if err == nil {
			m.mu.Lock()
			if m.stop != stop {
				m.mu.Unlock()
				_ = s.Close()
				return
			}
			m.sess = s
			m.state, m.errMsg = "connected", ""
			m.mu.Unlock()
			if rerr := s.RegisterCommands(cfg.ChannelID); rerr != nil { // se conecta igual: solo faltan las órdenes
				msg := redact(rerr.Error(), cfg.Token)
				log.Printf("discord: no pude dar de alta las órdenes: %s", msg)
				m.setState(stop, "connected", "slash commands: "+msg)
			}
			log.Print("discord: conectado")
			m.pump(s, cfg.ChannelID, stop)
			return
		}
		_ = s.Close()
		msg := redact(err.Error(), cfg.Token)
		log.Printf("discord: no conecto (%s); lo intento en %s", msg, backoff)
		m.setState(stop, "error", msg)
		select {
		case <-stop:
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > 5*time.Minute {
			backoff = 5 * time.Minute
		}
	}
}

// pump manda a Discord, como mucho un mensaje por tick.
func (m *discordMgr) pump(s dcSession, channel string, stop chan struct{}) {
	t := time.NewTicker(m.every)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			m.flush(s, channel)
		}
	}
}

// flush manda un mensaje con lo que haya en la cola.
func (m *discordMgr) flush(s dcSession, channel string) {
	if channel == "" {
		return
	}
	text := m.out.next()
	if text == "" {
		return
	}
	if err := s.Send(channel, text); err != nil {
		log.Printf("discord: no pude escribir en el canal: %s", redact(err.Error(), m.tokenCopy()))
	}
}

func (m *discordMgr) tokenCopy() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.Token
}

// bridge: una línea del chat de un bot, hacia el canal.
func (m *discordMgr) bridge(botName, line string) {
	m.mu.Lock()
	on := m.state == "connected" && m.cfg.ChannelID != ""
	m.mu.Unlock()
	if !on {
		return
	}
	m.out.add("**" + neutralize(botName) + "**: " + neutralize(line))
}

// neutralize: que el chat del juego no pueda mencionar a todo el mundo ni
// romper el formato.
func neutralize(s string) string {
	s = strings.ReplaceAll(s, "@", "@​")
	return strings.ReplaceAll(s, "`", "'")
}

func (m *discordMgr) allowed(user string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if user == "" {
		return false
	}
	for _, a := range m.cfg.Allowed {
		if a == user {
			return true
		}
	}
	return false
}

func (m *discordMgr) findBot(name string) (dcBot, bool) {
	name = strings.TrimSpace(name)
	for _, b := range m.hub.dcBots() {
		if strings.EqualFold(b.Name, name) || b.ID == name {
			return b, true
		}
	}
	return dcBot{}, false
}

func botLine(b dcBot) string {
	switch {
	case !b.Running:
		return fmt.Sprintf("**%s** is taking a nap (off)", neutralize(b.Name))
	case !b.Connected:
		return fmt.Sprintf("**%s** is on, still joining the server", neutralize(b.Name))
	case b.Task != "":
		return fmt.Sprintf("**%s** is busy: %s (health %.0f)", neutralize(b.Name), neutralize(b.Task), b.Health)
	}
	return fmt.Sprintf("**%s** is just hanging around (health %.0f)", neutralize(b.Name), b.Health)
}

// handle atiende una orden con barra.
func (m *discordMgr) handle(in dcInteraction) {
	if !m.allowed(in.UserID) {
		_ = in.Reply("Sorry mate, I only take orders from the people the owner picked. Ask them to add you!", true)
		return
	}
	switch in.Command {
	case "bots":
		bs := m.hub.dcBots()
		if len(bs) == 0 {
			_ = in.Reply("No bots yet, create one in the panel first!", false)
			return
		}
		sort.Slice(bs, func(i, j int) bool { return strings.ToLower(bs[i].Name) < strings.ToLower(bs[j].Name) })
		var sb strings.Builder
		for _, b := range bs {
			sb.WriteString(botLine(b) + "\n")
		}
		_ = in.Reply(strings.TrimRight(sb.String(), "\n"), false)
	case "status":
		b, ok := m.findBot(in.Options["name"])
		if !ok {
			_ = in.Reply("I don't know any bot called "+neutralize(in.Options["name"])+". Try /bots.", true)
			return
		}
		_ = in.Reply(botLine(b), false)
	case "bot":
		line := strings.TrimSpace(in.Options["command"])
		b, ok := m.findBot(in.Options["name"])
		if !ok {
			_ = in.Reply("I don't know any bot called "+neutralize(in.Options["name"])+". Try /bots.", true)
			return
		}
		if line == "" {
			_ = in.Reply("What should "+neutralize(b.Name)+" do? Give me a command, like `/goto 100 64 200`.", true)
			return
		}
		if !b.Running {
			_ = in.Reply(neutralize(b.Name)+" is off right now, start it from the panel first.", true)
			return
		}
		if err := m.hub.runCommand(b.ID, line); err != nil {
			_ = in.Reply("Couldn't pass that to "+neutralize(b.Name)+": "+neutralize(redact(err.Error(), m.tokenCopy())), true)
			return
		}
		_ = in.Reply("On it! Told **"+neutralize(b.Name)+"**: `"+neutralize(line)+"`", false)
	default:
		_ = in.Reply("I don't know that one.", true)
	}
}

// ---- pegamento con el hub ----

func (h *hub) dcBots() []dcBot {
	var out []dcBot
	for _, b := range h.state().Bots {
		out = append(out, dcBot{ID: b.ID, Name: b.Name, Task: b.Task, Running: b.Running, Connected: b.Connected, Health: b.Health})
	}
	return out
}

// runCommand manda una línea a la consola de un bot (su /cmd).
func (h *hub) runCommand(id, line string) error {
	b := h.bot(id)
	if b == nil {
		return i18n.Errorf("there is no bot %s", "no hay ningún bot %s", id)
	}
	b.mu.Lock()
	port := b.port
	on := b.cmd != nil
	b.mu.Unlock()
	if !on {
		return i18n.Errorf("the bot is off", "el bot está apagado")
	}
	body, _ := json.Marshal(map[string]string{"line": line})
	c := http.Client{Timeout: 5 * time.Second}
	resp, err := c.Post(fmt.Sprintf("http://127.0.0.1:%d/cmd?t=%s", port, url.QueryEscape(h.cfg.Token)), "application/json", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("%s", strings.TrimSpace(string(msg)))
	}
	return nil
}

var (
	chatLine   = regexp.MustCompile(`^(?:\[[^\]]+\] )?chat: (.+)$`) // lo que dice el bot
	playerLine = regexp.MustCompile(`^<([^>\s]+)> (.+)$`)           // lo que dice un jugador
)

// chatEvent: de una línea del log del bot, lo que se lleva al canal ("" = nada).
func chatEvent(line string) string {
	line = strings.TrimSpace(line)
	if m := chatLine.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	if m := playerLine.FindStringSubmatch(line); m != nil {
		return "<" + m[1] + "> " + m[2]
	}
	return ""
}

// pollChat lee el /log de los bots en marcha y se lleva lo del chat al puente.
func (h *hub) pollChat(m *discordMgr) {
	cursor := map[string]int{}
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	c := http.Client{Timeout: 1500 * time.Millisecond}
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-t.C:
		}
		if m.view().State != "connected" {
			cursor = map[string]int{} // al conectar, se empieza por lo nuevo
			continue
		}
		for _, id := range h.ids() {
			b := h.bot(id)
			if b == nil {
				continue
			}
			b.mu.Lock()
			port := b.port
			on := b.cmd != nil
			b.mu.Unlock()
			if !on {
				delete(cursor, id)
				continue
			}
			after, seen := cursor[id]
			resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/log?after=%d&t=%s", port, after, url.QueryEscape(h.cfg.Token)))
			if err != nil {
				continue
			}
			var r struct {
				Lines []string `json:"lines"`
				Next  int      `json:"next"`
			}
			err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r)
			resp.Body.Close()
			if err != nil {
				continue
			}
			cursor[id] = r.Next
			if !seen {
				continue // lo de antes de conectar no se cuenta
			}
			name := h.info(id).Name
			for _, l := range r.Lines {
				if ev := chatEvent(l); ev != "" {
					m.bridge(name, ev)
				}
			}
		}
	}
}

// startDiscord: al arrancar el hub.
func (h *hub) startDiscord() {
	var cfg discordConfig
	if h.cfg.Discord != nil {
		cfg = *h.cfg.Discord
	}
	h.discord = newDiscordMgr(h, cfg, func(c discordConfig) {
		h.mu.Lock()
		h.cfg.Discord = &c
		h.save()
		h.mu.Unlock()
	}, newGoSession)
	h.discord.restart()
	go h.pollChat(h.discord)
}

func (h *hub) discordRoutes(mux *http.ServeMux, ok func(http.ResponseWriter, *http.Request) bool) {
	mux.HandleFunc("/api/discord", func(w http.ResponseWriter, r *http.Request) {
		if !ok(w, r) {
			return
		}
		if h.discord == nil {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPost {
			var req struct {
				Enabled    bool     `json:"enabled"`
				Token      string   `json:"token"`
				ClearToken bool     `json:"clear_token"`
				ChannelID  string   `json:"channel_id"`
				Allowed    []string `json:"allowed"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
				i18n.HTTPMsg(w, r, http.StatusBadRequest, "unreadable request", "petición ilegible")
				return
			}
			if err := h.discord.update(req.Enabled, req.Token, req.ClearToken, req.ChannelID, req.Allowed); err != nil {
				i18n.HTTPError(w, r, http.StatusBadRequest, err)
				return
			}
		} else if r.Method != http.MethodGet {
			http.Error(w, "GET, POST", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(h.discord.view())
	})
}
