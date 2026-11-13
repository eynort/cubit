package main

// El panel web (--monitor :8080): el estado del bot, su inventario, la tarea
// y la cola, el log en directo y una caja para mandarle órdenes, como en la
// consola. Pensado para el móvil: si el bot corre en un teléfono viejo, se
// maneja desde el navegador de otro.
//
// Pide una clave (la de la URL que sale en el log al arrancar, guardada en
// bot.json): quien la tiene puede mandarle órdenes.

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed panel.html
var panelHTML []byte

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
	r.part += stripANSI(string(p))
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
	Server    string         `json:"server"`
	Connected bool           `json:"connected"`
	Queue     []string       `json:"queue"`
	Current   string         `json:"current"`
	Inventory map[string]int `json:"inventory"`
	Limits    string         `json:"limits"`
	Online    []string       `json:"online"`
}

func panelSnapshot() panelState {
	st := panelState{Server: serverAddr, Limits: describeLimits()}
	b := current.Load()
	if b == nil {
		st.stats.Name = botName
		return st
	}
	st.stats = b.stats()
	st.Connected = b.ctx.Err() == nil && !b.connectedAt.IsZero()
	if b.Queue != nil {
		st.Queue, st.Current = b.Queue.Pending(), b.Queue.Current()
	}
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
	ok := func(w http.ResponseWriter, r *http.Request) bool {
		t := r.URL.Query().Get("t")
		if t == "" {
			if c, err := r.Cookie("mcbot"); err == nil {
				t = c.Value
			}
		}
		if subtle.ConstantTimeCompare([]byte(t), []byte(token)) != 1 {
			http.Error(w, "falta la clave: abre la URL que sale en el log del bot (…/?t=…)", http.StatusUnauthorized)
			return false
		}
		http.SetCookie(w, &http.Cookie{Name: "mcbot", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 30 * 24 * 3600})
		return true
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if !ok(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(panelHTML)
	})
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		if !ok(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(panelSnapshot())
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
			http.Error(w, "falta la orden", http.StatusBadRequest)
			return
		}
		line := strings.TrimSpace(req.Line)
		if secret(line) {
			log.Printf("[panel] %s ***", strings.Fields(line)[0])
		} else {
			log.Printf("[panel] %s", line)
		}
		go execLine(line)
		w.WriteHeader(http.StatusNoContent)
	})
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
		host = net.JoinHostPort(localIP(), port)
	}
	log.Printf("panel: http://%s/?t=%s", host, token)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	go func() { _ = srv.Serve(ln) }()
}

// localIP: la IP de este equipo en la red (para la URL del panel).
func localIP() string {
	c, err := net.Dial("udp", "192.0.2.1:9") // (no manda nada: solo elige la interfaz)
	if err != nil {
		return "127.0.0.1"
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String()
}
