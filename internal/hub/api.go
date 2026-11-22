package hub

// La API pública del hub (docs/API.md): lista de bots, estado de uno (con su
// tarea y su cola), mandarle una orden, su log reciente y un flujo de eventos
// (SSE) con el chat y las tareas de todos los bots. Se entra con la clave del
// panel: "Authorization: Bearer <clave>" (o ?t=).
//
// Para otros módulos del programa (p. ej. un puente a Discord), que no
// tienen que pasar por HTTP:
//
//	hub.SendCommand("id", "/status")   manda una orden a un bot
//	ch, cancel := hub.Subscribe(64)    sus eventos (chat, tarea, cola, bot)
//	hub.Bots()                         el estado de todos

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cubit/internal/i18n"
)

// Event: lo que cuenta /api/events (un evento de un bot).
type Event struct {
	Bot    string `json:"bot"`  // el id del bot
	Name   string `json:"name"` // su nombre en el juego
	Time   string `json:"time"`
	Type   string `json:"type"`             // chat, system, whisper, task, queue, bot
	Text   string `json:"text,omitempty"`   // el mensaje, o el motivo de un fallo
	From   string `json:"from,omitempty"`   // quién habló (chat, whisper)
	Task   string `json:"task,omitempty"`   // el nombre de la tarea (task)
	Status string `json:"status,omitempty"` // task: started, finished, failed, stopped; bot: started, stopped
	Data   any    `json:"data,omitempty"`
}

// current: el hub en marcha (nil si el programa no está en modo hub).
var current atomic.Pointer[hub]

// ErrNoHub: el programa no corre en modo hub.
var ErrNoHub = i18n.Errorf("the hub is not running", "el hub no está en marcha")

// SendCommand manda una línea de consola ("/chop oak ~ ~ ~", "hola") al bot id.
// Vuelve en cuanto el bot la recibe (no espera a que la ejecute).
func SendCommand(id, line string) error {
	h := current.Load()
	if h == nil {
		return ErrNoHub
	}
	return h.sendCommand(id, line)
}

// Subscribe: los eventos de todos los bots, en un canal con buffer buf (si el
// que escucha no da abasto, se pierden los que no caben). cancel cierra la
// suscripción.
func Subscribe(buf int) (<-chan Event, func()) {
	h := current.Load()
	if h == nil {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}
	return h.events.subscribe(buf)
}

// Bots: el estado de todos los bots (como /api/bots).
func Bots() []BotState {
	h := current.Load()
	if h == nil {
		return nil
	}
	return h.state().Bots
}

// ---- difusión ----

type broker struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

func (br *broker) subscribe(buf int) (<-chan Event, func()) {
	if buf < 1 {
		buf = 1
	}
	ch := make(chan Event, buf)
	br.mu.Lock()
	if br.subs == nil {
		br.subs = map[chan Event]struct{}{}
	}
	br.subs[ch] = struct{}{}
	br.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			br.mu.Lock()
			delete(br.subs, ch)
			br.mu.Unlock()
		})
	}
}

func (br *broker) publish(e Event) {
	if e.Time == "" {
		e.Time = time.Now().UTC().Format(time.RFC3339)
	}
	br.mu.Lock()
	defer br.mu.Unlock()
	for ch := range br.subs {
		select {
		case ch <- e:
		default: // (no da abasto: se pierde)
		}
	}
}

func (br *broker) count() int {
	br.mu.Lock()
	defer br.mu.Unlock()
	return len(br.subs)
}

// ---- hacia los bots ----

// botGet / botPost hablan con el panel interno de un bot en marcha.
func (h *hub) botPort(id string) (int, error) {
	b := h.bot(id)
	if b == nil {
		return 0, i18n.Errorf("there is no bot %s", "no hay ningún bot %s", id)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cmd == nil || b.port == 0 {
		return 0, i18n.Errorf("the bot %s is off", "el bot %s está apagado", id)
	}
	return b.port, nil
}

func (h *hub) botReq(method string, port int, path string, body io.Reader, timeout time.Duration) (*http.Response, error) {
	req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c := http.Client{Timeout: timeout}
	return c.Do(req)
}

func (h *hub) sendCommand(id, line string) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return i18n.Errorf("missing command", "falta la orden")
	}
	if len(line) > 1024 || strings.ContainsAny(line, "\r\n\x00") {
		return i18n.Errorf("the command is too long or has line breaks", "la orden es demasiado larga o tiene saltos de línea")
	}
	port, err := h.botPort(id)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"line": line})
	resp, err := h.botReq(http.MethodPost, port, "/cmd", strings.NewReader(string(body)), 3*time.Second)
	if err != nil {
		return i18n.Errorf("the bot is not answering", "el bot no responde")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return i18n.Errorf("the bot refused it (%d)", "el bot la rechazó (%d)", resp.StatusCode)
	}
	return nil
}

