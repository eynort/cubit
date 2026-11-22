package hub

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeSess struct {
	mu      sync.Mutex
	openErr error
	sent    []string
	cmd     func(dcInteraction)
	state   func(bool)
	closed  bool
}

func (f *fakeSess) Open(st func(bool), c func(dcInteraction)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state, f.cmd = st, c
	return f.openErr
}
func (f *fakeSess) RegisterCommands(string) error { return nil }
func (f *fakeSess) Send(ch, t string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, t)
	return nil
}
func (f *fakeSess) Close() error { f.mu.Lock(); f.closed = true; f.mu.Unlock(); return nil }

type fakeHub struct {
	list []dcBot
	ran  []string
}

func (f *fakeHub) dcBots() []dcBot { return f.list }
func (f *fakeHub) runCommand(id, line string) error {
	f.ran = append(f.ran, id+": "+line)
	return nil
}

const tok = "MTIzNDU2Nzg5MDEyMzQ1Njc4.GabCde.abcdefghijklmnopqrstuvwxyz0123456789"

func newMgr(allowed ...string) (*discordMgr, *fakeHub) {
	h := &fakeHub{list: []dcBot{
		{ID: "b1", Name: "Rocky", Running: true, Connected: true, Task: "digging", Health: 20},
		{ID: "b2", Name: "Sleepy"},
	}}
	return newDiscordMgr(h, discordConfig{Enabled: true, Token: tok, ChannelID: "123456789012345678", Allowed: allowed}, func(discordConfig) {}, nil), h
}

func ask(m *discordMgr, user, cmd string, opts map[string]string) (string, bool) {
	var got string
	var eph bool
	m.handle(dcInteraction{UserID: user, Command: cmd, Options: opts, Reply: func(t string, e bool) error { got, eph = t, e; return nil }})
	return got, eph
}

func TestDiscordPermissions(t *testing.T) {
	m, h := newMgr("111111111111111111")
	got, eph := ask(m, "999999999999999999", "bot", map[string]string{"name": "rocky", "command": "/stop"})
	if !eph || !strings.Contains(got, "Sorry mate") || len(h.ran) != 0 {
		t.Fatalf("a stranger got through: %q eph=%v ran=%v", got, eph, h.ran)
	}
	if _, eph := ask(m, "", "bots", nil); !eph {
		t.Fatal("an empty user id must be refused")
	}
	got, eph = ask(m, "111111111111111111", "bot", map[string]string{"name": "ROCKY", "command": "/goto 1 2 3"})
	if eph || len(h.ran) != 1 || h.ran[0] != "b1: /goto 1 2 3" {
		t.Fatalf("allowed user: %q ran=%v", got, h.ran)
	}
	if _, eph := ask(m, "111111111111111111", "bot", map[string]string{"name": "nobody", "command": "x"}); !eph {
		t.Fatal("unknown bot should be an ephemeral error")
	}
	if got, _ := ask(m, "111111111111111111", "bot", map[string]string{"name": "Sleepy", "command": "x"}); !strings.Contains(got, "off") || len(h.ran) != 1 {
		t.Fatalf("a stopped bot must not get commands: %q", got)
	}
	list, _ := ask(m, "111111111111111111", "bots", nil)
	if !strings.Contains(list, "Rocky") || !strings.Contains(list, "Sleepy") || !strings.Contains(list, "digging") {
		t.Fatalf("/bots: %q", list)
	}
	if st, _ := ask(m, "111111111111111111", "status", map[string]string{"name": "Rocky"}); !strings.Contains(st, "digging") {
		t.Fatalf("/status: %q", st)
	}
}

func TestDiscordMentionsNeutralized(t *testing.T) {
	m, _ := newMgr("111111111111111111")
	got, _ := ask(m, "111111111111111111", "bot", map[string]string{"name": "Rocky", "command": "@everyone `x`"})
	if strings.Contains(got, "@everyone") || strings.Contains(got, "`x`") {
		t.Fatalf("not neutralized: %q", got)
	}
}

