package main

// El panel web (--monitor :8080): el estado del bot, su inventario, la tarea,
// el log en directo y una caja para mandarle órdenes, como en la
// consola. Pensado para el móvil: si el bot corre en un teléfono viejo, se
// maneja desde el navegador de otro.
//
// Pide una clave (la de la URL que sale en el log al arrancar, guardada en
// bot.json): quien la tiene puede mandarle órdenes.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"cubit/brand"
	"cubit/internal/ansi"
	"cubit/internal/avatar"
	"cubit/internal/hub"
	"cubit/internal/i18n"
	"cubit/internal/mcserver"
	"cubit/internal/rcon"
	"cubit/internal/web"
)

// logRing guarda las últimas líneas del log para el panel.
type logRing struct {
	mu    sync.Mutex
	lines []string
	first int // el número de lines[0]
	part  string
}

const ringMax = 400

var panelLog = &logRing{}

func (r *logRing) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.part += ansi.Strip(string(p))
	for {
		i := strings.IndexByte(r.part, '\n')
		if i < 0 {
			break
		}
		r.lines = append(r.lines, r.part[:i])
		r.part = r.part[i+1:]
	}
	if n := len(r.lines) - ringMax; n > 0 {
		r.lines = append([]string(nil), r.lines[n:]...)
		r.first += n
	}
	return len(p), nil
}

// since: las líneas a partir del número n, y el número de la siguiente.
func (r *logRing) since(n int) ([]string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n < r.first {
		n = r.first
	}
	end := r.first + len(r.lines)
	if n > end {
		n = end
	}
	return append([]string(nil), r.lines[n-r.first:]...), end
}

// panelToken: la clave del panel (se crea una vez y se guarda).
func panelToken() string {
	if t := os.Getenv("MCBOT_PANEL_TOKEN"); t != "" {
		return t // (lo arranca el hub: su clave)
	}
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if cfg.PanelToken == "" {
		b := make([]byte, 12)
		_, _ = rand.Read(b)
		cfg.PanelToken = hex.EncodeToString(b)
		_ = saveConfigLocked()
	}
	return cfg.PanelToken
}

type panelState struct {
	stats
	Server      string         `json:"server"`
	Connected   bool           `json:"connected"`
	Inventory   map[string]int `json:"inventory"`
	Online      []string       `json:"online"`
	Queue       []string       `json:"queue"`        // las órdenes esperando (/queue)
	QueuePaused bool           `json:"queue_paused"` // la cola está en pausa
}

func panelSnapshot() panelState {
	st := panelState{Server: serverAddr}
	st.Queue, st.QueuePaused = workQueue.snapshot()
	b := current.Load()
	if b == nil {
		st.stats.Name = botName
		return st
	}
	st.stats = b.stats()
	st.Connected = b.ctx.Err() == nil && !b.connectedAt.IsZero()
	if b.Screens != nil {
		st.Inventory = b.itemTotals()
	}
	if n := onlineNames.Load(); n != nil {
		st.Online = append([]string(nil), (*n)...)
		sort.Strings(st.Online)
	}
	return st
}

