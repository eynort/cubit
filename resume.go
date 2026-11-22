package main

// Retomar el trabajo tras reiniciar el programa (la actualización del hub
// reinicia el hub y sus bots). Las tareas son funciones, no se guardan: se
// guarda la orden que las empezó ("/chop oak 100 64 200") en resume.json, y el
// bot nuevo, al entrar, la vuelve a dar. Solo lo pide el hub (/saveresume)
// justo antes de actualizar: parar un bot a mano no deja nada que retomar.

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

const resumeFile = "resume.json"

// resumeMaxAge: un archivo más viejo no se retoma (se quedó de un reinicio que no volvió).
const resumeMaxAge = 15 * time.Minute

var orderLine struct {
	sync.Mutex
	line string
}

// noteOrderLine apunta la orden que se va a ejecutar; startWork la recoge.
func noteOrderLine(line string) {
	orderLine.Lock()
	orderLine.line = strings.TrimSpace(line)
	orderLine.Unlock()
}

func takeOrderLine() string {
	orderLine.Lock()
	defer orderLine.Unlock()
	l := orderLine.line
	orderLine.line = ""
	return l
}

type resumeData struct {
	Line string    `json:"line"`
	At   time.Time `json:"at"`
}

// saveResume apunta el trabajo en marcha (o nada, si no hay).
func saveResume() {
	b := current.Load()
	var line string
	if b != nil && b.Tasks != nil {
		b.work.mu.Lock()
		if b.work.name != "" && b.work.line != "" && (b.Tasks.Status() != "" || b.work.resuming) {
			line = b.work.line
		}
		b.work.mu.Unlock()
	}
	if line == "" {
		_ = os.Remove(resumeFile)
		return
	}
	data, _ := json.Marshal(resumeData{Line: line, At: time.Now()})
	if err := os.WriteFile(resumeFile, data, 0o600); err != nil {
		log.Printf("resume: no se guardó: %v", err)
		return
	}
	log.Printf(tr("resume: saved %s to resume it after a restart", "resume: apunto %s para retomarlo al reiniciar"), line)
}

var resumeOnce sync.Once

// resumeAfterRestart: al entrar por primera vez tras un reinicio pedido por el
// hub, vuelve a dar la orden que estaba en marcha.
func (b *Bot) resumeAfterRestart() {
	resumeOnce.Do(func() {
		raw, err := os.ReadFile(resumeFile)
		if err != nil {
			return
		}
		_ = os.Remove(resumeFile) // (una sola vez)
		var d resumeData
		if json.Unmarshal(raw, &d) != nil || d.Line == "" || time.Since(d.At) > resumeMaxAge {
			return
		}
		time.Sleep(6 * time.Second) // (que cargue el mundo)
		if b.ctx.Err() != nil || b.Tasks.Status() != "" {
			return
		}
		log.Printf(tr("resume: resuming %s after the restart", "resume: retomo %s tras reiniciar"), d.Line)
		b.exec(d.Line)
	})
}