func TestDiscordRateLimitAndBatching(t *testing.T) {
	m, _ := newMgr()
	m.state = "connected"
	f := &fakeSess{}
	for i := 0; i < 50; i++ {
		m.bridge("Rocky", fmt.Sprintf("line %d", i))
	}
	m.flush(f, "c")
	if len(f.sent) != 1 {
		t.Fatalf("one flush = one message, got %d", len(f.sent))
	}
	if n := strings.Count(f.sent[0], "\n") + 1; n != 50 {
		t.Fatalf("lines should be batched together, got %d", n)
	}
	if !strings.HasPrefix(f.sent[0], "**Rocky**: line 0") {
		t.Fatalf("prefix: %q", f.sent[0])
	}
	m.flush(f, "c")
	if len(f.sent) != 1 {
		t.Fatal("an empty queue must send nothing")
	}
	// mucho texto: se parte en varios mensajes, cada uno bajo el límite
	for i := 0; i < 200; i++ {
		m.bridge("Rocky", strings.Repeat("x", 100))
	}
	for i := 0; i < 20; i++ {
		m.flush(f, "c")
	}
	for _, s := range f.sent {
		if len(s) > 2000 {
			t.Fatalf("message too long: %d", len(s))
		}
	}
	// desbordamiento: se avisa de lo que se tiró
	m2, _ := newMgr()
	m2.state = "connected"
	for i := 0; i < dcQueueMax+25; i++ {
		m2.bridge("Rocky", "spam")
	}
	if out := m2.out.next(); !strings.Contains(out, "25 lines skipped") {
		t.Fatalf("no overflow note: %q", out[:min(80, len(out))])
	}
}

func TestDiscordPumpOnePerTick(t *testing.T) {
	m, _ := newMgr()
	m.state = "connected"
	m.every = 20 * time.Millisecond
	f := &fakeSess{}
	stop := make(chan struct{})
	for i := 0; i < 300; i++ {
		m.bridge("Rocky", strings.Repeat("y", 150))
	}
	go m.pump(f, "c", stop)
	time.Sleep(110 * time.Millisecond)
	close(stop)
	f.mu.Lock()
	n := len(f.sent)
	f.mu.Unlock()
	if n < 2 || n > 7 {
		t.Fatalf("expected about 1 message per tick (20ms), got %d in 110ms", n)
	}
}

func TestDiscordBridgeOffWhenDisconnected(t *testing.T) {
	m, _ := newMgr()
	m.bridge("Rocky", "hello") // state "off"
	if m.out.next() != "" {
		t.Fatal("nothing should queue while disconnected")
	}
}