func startMonitor(ctx context.Context) {
	if *monitorFlag == "" {
		return
	}
	log.SetOutput(io.MultiWriter(log.Writer(), panelLog))
	token := panelToken()
	ok := web.KeyCheck(token, "missing key: open the URL shown in the bot log (…/?t=…)", "falta la clave: abre la URL que sale en el log del bot (…/?t=…)")
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
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		if !ok(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(panelSnapshot())
	})
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		if ok(w, r) {
			eventsHandler(w, r)
		}
	})
	mux.HandleFunc("/log", func(w http.ResponseWriter, r *http.Request) {
		if !ok(w, r) {
			return
		}
		n, _ := strconv.Atoi(r.URL.Query().Get("after"))
		lines, next := panelLog.since(n)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"lines": lines, "next": next})
	})
	mux.HandleFunc("/cmd", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !ok(w, r) {
			if r.Method != http.MethodPost {
				http.Error(w, "POST", http.StatusMethodNotAllowed)
			}
			return
		}
		var req struct {
			Line string `json:"line"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil || strings.TrimSpace(req.Line) == "" {
			i18n.HTTPMsg(w, r, http.StatusBadRequest, "missing command", "falta la orden")
			return
		}
		line := strings.TrimSpace(req.Line)
		log.Printf("[panel] %s", line)
		go execLine(line)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/complete", func(w http.ResponseWriter, r *http.Request) {
		if !ok(w, r) {
			return
		}
		type sug struct {
			Value string `json:"value"`
			Label string `json:"label"`
			Desc  string `json:"desc"`
		}
		out := []sug{}
		for _, x := range suggestions(r.URL.Query().Get("line")) {
			out = append(out, sug{x.value, ansi.Strip(x.label), ansi.Strip(x.desc)})
			if len(out) == 40 {
				break
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
	settingsRoutes(mux, ok)
	mux.HandleFunc("/reconnect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && ok(w, r) {
			reconnectSoon() // (el hub: el servidor de pruebas acaba de encenderse)
			w.WriteHeader(http.StatusNoContent)
		}
	})
	mcserver.Routes(mux, ok)
	pp := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if ok(w, r) {
				h(w, r)
			}
		}
	}
	mux.HandleFunc("/debug/pprof/", pp(pprof.Index))
	mux.HandleFunc("/debug/pprof/profile", pp(pprof.Profile))
	mux.HandleFunc("/debug/pprof/symbol", pp(pprof.Symbol))
	mux.HandleFunc("/debug/pprof/trace", pp(pprof.Trace))
	ln, err := net.Listen("tcp", *monitorFlag)
	if err != nil {
		log.Printf("--monitor: %v", err)
		return
	}
	host := ln.Addr().String()
	if h, port, err := net.SplitHostPort(host); err == nil && (h == "::" || h == "0.0.0.0") {
		host = net.JoinHostPort(web.LocalIP(), port)
	}
	log.Printf("panel: http://%s/?t=%s", host, token)
	// el mismo idioma que el hub (un hub con un bot): /api/state, /b/self/…, /avatar/…
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		if ok(w, r) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(singleState())
		}
	})
	mux.HandleFunc("/avatar/", func(w http.ResponseWriter, r *http.Request) {
		if ok(w, r) {
			avatar.Serve(w, r, strings.TrimPrefix(r.URL.Path, "/avatar/"))
		}
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rest, found := strings.CutPrefix(r.URL.Path, "/b/self/"); found {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/" + rest
			mux.ServeHTTP(w, r2)
			return
		}
		mux.ServeHTTP(w, r)
	})
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	go func() { _ = srv.Serve(ln) }()
}

// singleState: /api/state de un bot solo (--monitor, sin hub): un hub con un
// bot, "self", siempre en marcha (es este programa).
func singleState() hub.State {
	st := hub.State{Mode: "single", Last: "self"}
	p := panelSnapshot()
	cfgMu.Lock()
	owners := append([]string{}, cfg.Owners...)
	cfgMu.Unlock()
	st.Bots = []hub.BotState{{ID: "self", Name: botName, Server: serverAddr, OnTest: botOnTestServer(), Owners: owners,
		Running: true, Connected: p.Connected, Health: p.Health, Task: p.Task, Queue: p.Queue, QueuePaused: p.QueuePaused, Avatar: avatar.URL(botName, *microsoftFlag)}}
	st.Servers = []hub.ServerView{}
	if t := mcserver.TestServerAddr(); t != "" {
		st.TestServer.Available, st.TestServer.Addr = true, t
		if mcserver.Off() {
			st.TestServer.Power = mcserver.PowerState(false)
		} else {
			_, err := rcon.Cmd("list")
			st.TestServer.Power = mcserver.PowerState(err == nil)
		}
		v := hub.ServerView{ID: hub.TestServerID, Name: "Test server", Addr: t, Kind: "test", Power: st.TestServer.Power, Bots: []string{}, JoinPort: mcserver.TestJoinPort(), Version: versionName()}
		if botOnTestServer() {
			v.Bots = []string{"self"}
		}
		st.Servers = append(st.Servers, v)
	}
	if botOnTestServer() {
		st.Bots[0].ServerID = hub.TestServerID
	} else {
		st.Bots[0].ServerID = "current"
		st.Servers = append(st.Servers, hub.ServerView{ID: "current", Name: serverAddr, Addr: serverAddr, Kind: "external", Premium: *microsoftFlag, Bots: []string{"self"}, Version: versionName()})
	}
	return st
}
