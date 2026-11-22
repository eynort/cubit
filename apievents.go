package main

// Los eventos del bot para la API pública (el hub los junta de todos los
// bots en /api/events): líneas del chat, tareas que empiezan y acaban, cambios
// de la cola. Un anillo en memoria; el hub los pide por /events?after=N.

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"
)

type apiEvent struct {
	Seq  int    `json:"seq"`
	Time string `json:"time"`
	Type string `json:"type"` // chat, system, whisper, task, queue
	Text string `json:"text,omitempty"`
	From string `json:"from,omitempty"`
	Task string `json:"task,omitempty"`
	// Status de un evento "task": started, finished, failed, stopped
	Status string `json:"status,omitempty"`
	Data   any    `json:"data,omitempty"`
}

const eventsMax = 200

var apiEvents struct {
	sync.Mutex
	list []apiEvent
	next int
}

func emitEvent(e apiEvent) {
	apiEvents.Lock()
	defer apiEvents.Unlock()
	e.Seq = apiEvents.next
	apiEvents.next++
	if e.Time == "" {
		e.Time = time.Now().UTC().Format(time.RFC3339)
	}
	apiEvents.list = append(apiEvents.list, e)
	if n := len(apiEvents.list) - eventsMax; n > 0 {
		apiEvents.list = append([]apiEvent(nil), apiEvents.list[n:]...)
	}
}

// eventsSince: los eventos con seq >= after (after < 0: ninguno, solo dónde va).
func eventsSince(after int) ([]apiEvent, int) {
	apiEvents.Lock()
	defer apiEvents.Unlock()
	if after < 0 {
		return []apiEvent{}, apiEvents.next
	}
	out := []apiEvent{}
	for _, e := range apiEvents.list {
		if e.Seq >= after {
			out = append(out, e)
		}
	}
	return out, apiEvents.next
}

func eventsHandler(w http.ResponseWriter, r *http.Request) {
	after, err := strconv.Atoi(r.URL.Query().Get("after"))
	if err != nil {
		after = -1
	}
	list, next := eventsSince(after)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"events": list, "next": next})
}