// pollEvents: cada segundo, pide a cada bot en marcha sus eventos nuevos (solo
// mientras alguien escuche) y los difunde.
func (h *hub) pollEvents(ctx context.Context) {
	cursors := map[string]int{}
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if h.events.count() == 0 {
			clear(cursors)
			continue
		}
		var wg sync.WaitGroup
		var mu sync.Mutex
		for _, id := range h.ids() {
			port, err := h.botPort(id)
			if err != nil {
				mu.Lock()
				delete(cursors, id)
				mu.Unlock()
				continue
			}
			mu.Lock()
			after, known := cursors[id]
			mu.Unlock()
			if !known {
				after = -1
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				evs, next, ok := h.fetchEvents(port, after)
				if !ok {
					return
				}
				mu.Lock()
				cursors[id] = next
				mu.Unlock()
				name := h.info(id).Name
				for _, e := range evs {
					e.Bot, e.Name = id, name
					h.events.publish(e)
				}
			}()
		}
		wg.Wait()
	}
}

// fetchEvents: /events de un bot desde after. Si el bot se reinició (next < after), empieza de cero.
func (h *hub) fetchEvents(port, after int) ([]Event, int, bool) {
	resp, err := h.botReq(http.MethodGet, port, fmt.Sprintf("/events?after=%d", after), nil, 2*time.Second)
	if err != nil {
		return nil, 0, false
	}
	defer resp.Body.Close()
	var out struct {
		Events []Event `json:"events"`
		Next   int     `json:"next"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out) != nil {
		return nil, 0, false
	}
	if after > out.Next { // otro proceso: los números empiezan de nuevo
		return nil, 0, true
	}
	return out.Events, out.Next, true
}

// ---- rutas ----

func (h *hub) apiRoutes(mux *http.ServeMux, ok func(http.ResponseWriter, *http.Request) bool) {
	// GET /api/events?bot=<id>[,<id>]&type=chat,task: SSE
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "GET", http.StatusMethodNotAllowed)
			return
		}
		if !ok(w, r) {
			return
		}
		fl, can := w.(http.Flusher)
		if !can {
			i18n.HTTPMsg(w, r, http.StatusInternalServerError, "streaming not supported", "no se puede mandar en directo")
			return
		}
		bots, types := csvSet(r.URL.Query().Get("bot")), csvSet(r.URL.Query().Get("type"))
		ch, cancel := h.events.subscribe(256)
		defer cancel()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		fmt.Fprint(w, "retry: 3000\n: connected\n\n")
		fl.Flush()
		beat := time.NewTicker(15 * time.Second)
		defer beat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-h.ctx.Done():
				return
			case <-beat.C:
				fmt.Fprint(w, ": keepalive\n\n")
				fl.Flush()
			case e := <-ch:
				if (len(bots) > 0 && !bots[e.Bot]) || (len(types) > 0 && !types[e.Type]) {
					continue
				}
				b, _ := json.Marshal(e)
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, b)
				fl.Flush()
			}
		}
	})
}

// apiGet atiende los GET de /api/bots/<id>[/log]: devuelve si lo atendió.
func (h *hub) apiGet(w http.ResponseWriter, r *http.Request, id, action string, jsonOut func(http.ResponseWriter, any)) bool {
	switch action {
	case "":
		for _, bs := range h.state().Bots {
			if bs.ID == id {
				jsonOut(w, bs)
				return true
			}
		}
		i18n.HTTPMsg(w, r, http.StatusNotFound, "there is no bot "+id, "no hay ningún bot "+id)
	case "log":
		port, err := h.botPort(id)
		if err != nil {
			i18n.HTTPError(w, r, http.StatusConflict, err)
			return true
		}
		resp, err := h.botReq(http.MethodGet, port, "/log?after="+urlInt(r.URL.Query().Get("after")), nil, 3*time.Second)
		if err != nil {
			i18n.HTTPMsg(w, r, http.StatusBadGateway, "the bot is not answering", "el bot no responde")
			return true
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(w, io.LimitReader(resp.Body, 4<<20))
	default:
		return false
	}
	return true
}

// apiCommand: POST /api/bots/<id>/command {"line": "/status"}.
func (h *hub) apiCommand(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Line string `json:"line"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		i18n.HTTPMsg(w, r, http.StatusBadRequest, "unreadable request (expected {\"line\": \"...\"})", "petición ilegible (se espera {\"line\": \"...\"})")
		return
	}
	if err := h.sendCommand(id, req.Line); err != nil {
		code := http.StatusBadRequest
		if h.bot(id) == nil {
			code = http.StatusNotFound
		} else if _, e := h.botPort(id); e != nil {
			code = http.StatusConflict
		}
		i18n.HTTPError(w, r, code, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"sent"}` + "\n"))
}

func csvSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			m[p] = true
		}
	}
	return m
}

// urlInt: solo dígitos (para pasar un parámetro al bot sin colar nada).
func urlInt(s string) string {
	for _, c := range s {
		if c < '0' || c > '9' {
			return "0"
		}
	}
	if s == "" {
		return "0"
	}
	return s
}
