package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"cubit/internal/web"
)

// fakeBot: un panel de bot falso (/cmd, /events, /log) y un hub con él.
type fakeBot struct {
	mu   sync.Mutex
	cmds []string
	evs  []Event
	srv  *httptest.Server
}

func newFakeHub(t *testing.T) (*hub, *fakeBot) {
	t.Helper()
	const token = "secret"
	fb := &fakeBot{}
	fb.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		fb.mu.Lock()
		defer fb.mu.Unlock()
		switch r.URL.Path {
		case "/cmd":
			var req struct{ Line string }
			_ = json.NewDecoder(r.Body).Decode(&req)
			fb.cmds = append(fb.cmds, req.Line)
			w.WriteHeader(http.StatusNoContent)
		case "/events":
			after, _ := strconv.Atoi(r.URL.Query().Get("after"))
			out := []Event{}
			if after >= 0 {
				for i, e := range fb.evs {
					if i >= after {
						out = append(out, e)
					}
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"events": out, "next": len(fb.evs)})
		case "/log":
			_ = json.NewEncoder(w).Encode(map[string]any{"lines": []string{"hola"}, "next": 1})
		}
	}))
	t.Cleanup(fb.srv.Close)
	u, _ := url.Parse(fb.srv.URL)
	port, _ := strconv.Atoi(u.Port())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h := &hub{bots: map[string]*hubBot{"b1": {id: "b1", cmd: &exec.Cmd{}, port: port}}, ctx: ctx}
	h.cfg.Token = token
	h.cfg.Bots = []string{"b1"}
	return h, fb
}

func TestSendCommand(t *testing.T) {
	h, fb := newFakeHub(t)
	if err := h.sendCommand("b1", "/status"); err != nil {
		t.Fatal(err)
	}
	if len(fb.cmds) != 1 || fb.cmds[0] != "/status" {
		t.Fatalf("cmds = %q", fb.cmds)
	}
	for _, bad := range []string{"", "  ", "/say a\n/stop"} {
		if h.sendCommand("b1", bad) == nil {
			t.Errorf("%q debía fallar", bad)
		}
	}
	if h.sendCommand("nope", "/status") == nil {
		t.Error("bot inexistente")
	}
	h.bots["b1"].cmd = nil
	if h.sendCommand("b1", "/status") == nil {
		t.Error("bot apagado")
	}
}

func TestSubscribePublishFilters(t *testing.T) {
	var br broker
	ch, cancel := br.subscribe(2)
	br.publish(Event{Bot: "a", Type: "chat"})
	br.publish(Event{Bot: "a", Type: "chat"})
	br.publish(Event{Bot: "a", Type: "chat"}) // no cabe: se pierde sin bloquear
	if len(ch) != 2 {
		t.Fatalf("len = %d", len(ch))
	}
	cancel()
	cancel() // dos veces: sin pánico
	if br.count() != 0 {
		t.Fatal("count")
	}
}

func TestAPIEventsSSE(t *testing.T) {
	h, fb := newFakeHub(t)
	mux := http.NewServeMux()
	ok := web.KeyCheck(h.cfg.Token, "x", "x")
	h.apiRoutes(mux, ok)
	go h.pollEvents(h.ctx)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// sin clave: 401
	resp, err := http.Get(srv.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("sin clave: %d", resp.StatusCode)
	}

	req, _ := http.NewRequest("GET", srv.URL+"/api/events?type=chat", nil)
	req.Header.Set("Authorization", "Bearer secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	if c := resp.Header.Values("Set-Cookie"); len(c) != 0 {
		t.Fatalf("con Bearer no se pone cookie: %v", c)
	}
	go func() { // el bot dice cosas (una tarea, que el filtro deja fuera; y un chat)
		time.Sleep(1500 * time.Millisecond)
		fb.mu.Lock()
		fb.evs = append(fb.evs, Event{Type: "task", Task: "talar", Status: "started"}, Event{Type: "chat", From: "Steve", Text: "hola"})
		fb.mu.Unlock()
	}()
	got := make(chan string, 4)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if l := sc.Text(); strings.HasPrefix(l, "data: ") {
				got <- l[6:]
			}
		}
	}()
	select {
	case l := <-got:
		var e Event
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatal(err)
		}
		if e.Type != "chat" || e.From != "Steve" || e.Bot != "b1" {
			t.Fatalf("evento %+v", e)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("no llegó el evento")
	}
}

func TestAPIGetLogAndCommand(t *testing.T) {
	h, fb := newFakeHub(t)
	mux := http.NewServeMux()
	h.apiRoutes(mux, func(http.ResponseWriter, *http.Request) bool { return true })
	rec := httptest.NewRecorder()
	if !h.apiGet(rec, httptest.NewRequest("GET", "/api/bots/b1/log?after=0", nil), "b1", "log", func(http.ResponseWriter, any) {}) {
		t.Fatal("log no atendido")
	}
	if !strings.Contains(rec.Body.String(), "hola") {
		t.Fatalf("log = %q", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.apiCommand(rec, httptest.NewRequest("POST", "/api/bots/b1/command", strings.NewReader(`{"line":"/stop"}`)), "b1")
	if rec.Code != http.StatusAccepted || len(fb.cmds) != 1 {
		t.Fatalf("code %d cmds %q", rec.Code, fb.cmds)
	}
	rec = httptest.NewRecorder()
	h.apiCommand(rec, httptest.NewRequest("POST", "/api/bots/zz/command", strings.NewReader(`{"line":"/stop"}`)), "zz")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.apiCommand(rec, httptest.NewRequest("POST", "/api/bots/b1/command", strings.NewReader(`nonsense`)), "b1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code %d", rec.Code)
	}
}
