package hub

// Las actualizaciones de Cubit en el hub: mirar si hay versión nueva
// (internal/update), enseñarlo en el panel y, si el método de instalación lo
// permite, ponerla con un clic: copia de seguridad de la configuración,
// descarga comprobada (SHA256 + firma), sustitución del binario y reinicio del
// hub, que vuelve a arrancar los bots que estaban en marcha (hub.json "run") y
// estos retoman su trabajo (resume.json, ver main/resume.go).

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"cubit/internal/config"
	"cubit/internal/i18n"
	"cubit/internal/update"
)

type updater struct {
	chk  *update.Checker
	stop context.CancelFunc

	mu       sync.Mutex
	phase    string // "", verifying, downloading, installing, restarting, error
	err      string
	restart  bool // al parar el hub: volver a ejecutar el binario (ya sustituido)
	writable bool
	wrOnce   sync.Once
}

func (u *updater) setPhase(p string) {
	u.mu.Lock()
	u.phase = p
	if p != "error" {
		u.err = ""
	}
	u.mu.Unlock()
}

// exit: el hub ya ha parado todo; si hay que reiniciar, se reinicia.
func (u *updater) exit(version string) {
	u.mu.Lock()
	restart := u.restart
	u.mu.Unlock()
	if !restart {
		update.NoteCleanExit(".", version)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		log.Printf("update: no puedo reiniciar: %v", err)
		return
	}
	log.Print("update: reinicio el hub con el binario nuevo")
	if err := update.Restart(exe, os.Args, os.Environ()); err != nil {
		log.Printf("update: no se pudo reiniciar (arráncalo a mano): %v", err)
	}
}

func (u *updater) requestRestart() {
	u.mu.Lock()
	u.restart = true
	u.mu.Unlock()
	if u.stop != nil {
		u.stop()
	}
}

func (h *hub) initUpdates() {
	h.up = &updater{}
	h.up.chk = &update.Checker{Current: h.opts.Version, Enabled: h.updateEnabled}
}

func (h *hub) updateEnabled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.cfg.NoUpdateCheck
}

// rollbackAtStart: el binario recién instalado ya falló al arrancar antes:
// se vuelve al de antes y se ejecuta ese. Devuelve true si no hay que seguir.
func (h *hub) rollbackAtStart() bool {
	rb, exe := update.BeginStart(".", h.opts.Version)
	if !rb {
		return false
	}
	log.Printf("update: %s no arrancó bien: vuelvo a la versión anterior", h.opts.Version)
	if err := update.Restart(exe, os.Args, os.Environ()); err != nil {
		log.Printf("update: no pude arrancar la versión anterior (%s): %v", exe, err)
		return false
	}
	return true
}

// migrateConfigs lleva hub.json y los bot.json al formato de esta versión
// (internal/config/migrate.go). Uno de una versión más nueva no se toca.
func (h *hub) migrateConfigs() {
	if err := config.MigrateFile(hubFile, config.MigrateHub); err != nil {
		log.Fatalf("%v\n(esta versión es más vieja que la que guardó la configuración: usa la nueva, o restaura backups/<versión>/)", err)
	}
	ents, _ := os.ReadDir(botsDir)
	for _, e := range ents {
		if e.IsDir() {
			if err := config.MigrateFile(botsDir+"/"+e.Name()+"/bot.json", config.MigrateBot); err != nil {
				log.Printf("hub: %v (no lo toco)", err)
			}
		}
	}
}

// confirmUpdate: tras una actualización, el hub nuevo se da por bueno cuando
// responde a su propio panel; si en HealthWindow no lo hace, se vuelve atrás.
func (h *hub) confirmUpdate(addr string) {
	if !update.Pending(".", h.opts.Version) {
		return
	}
	_, port, _ := net.SplitHostPort(addr)
	url := fmt.Sprintf("http://127.0.0.1:%s/api/state?t=%s", port, h.cfg.Token)
	deadline := time.Now().Add(update.HealthWindow)
	c := http.Client{Timeout: 3 * time.Second}
	for time.Now().Before(deadline) {
		select {
		case <-h.ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
		if resp, err := c.Get(url); err == nil {
			ok := resp.StatusCode == http.StatusOK
			resp.Body.Close()
			if ok {
				update.MarkOK(".", h.opts.Version)
				log.Printf("update: %s funciona (actualización confirmada)", h.opts.Version)
				return
			}
		}
	}
	log.Print("update: el hub nuevo no responde: vuelvo a la versión anterior")
	if err := update.DoRollback(".", "the new version did not answer in time"); err != nil {
		log.Printf("update: no se pudo deshacer: %v", err)
		return
	}
	h.up.requestRestart()
}

func (h *hub) writable() bool {
	h.up.wrOnce.Do(func() {
		if exe, err := os.Executable(); err == nil {
			h.up.writable = update.Writable(dirOf(exe))
		}
	})
	return h.up.writable
}

func dirOf(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[:i]
	}
	return "."
}

// UpdateView: lo que ve el panel de las actualizaciones.
type UpdateView struct {
	update.Status
	Busy       int                `json:"busy"`            // bots con una tarea en marcha
	Phase      string             `json:"phase,omitempty"` // actualizando: verifying, downloading, installing, restarting, error
	Error      string             `json:"error,omitempty"`
	RolledBack *update.RolledBack `json:"rolled_back,omitempty"`
}

