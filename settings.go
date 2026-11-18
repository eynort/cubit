package main

// Los ajustes desde el panel web: lo mismo que los flags, sin tocar nada a
// mano. Dueños, webhook y límites cambian al momento; el servidor, el nombre
// y la IA reinician el bot (vuelve a arrancar solo, leyendo bot.json).

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/automation"
)

// keepConfigEnv: al reiniciarse desde el panel, manda bot.json (no los flags
// con los que se arrancó).
const keepConfigEnv = "MCBOT_KEEP_CONFIG"

type panelSettings struct {
	Server     string   `json:"server"`
	Name       string   `json:"name"`
	Owners     []string `json:"owners"`
	Webhook    string   `json:"webhook"`
	AIURL      string   `json:"ai_url"`
	AIAPI      string   `json:"ai_api"`
	AIModel    string   `json:"ai_model"`
	Area       string   `json:"area"`        // "x1 z1 x2 z2" o ""
	PvP        bool     `json:"pvp"`         // puede pelear con jugadores
	NeverBreak string   `json:"never_break"` // separados por comas
	Chests     *bool    `json:"chests"`      // usa cofres para guardar (nil: sin cambios)
	Chest      *string  `json:"chest"`       // el cofre donde guarda todo: "x y z", "" = ninguno (nil: sin cambios)
	Lang       string   `json:"lang"`        // el idioma de las explicaciones (en, es)
	RCON       bool     `json:"rcon"`        // (solo lectura) hay consola del servidor
}

func currentSettings() panelSettings {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	s := panelSettings{
		Server: cfg.Server, Name: cfg.Name, Owners: append([]string{}, cfg.Owners...), Webhook: cfg.Webhook,
		PvP: !cfg.Limits.NoPvP, NeverBreak: strings.Join(cfg.Limits.neverBreak(), ", "), RCON: rconAddr() != "", Lang: lang(), Chests: new(bool),
	}
	*s.Chests = !cfg.NoChests
	s.Chest = new(string)
	if st, dim := botStore(); st != nil {
		if c, ok := st.MineChest(dim); ok {
			*s.Chest = fmt.Sprintf("%d %d %d", c.X, c.Y, c.Z)
		}
	}
	if a := cfg.AI; a != nil {
		s.AIURL, s.AIAPI, s.AIModel = a.URL, a.API, a.Model
	}
	if a := cfg.Limits.Area; a != nil {
		s.Area = fmt.Sprintf("%d %d %d %d", a[0], a[1], a[2], a[3])
	}
	return s
}

// applySettings guarda los ajustes; restart: hace falta reiniciar.
func applySettings(s panelSettings) (restart bool, err error) {
	s.Server, s.Name = strings.TrimSpace(s.Server), strings.TrimSpace(s.Name)
	cfgMu.Lock()
	if s.Server == "" { // (vacío = el de siempre: cambiar otra cosa no obliga a escribirlo)
		s.Server = cfg.Server
	}
	if s.Name == "" {
		s.Name = cfg.Name
	}
	cfgMu.Unlock()
	if !validName.MatchString(s.Name) {
		return false, bi("invalid name %q: 3-16 letters, digits or _", "nombre %q inválido: 3-16 letras, números o _", s.Name)
	}
	var owners []string
	for _, o := range s.Owners {
		if o = strings.TrimSpace(o); o != "" && !slices.Contains(owners, o) {
			if o != "*" && !validName.MatchString(o) {
				return false, bi("invalid owner %q: 3-16 letters, digits or _ (or *)", "dueño %q inválido: 3-16 letras, números o _ (o *)", o)
			}
			owners = append(owners, o)
		}
	}
	if s.Lang != "" && languages[s.Lang] == "" {
		return false, bi("language %q: en or es", "idioma %q: en o es", s.Lang)
	}
	area, err := parseArea(s.Area)
	if err != nil {
		return false, bi("area: \"x1 z1 x2 z2\" (four numbers)", "zona: \"x1 z1 x2 z2\" (cuatro números)")
	}
	nb := parseNeverBreak(s.NeverBreak)
	if slices.Equal(nb, defaultNeverBreak) {
		nb = nil
	}
	switch s.AIAPI = strings.TrimSpace(s.AIAPI); s.AIAPI {
	case "ollama", "openai":
	case "":
		s.AIAPI = "ollama"
	default:
		return false, bi(`AI API: "ollama" or "openai"`, `API de la IA: "ollama" u "openai"`)
	}
	ai := &aiSettings{URL: strings.TrimRight(strings.TrimSpace(s.AIURL), "/"), API: s.AIAPI, Model: strings.TrimSpace(s.AIModel)}
	if ai.URL != "" && ai.Model == "" {
		return false, bi("missing AI model", "falta el modelo de la IA")
	}

	cfgMu.Lock()
	restart = !strings.EqualFold(s.Server, cfg.Server) || s.Name != cfg.Name || cfg.AI == nil || *ai != *cfg.AI
	newOwners := !slices.Equal(owners, cfg.Owners)
	cfg.Server, cfg.Name, cfg.Owners, cfg.Webhook, cfg.AI = s.Server, s.Name, owners, strings.TrimSpace(s.Webhook), ai
	cfg.Limits.Area, cfg.Limits.NoPvP, cfg.Limits.NeverBreak = area, !s.PvP, nb
	remoteOwners = owners
	err = saveConfigLocked()
	cfgMu.Unlock()
	if err != nil {
		return false, err
	}
	log.Printf("[panel] ajustes guardados · dueños: %s · límites: %s", strings.Join(owners, ", "), describeLimits())
	if s.Chest != nil {
		if err := setStorageChest(*s.Chest); err != nil {
			return false, err
		}
	}
	if s.Chests != nil && *s.Chests != chestsOn() {
		_ = setChests(*s.Chests)
	}
	if s.Lang != "" && s.Lang != lang() {
		setLang(s.Lang)
	}
	if newOwners {
		opOwners(owners)
	}
	return restart, nil
}

