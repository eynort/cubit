package main

// Los ajustes desde el panel web: lo mismo que los flags, sin tocar nada a
// mano. Dueños, webhook y límites cambian al momento; el servidor, el nombre
// y la IA reinician el bot (vuelve a arrancar solo, leyendo bot.json).

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"cubit/internal/i18n"
	"cubit/internal/mc"
	"cubit/internal/rcon"
)

// keepConfigEnv: al reiniciarse desde el panel, manda bot.json (no los flags
// con los que se arrancó).
const keepConfigEnv = "MCBOT_KEEP_CONFIG"

type panelSettings struct {
	Server string   `json:"server"`
	Name   string   `json:"name"`
	Owners []string `json:"owners"`
	Lang   string   `json:"lang"` // el idioma del bot: su ayuda y las sugerencias (en, es)
	RCON   bool     `json:"rcon"` // (solo lectura) hay consola del servidor
	// La contraseña de inicio de sesión del servidor (AuthMe / nLogin): solo
	// entra (vacía = no cambiar); nunca sale: se ve si hay una (HasLoginPassword).
	LoginPassword      string `json:"loginPassword,omitempty"`
	ClearLoginPassword bool   `json:"clearLoginPassword,omitempty"`
	HasLoginPassword   bool   `json:"hasLoginPassword"`
}

func currentSettings() panelSettings {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	return panelSettings{Server: cfg.Server, Name: cfg.Name, Owners: append([]string{}, cfg.Owners...), RCON: rcon.Addr() != "", Lang: lang(), HasLoginPassword: cfg.LoginPassword != ""}
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
	if !mc.ValidName.MatchString(s.Name) {
		return false, i18n.Errorf("invalid name %q: 3-16 letters, digits or _", "nombre %q inválido: 3-16 letras, números o _", s.Name)
	}
	var owners []string
	for _, o := range s.Owners {
		if o = strings.TrimSpace(o); o != "" && !slices.Contains(owners, o) {
			if o != "*" && !mc.ValidName.MatchString(o) {
				return false, i18n.Errorf("invalid owner %q: 3-16 letters, digits or _ (or *)", "dueño %q inválido: 3-16 letras, números o _ (o *)", o)
			}
			owners = append(owners, o)
		}
	}
	if err := validLoginPassword(s.LoginPassword); err != nil {
		return false, err
	}
	if s.Lang != "" && languages[s.Lang] == "" {
		return false, i18n.Errorf("language %q: en or es", "idioma %q: en o es", s.Lang)
	}

	cfgMu.Lock()
	restart = !strings.EqualFold(s.Server, cfg.Server) || s.Name != cfg.Name
	newOwners := !slices.Equal(owners, cfg.Owners)
	cfg.Server, cfg.Name, cfg.Owners = s.Server, s.Name, owners
	switch {
	case s.ClearLoginPassword:
		cfg.LoginPassword = ""
	case s.LoginPassword != "":
		cfg.LoginPassword = s.LoginPassword
	}
	remoteOwners = owners
	err = saveConfigLocked()
	cfgMu.Unlock()
	if err != nil {
		return false, err
	}
	log.Printf("[panel] ajustes guardados · dueños: %s", strings.Join(owners, ", "))
	if s.Lang != "" && s.Lang != lang() {
		setLang(s.Lang)
	}
	if newOwners {
		rcon.OpOwners(owners)
	}
	return restart, nil
}

// validLoginPassword: sin espacios ni caracteres de control (iría partida en el comando).
func validLoginPassword(pw string) error {
	if pw == "" {
		return nil
	}
	if len(pw) > 100 || strings.ContainsFunc(pw, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return i18n.Errorf("the server password cannot have spaces and is at most 100 characters", "la contraseña del servidor no puede llevar espacios y tiene como mucho 100 caracteres")
	}
	return nil
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

// renameBot cambia solo el nombre del bot (y lo reinicia, si cambió).
func renameBot(name string) (restart bool, err error) {
	name = strings.TrimSpace(name)
	if !mc.ValidName.MatchString(name) {
		return false, i18n.Errorf("invalid name %q: 3-16 letters, digits or _", "nombre %q inválido: 3-16 letras, números o _", name)
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
			i18n.HTTPMsg(w, r, http.StatusBadRequest, "unreadable request", "petición ilegible")
			return
		}
		restart, err := renameBot(req.Name)
		if err != nil {
			i18n.HTTPError(w, r, http.StatusBadRequest, err)
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
				i18n.HTTPMsg(w, r, http.StatusBadRequest, "unreadable settings", "ajustes ilegibles")
				return
			}
			restart, err := applySettings(s)
			if err != nil {
				i18n.HTTPError(w, r, http.StatusBadRequest, err)
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
			i18n.HTTPMsg(w, r, http.StatusBadRequest, "missing command", "falta el comando")
			return
		}
		log.Printf("[servidor] > %s", strings.TrimSpace(req.Line))
		out, err := rcon.Cmd(req.Line)
		if err != nil {
			i18n.HTTPError(w, r, http.StatusBadGateway, err)
			return
		}
		if out != "" {
			log.Printf("[servidor] %s", out)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"out": out})
	})
}