func (h *hub) updateView(busy int) *UpdateView {
	if !update.ValidVersion(h.opts.Version) && h.up.chk.Release() == nil {
		// compilación de desarrollo: solo el ajuste y la versión
		return &UpdateView{Status: update.Status{Enabled: h.updateEnabled(), Current: h.opts.Version, Method: update.EffectiveMethod(".")}}
	}
	v := &UpdateView{Status: h.up.chk.Status(update.EffectiveMethod("."), h.writable()), Busy: busy}
	h.up.mu.Lock()
	v.Phase, v.Error = h.up.phase, h.up.err
	h.up.mu.Unlock()
	v.RolledBack = update.LastRollback(".")
	return v
}

// saveResume: a cada bot en marcha se le pide que apunte lo que está haciendo
// (resume.json) para retomarlo al volver a arrancar.
func (h *hub) saveResume() {
	var wg sync.WaitGroup
	for _, id := range h.ids() {
		b := h.bot(id)
		if b == nil {
			continue
		}
		b.mu.Lock()
		port, on := b.port, b.cmd != nil
		b.mu.Unlock()
		if !on {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := http.Client{Timeout: 3 * time.Second}
			req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/saveresume?t=%s", port, h.cfg.Token), nil)
			if resp, err := c.Do(req); err == nil {
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()
}

// applyUpdate: la actualización con un clic (en segundo plano; el panel mira la fase).
func (h *hub) applyUpdate(rel *update.Release, pub ed25519.PublicKey) {
	exe, err := os.Executable()
	if err == nil {
		a := &update.Applier{Exe: exe, DataDir: ".", Current: h.opts.Version, PubKey: pub, Phase: h.up.setPhase}
		err = a.Apply(h.ctx, rel)
	}
	if err != nil {
		log.Printf("update: %v", err)
		h.up.mu.Lock()
		h.up.phase, h.up.err = "error", err.Error()
		h.up.mu.Unlock()
		return
	}
	log.Printf("update: %s instalada; reinicio el hub y los bots", rel.Tag)
	h.up.setPhase("restarting")
	h.saveResume()
	h.up.requestRestart() // (parar el hub guarda "run": los bots vuelven con él)
}

func (h *hub) updateRoutes(mux *http.ServeMux, ok func(http.ResponseWriter, *http.Request) bool, jsonOut func(http.ResponseWriter, any)) {
	busy := func() int {
		n := 0
		for _, id := range h.ids() {
			if b := h.bot(id); b != nil {
				var bs BotState
				b.mu.Lock()
				port, on := b.port, b.cmd != nil
				b.mu.Unlock()
				if on {
					h.peek(port, &bs)
					if bs.Task != "" {
						n++
					}
				}
			}
		}
		return n
	}
	post := func(path string, f func(w http.ResponseWriter, r *http.Request)) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST", http.StatusMethodNotAllowed)
				return
			}
			if ok(w, r) {
				f(w, r)
			}
		})
	}
	mux.HandleFunc("/api/update", func(w http.ResponseWriter, r *http.Request) {
		if ok(w, r) {
			jsonOut(w, h.updateView(busy()))
		}
	})
	post("/api/update/check", func(w http.ResponseWriter, r *http.Request) {
		if !update.ValidVersion(h.opts.Version) {
			i18n.HTTPMsg(w, r, http.StatusBadRequest, "this is a development build: no version to compare", "es una compilación de desarrollo: no hay versión con la que comparar")
			return
		}
		if err := h.up.chk.Check(r.Context(), true); err != nil && err != update.ErrNoRelease {
			i18n.HTTPMsg(w, r, http.StatusBadGateway, "could not check for updates: "+err.Error(), "no se pudo comprobar si hay actualizaciones: "+err.Error())
			return
		}
		jsonOut(w, h.updateView(busy()))
	})
	post("/api/update/setting", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Enabled bool `json:"enabled"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req) != nil {
			i18n.HTTPMsg(w, r, http.StatusBadRequest, "unreadable request", "petición ilegible")
			return
		}
		h.mu.Lock()
		h.cfg.NoUpdateCheck = !req.Enabled
		h.save()
		h.mu.Unlock()
		jsonOut(w, h.updateView(busy()))
	})
	post("/api/update/dismiss", func(w http.ResponseWriter, r *http.Request) {
		update.DismissRollback(".")
		h.up.mu.Lock()
		if h.up.phase == "error" {
			h.up.phase, h.up.err = "", ""
		}
		h.up.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	post("/api/update/apply", func(w http.ResponseWriter, r *http.Request) {
		rel := h.up.chk.Release()
		if rel == nil || !update.Newer(rel.Tag, h.opts.Version) {
			i18n.HTTPMsg(w, r, http.StatusConflict, "there is no newer version", "no hay ninguna versión más nueva")
			return
		}
		if p := update.PlanFor(update.EffectiveMethod("."), h.writable(), rel); p.Kind != "self" {
			i18n.HTTPMsg(w, r, http.StatusConflict, "this installation is updated another way (see the command in the panel)", "esta instalación se actualiza de otra forma (mira el comando en el panel)")
			return
		}
		pub, err := update.PublicKey()
		if err != nil {
			i18n.HTTPMsg(w, r, http.StatusConflict, err.Error(), "esta versión aún no lleva clave de firma: actualiza a mano desde la página de Releases")
			return
		}
		h.up.mu.Lock()
		if h.up.phase != "" && h.up.phase != "error" {
			h.up.mu.Unlock()
			i18n.HTTPMsg(w, r, http.StatusConflict, "an update is already in progress", "ya hay una actualización en curso")
			return
		}
		h.up.phase, h.up.err = "verifying", ""
		h.up.mu.Unlock()
		go h.applyUpdate(rel, pub)
		w.WriteHeader(http.StatusAccepted)
	})
}
