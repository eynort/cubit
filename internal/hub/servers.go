package hub

// Los servidores del hub, aparte de los bots: el bot es quién juega, el
// servidor es dónde. El servidor de pruebas (el local de Docker) está siempre
// (id "test"); los externos los da de alta el jugador una vez (nombre,
// dirección, si es premium) y los usa cualquier bot. Mover un bot a otro
// servidor = cambiar la dirección de su bot.json y reiniciarlo.
//
//	POST /api/servers                {name, addr, premium} → {id}
//	POST /api/servers/<id>/update    {name, addr, premium}
//	POST /api/servers/<id>/delete    (409 si tiene bots: hay que moverlos antes)
//	POST /api/bots/<id>/move         {server_id}

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/mj41/go-mc26/data/version"

	"cubit/internal/i18n"
	"cubit/internal/mc"
	"cubit/internal/mcserver"
	"cubit/internal/rcon"
)

// TestServerID: el id del servidor de pruebas en la lista.
const TestServerID = "test"

// hubServer: un servidor externo guardado.
type hubServer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Addr    string `json:"addr"`
	Premium bool   `json:"premium,omitempty"` // con cuenta de Microsoft (online-mode)
}

// ServerView: un servidor tal como lo ve el panel.
type ServerView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Addr    string `json:"addr"`
	Kind    string `json:"kind"` // "test" (el local) o "external"
	Premium bool   `json:"premium"`
	Power   string `json:"power,omitempty"` // solo el de pruebas: on, off, stopping, starting
	// cómo entra un jugador: el de pruebas, por el puerto publicado en el equipo
	// del hub (el panel pone delante el nombre con el que se abrió); los
	// externos, por su dirección
	JoinPort int      `json:"join_port,omitempty"`
	Version  string   `json:"version"` // la de Minecraft que hace falta (la del bot)
	Bots     []string `json:"bots"`    // los ids de los bots asignados
	// la versión que dice tener el servidor externo (Server List Ping) y si el
	// bot entra directo (""), por ViaProxy ("ok") o no puede ("unavailable")
	Detected string `json:"detected,omitempty"`
	Via      string `json:"via,omitempty"`
}

// sameAddr: dos direcciones son el mismo servidor (host sin mayúsculas, puerto 25565 por defecto).
func sameAddr(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ha, pa := mc.SplitAddr(strings.TrimSpace(a))
	hb, pb := mc.SplitAddr(strings.TrimSpace(b))
	return strings.EqualFold(ha, hb) && pa == pb
}