func TestChatEvent(t *testing.T) {
	for in, want := range map[string]string{
		"chat: all done mate":                    "all done mate",
		"[excavar] chat: need a pickaxe":         "need a pickaxe",
		"<Steve> hi there":                       "<Steve> hi there",
		"[sistema] Steve joined the game":        "",
		"tarea: excavar":                         "",
		"[excavar] lava: espero a estar a salvo": "",
	} {
		if got := chatEvent(in); got != want {
			t.Errorf("chatEvent(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTokenMasking(t *testing.T) {
	if got := maskToken(tok); strings.Contains(got, tok[:10]) || !strings.HasSuffix(got, tok[len(tok)-4:]) || len(got) > 40 {
		t.Fatalf("mask: %q", got)
	}
	if maskToken("short") != "••••••••" || maskToken("") != "" {
		t.Fatal("short/empty mask")
	}
	m, _ := newMgr()
	b, _ := json.Marshal(m.view())
	if strings.Contains(string(b), tok) || strings.Contains(string(b), tok[:20]) {
		t.Fatalf("the view leaks the token: %s", b)
	}
	if redact("boom "+tok, tok) != "boom ***" {
		t.Fatal("redact")
	}
}

func TestDiscordAPINeverLeaksToken(t *testing.T) {
	var saved discordConfig
	m := newDiscordMgr(&fakeHub{}, discordConfig{}, func(c discordConfig) { saved = c }, func(string) dcSession { return &fakeSess{} })
	h := &hub{discord: m}
	mux := http.NewServeMux()
	h.discordRoutes(mux, func(http.ResponseWriter, *http.Request) bool { return true })
	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/discord", strings.NewReader(body)))
		return w
	}
	w := post(fmt.Sprintf(`{"enabled":true,"token":%q,"channel_id":"123456789012345678","allowed":["111111111111111111, 222222222222222222"]}`, tok))
	if w.Code != 200 || strings.Contains(w.Body.String(), tok) {
		t.Fatalf("POST: %d %s", w.Code, w.Body)
	}
	if saved.Token != tok || len(saved.Allowed) != 2 || !saved.Enabled {
		t.Fatalf("not saved: %+v", saved)
	}
	// token vacío = se queda el que había
	w = post(`{"enabled":true,"token":"","channel_id":"123456789012345678","allowed":[]}`)
	if saved.Token != tok || w.Code != 200 {
		t.Fatalf("empty token must keep the old one: %+v", saved)
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/discord", nil))
	if strings.Contains(w.Body.String(), tok) || !strings.Contains(w.Body.String(), `"token_set":true`) {
		t.Fatalf("GET: %s", w.Body)
	}
	if w := post(`{"channel_id":"nope"}`); w.Code != 400 {
		t.Fatalf("bad channel accepted: %d", w.Code)
	}
	if w := post(`{"allowed":["abc"]}`); w.Code != 400 {
		t.Fatalf("bad user id accepted: %d", w.Code)
	}
	if w := post(`{"token":"hi there","channel_id":""}`); w.Code != 400 {
		t.Fatalf("bad token accepted: %d", w.Code)
	}
	post(`{"enabled":false,"clear_token":true}`)
	if saved.Token != "" || saved.Enabled {
		t.Fatalf("clear: %+v", saved)
	}
	m.restart()
}

func TestDiscordReconnectBackoffAndState(t *testing.T) {
	var mu sync.Mutex
	n := 0
	good := &fakeSess{}
	m := newDiscordMgr(&fakeHub{}, discordConfig{Enabled: true, Token: tok}, func(discordConfig) {}, func(string) dcSession {
		mu.Lock()
		defer mu.Unlock()
		n++
		if n == 1 {
			return &fakeSess{openErr: errors.New("dial failed for " + tok)}
		}
		return good
	})
	m.every = 10 * time.Millisecond
	m.restart()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && m.view().State != "error" {
		time.Sleep(2 * time.Millisecond)
	}
	v := m.view()
	if v.State != "error" || strings.Contains(v.Error, tok) {
		t.Fatalf("after failed open: %+v", v)
	}
	// el reintento llega a los 2 s: no se espera, se comprueba que sigue pendiente
	if m.view().State == "connected" {
		t.Fatal("connected too early")
	}
	// apagar corta los reintentos
	m.mu.Lock()
	m.cfg.Enabled = false
	m.mu.Unlock()
	m.restart()
	if m.view().State != "off" {
		t.Fatal("not off")
	}
}

func TestDiscordConnectedState(t *testing.T) {
	f := &fakeSess{}
	m := newDiscordMgr(&fakeHub{}, discordConfig{Enabled: true, Token: tok}, func(discordConfig) {}, func(string) dcSession { return f })
	m.restart()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && m.view().State != "connected" {
		time.Sleep(2 * time.Millisecond)
	}
	if m.view().State != "connected" {
		t.Fatalf("state %s", m.view().State)
	}
	f.mu.Lock()
	st := f.state
	f.mu.Unlock()
	st(false)
	if m.view().State != "connecting" {
		t.Fatal("a dropped gateway should read as connecting")
	}
	st(true)
	if m.view().State != "connected" {
		t.Fatal("resumed")
	}
	m.cfg.Enabled = false
	m.restart()
}