// restartRequested: al salir, main vuelve a arrancar el programa.
var restartRequested atomic.Bool

func requestRestart() {
	log.Print("[panel] reinicio para aplicar los ajustes…")
	restartRequested.Store(true)
	appQuit()
}

// restartSelf: arranca el programa otra vez con los mismos argumentos (y
// bot.json mandando sobre ellos).
func restartSelf() {
	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("no se pudo reiniciar: %v", err)
	}
	_ = os.Setenv(keepConfigEnv, "1")
	if err := reexec(exe, os.Args, os.Environ()); err != nil {
		log.Fatalf("no se pudo reiniciar: %v", err)
	}
	os.Exit(0)
}

// botStore: el almacén del bot conectado y su dimensión (nil si no hay bot).
func botStore() (*automation.Storage, string) {
	b := current.Load()
	if b == nil || b.Tasks == nil {
		return nil, ""
	}
	dim := "minecraft:overworld"
	if b.Player != nil && b.Player.Spawn.Dimension != "" {
		dim = string(b.Player.Spawn.Dimension)
	}
	return b.Tasks.Store, dim
}

// setStorageChest: el cofre donde guarda todo ("x y z"; "" = ninguno), como /chest.
func setStorageChest(v string) error {
	st, dim := botStore()
	if st == nil {
		return bi("the bot is not running: start it to set its chest", "el bot no está en marcha: arráncalo para marcar su cofre")
	}
	v = strings.TrimSpace(strings.ReplaceAll(v, ",", " "))
	if v == "" {
		if _, ok := st.MineChest(dim); ok {
			st.SetMineChest(dim, nil)
			log.Print("[panel] cofre para guardar: ninguno")
		}
		return nil
	}
	f := strings.Fields(v)
	var n [3]int
	ok := len(f) == 3
	for i := 0; ok && i < 3; i++ {
		x, err := strconv.Atoi(f[i])
		n[i], ok = x, err == nil
	}
	if !ok {
		return bi("chest: three numbers, x y z", "cofre: tres números, x y z")
	}
	c := world.BlockPos{X: n[0], Y: n[1], Z: n[2]}
	if old, had := st.MineChest(dim); had && old == c {
		return nil
	}
	st.SetMineChest(dim, &c)
	st.SetBase(c)
	log.Printf("[panel] cofre para guardar: %d %d %d", c.X, c.Y, c.Z)
	return nil
}

// renameBot cambia solo el nombre del bot (y lo reinicia, si cambió).
func renameBot(name string) (restart bool, err error) {
	name = strings.TrimSpace(name)
	if !validName.MatchString(name) {
		return false, bi("invalid name %q: 3-16 letters, digits or _", "nombre %q inválido: 3-16 letras, números o _", name)
	}
	cfgMu.Lock()
	if cfg.Name == name {
		cfgMu.Unlock()
		return false, nil
	}
	old := cfg.Name
	cfg.Name = name
	err = saveConfigLocked()
	cfgMu.Unlock()
	if err != nil {
		return false, err
	}
	log.Printf("[panel] nombre del bot: %s → %s", old, name)
	return true, nil
}

func settingsRoutes(mux *http.ServeMux, ok func(http.ResponseWriter, *http.Request) bool) {
	mux.HandleFunc("/settings/name", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST", http.StatusMethodNotAllowed)
			return
		}
		if !ok(w, r) {
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
			httpMsg(w, r, http.StatusBadRequest, "unreadable request", "petición ilegible")
			return
		}
		restart, err := renameBot(req.Name)
		if err != nil {
			httpErr(w, r, http.StatusBadRequest, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"restart": restart})
		if restart {
			time.AfterFunc(300*time.Millisecond, requestRestart)
		}
	})
	mux.HandleFunc("/settings", func(w http.ResponseWriter, r *http.Request) {
		if !ok(w, r) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(currentSettings())
		case http.MethodPost:
			var s panelSettings
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&s); err != nil {
				httpMsg(w, r, http.StatusBadRequest, "unreadable settings", "ajustes ilegibles")
				return
			}
			restart, err := applySettings(s)
			if err != nil {
				httpErr(w, r, http.StatusBadRequest, err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]bool{"restart": restart})
			if restart {
				time.AfterFunc(300*time.Millisecond, requestRestart)
			}
		default:
			http.Error(w, "GET o POST", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/restart", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST", http.StatusMethodNotAllowed)
			return
		}
		if !ok(w, r) {
			return
		}
		w.WriteHeader(http.StatusNoContent)
		time.AfterFunc(300*time.Millisecond, requestRestart)
	})
	mux.HandleFunc("/rcon", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST", http.StatusMethodNotAllowed)
			return
		}
		if !ok(w, r) {
			return
		}
		var req struct {
			Line string `json:"line"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || strings.TrimSpace(req.Line) == "" {
			httpMsg(w, r, http.StatusBadRequest, "missing command", "falta el comando")
			return
		}
		log.Printf("[servidor] > %s", strings.TrimSpace(req.Line))
		out, err := rconCmd(req.Line)
		if err != nil {
			httpErr(w, r, http.StatusBadGateway, err)
			return
		}
		if out != "" {
			log.Printf("[servidor] %s", out)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"out": out})
	})
}