// serverOf: el id del servidor de la dirección addr ("" si no está dado de alta).
func (h *hub) serverOf(addr string) string {
	if mcserver.OnTestServer(addr) {
		return TestServerID
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.cfg.Servers {
		if sameAddr(s.Addr, addr) {
			return s.ID
		}
	}
	return ""
}

// server: el servidor id (el de pruebas incluido). ok false si no existe.
func (h *hub) server(id string) (hubServer, bool) {
	if id == TestServerID {
		t := mcserver.TestServerAddr()
		return hubServer{ID: TestServerID, Name: "Test server", Addr: t}, t != ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, s := range h.cfg.Servers {
		if s.ID == id {
			return s, true
		}
	}
	return hubServer{}, false
}

// adoptServers: las direcciones de los bots que no son de ningún servidor
// guardado se dan de alta solas (los bots de antes, o un bot.json a mano).
func (h *hub) adoptServers() {
	for _, id := range h.ids() {
		addr := h.info(id).Server
		if addr == "" || h.serverOf(addr) != "" {
			continue
		}
		s, err := h.addServer(hubServer{Name: addr, Addr: addr})
		if err == nil {
			log.Printf("hub: el servidor %s (de %s) queda guardado", s.Addr, id)
		}
	}
}

func (h *hub) addServer(s hubServer) (hubServer, error) {
	s.Name, s.Addr = strings.TrimSpace(s.Name), strings.TrimSpace(s.Addr)
	if s.Addr == "" {
		return s, i18n.Errorf("missing the server address", "falta la dirección del servidor")
	}
	if strings.ContainsAny(s.Addr, " /") {
		return s, i18n.Errorf("invalid address %q: host or host:port", "dirección %q inválida: host o host:puerto", s.Addr)
	}
	if s.Name == "" {
		s.Name = s.Addr
	}
	if mcserver.OnTestServer(s.Addr) {
		return s, i18n.Errorf("that is the test server: it is already in the list", "ese es el servidor de pruebas: ya está en la lista")
	}
	if h.serverOf(s.Addr) != "" {
		return s, i18n.Errorf("there is already a server with the address %s", "ya hay un servidor con la dirección %s", s.Addr)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	base := idChars.ReplaceAllString(strings.ToLower(s.Name), "")
	if base == "" || base == TestServerID {
		base = "srv"
	}
	s.ID = base
	for n := 2; slices.ContainsFunc(h.cfg.Servers, func(x hubServer) bool { return x.ID == s.ID }); n++ {
		s.ID = fmt.Sprintf("%s%d", base, n)
	}
	h.cfg.Servers = append(h.cfg.Servers, s)
	h.save()
	return s, nil
}

// botsOn: los bots que van al servidor id.
func (h *hub) botsOn(id string) []string {
	out := []string{}
	for _, b := range h.ids() {
		if h.serverOf(h.info(b).Server) == id {
			out = append(out, b)
		}
	}
	return out
}

func (h *hub) servers() []ServerView {
	var out []ServerView
	if t := mcserver.TestServerAddr(); t != "" {
		v := ServerView{ID: TestServerID, Name: "Test server", Addr: t, Kind: "test", Bots: h.botsOn(TestServerID), JoinPort: mcserver.TestJoinPort(), Version: version.Name}
		v.Power = "on"
		if mcserver.Off() {
			v.Power = mcserver.PowerState(false)
		} else if _, err := rcon.Cmd("list"); err != nil {
			v.Power = mcserver.PowerState(false)
		}
		out = append(out, v)
	}
	h.mu.Lock()
	ext := slices.Clone(h.cfg.Servers)
	h.mu.Unlock()
	for _, s := range ext {
		det, via := serverVersion(s.Addr)
		out = append(out, ServerView{ID: s.ID, Name: s.Name, Addr: s.Addr, Kind: "external", Premium: s.Premium, Bots: h.botsOn(s.ID), Version: version.Name, Detected: det, Via: via})
	}
	if out == nil {
		out = []ServerView{}
	}
	return out
}

// updateServer cambia un servidor externo; los bots que van a él siguen
// yendo (se les cambia la dirección y, si están en marcha, se reinician).
func (h *hub) updateServer(id string, n hubServer) error {
	old, ok := h.server(id)
	if !ok || id == TestServerID {
		return i18n.Errorf("there is no external server %s", "no hay ningún servidor externo %s", id)
	}
	n.Name, n.Addr = strings.TrimSpace(n.Name), strings.TrimSpace(n.Addr)
	if n.Addr == "" {
		n.Addr = old.Addr
	}
	if n.Name == "" {
		n.Name = n.Addr
	}
	if !sameAddr(n.Addr, old.Addr) {
		if mcserver.OnTestServer(n.Addr) {
			return i18n.Errorf("that is the test server", "ese es el servidor de pruebas")
		}
		if other := h.serverOf(n.Addr); other != "" && other != id {
			return i18n.Errorf("there is already a server with the address %s", "ya hay un servidor con la dirección %s", n.Addr)
		}
	}
	bots := h.botsOn(id)
	h.mu.Lock()
	for i := range h.cfg.Servers {
		if h.cfg.Servers[i].ID == id {
			h.cfg.Servers[i].Name, h.cfg.Servers[i].Addr, h.cfg.Servers[i].Premium = n.Name, n.Addr, n.Premium
		}
	}
	h.save()
	h.mu.Unlock()
	if !sameAddr(n.Addr, old.Addr) || n.Premium != old.Premium {
		for _, b := range bots {
			if err := h.setBotServer(b, n.Addr); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *hub) deleteServer(id string) error {
	if id == TestServerID {
		return i18n.Errorf("the test server cannot be removed", "el servidor de pruebas no se puede quitar")
	}
	if _, ok := h.server(id); !ok {
		return i18n.Errorf("there is no server %s", "no hay ningún servidor %s", id)
	}
	if bots := h.botsOn(id); len(bots) > 0 {
		var names []string
		for _, b := range bots {
			names = append(names, h.info(b).Name)
		}
		return i18n.Errorf("%s still play on it: move them to another server first", "aún juegan en él %s: muévelos antes a otro servidor", strings.Join(names, ", "))
	}
	h.mu.Lock()
	h.cfg.Servers = slices.DeleteFunc(h.cfg.Servers, func(s hubServer) bool { return s.ID == id })
	h.save()
	h.mu.Unlock()
	return nil
}

// setBotServer: el bot va a addr (su bot.json); si está en marcha, se reinicia.
func (h *hub) setBotServer(id, addr string) error {
	p := filepath.Join(botsDir, id, "bot.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	var raw map[string]any // (sin perder campos que el hub no conoce)
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	raw["server"] = addr
	nb, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(p+".tmp", append(nb, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(p+".tmp", p); err != nil {
		return err
	}
	if bot := h.bot(id); bot != nil && bot.running() {
		bot.mu.Lock()
		bot.again, bot.fails = true, 0 // (watch lo vuelve a arrancar enseguida, con la dirección nueva)
		bot.mu.Unlock()
		h.stopProc(bot)
	}
	return nil
}

// moveBot: el bot pasa a jugar en el servidor serverID.
func (h *hub) moveBot(id, serverID string) error {
	if h.bot(id) == nil {
		return i18n.Errorf("there is no bot %s", "no hay ningún bot %s", id)
	}
	s, ok := h.server(serverID)
	if !ok {
		return i18n.Errorf("there is no server %s", "no hay ningún servidor %s", serverID)
	}
	log.Printf("hub: %s pasa a %s (%s)", h.info(id).Name, s.Name, s.Addr)
	return h.setBotServer(id, s.Addr)
}

// premiumFor: el bot va a un servidor premium (entra con cuenta de Microsoft).
func (h *hub) premiumFor(addr string) bool {
	id := h.serverOf(addr)
	if id == "" || id == TestServerID {
		return false
	}
	s, _ := h.server(id)
	return s.Premium
}

func (h *hub) serverRoutesAPI(mux *http.ServeMux, ok func(http.ResponseWriter, *http.Request) bool) {
	read := func(w http.ResponseWriter, r *http.Request, v any) bool {
		if r.Method != http.MethodPost {
			http.Error(w, "POST", http.StatusMethodNotAllowed)
			return false
		}
		if !ok(w, r) {
			return false
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(v); err != nil {
			i18n.HTTPMsg(w, r, http.StatusBadRequest, "unreadable request", "petición ilegible")
			return false
		}
		return true
	}
	mux.HandleFunc("/api/servers", func(w http.ResponseWriter, r *http.Request) {
		var s hubServer
		if !read(w, r, &s) {
			return
		}
		ns, err := h.addServer(s)
		if err != nil {
			i18n.HTTPError(w, r, http.StatusBadRequest, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		det, via := probeNow(r.Context(), ns.Addr) // (otra versión: avisa si falta ViaProxy)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": ns.ID, "detected": det, "via": via})
	})
	mux.HandleFunc("/api/servers/", func(w http.ResponseWriter, r *http.Request) {
		id, action, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/api/servers/"), "/")
		var err error
		switch action {
		case "update":
			var s hubServer
			if !read(w, r, &s) {
				return
			}
			err = h.updateServer(id, s)
		case "delete":
			var none struct{}
			if r.ContentLength == 0 {
				if r.Method != http.MethodPost {
					http.Error(w, "POST", http.StatusMethodNotAllowed)
					return
				}
				if !ok(w, r) {
					return
				}
			} else if !read(w, r, &none) {
				return
			}
			err = h.deleteServer(id)
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			i18n.HTTPError(w, r, http.StatusConflict, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// msLogin: el código de la cuenta de Microsoft que pide un bot (servidor premium).
type msLogin struct {
	URL  string `json:"url"`
	Code string `json:"code"`
}

var msLoginLine = regexp.MustCompile(`CUENTA DE MICROSOFT: abre (\S+) y escribe el código\s+(\S+)`)

// loginFrom: el último código pedido en la salida del bot, si aún no entró.
func loginFrom(tail []string) *msLogin {
	for i := len(tail) - 1; i >= 0; i-- {
		if strings.Contains(tail[i], "login correcto") || strings.Contains(tail[i], "microsoft: la cuenta") {
			return nil
		}
		if m := msLoginLine.FindStringSubmatch(tail[i]); m != nil {
			return &msLogin{URL: m[1], Code: m[2]}
		}
	}
	return nil
}
