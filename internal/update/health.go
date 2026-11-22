package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Tras sustituir el binario se apunta una "actualización pendiente"
// (update-pending.json). El binario nuevo, al arrancar, la cuenta; si el hub
// llega a estar sirviendo bien dentro de HealthWindow, la borra (MarkOK, y deja
// update-ok.json). Si no, vuelve al binario de antes: al siguiente arranque
// (se cayó) o por el vigilante (se quedó colgado).
const (
	pendingFile    = "update-pending.json"
	okFile         = "update-ok.json"
	rolledBackFile = "update-rolledback.json"
	HealthWindow   = 60 * time.Second
)

type state struct {
	From     string    `json:"from"`
	To       string    `json:"to"`
	Exe      string    `json:"exe"`
	At       time.Time `json:"at"`
	Launches int       `json:"launches"` // arranques del binario nuevo sin llegar a "bien"
}

// RolledBack: la última actualización que se deshizo (el panel lo cuenta).
type RolledBack struct {
	From   string    `json:"from"` // la que no arrancó
	To     string    `json:"to"`   // a la que se volvió
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

func readJSON(path string, v any) bool {
	b, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(b, v) == nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeState(dataDir string, s *state) error {
	return writeJSON(filepath.Join(dataDir, pendingFile), s)
}

// Pending: hay una actualización sin confirmar (para el binario en marcha, current).
func Pending(dataDir, current string) bool {
	var s state
	return readJSON(filepath.Join(dataDir, pendingFile), &s) && s.To == current
}

// BeginStart se llama al arrancar el hub. Si el binario en marcha es la versión
// recién instalada: cuenta el arranque; si ya hubo uno sin confirmar, deshace la
// actualización (rolledBack=true: hay que volver a ejecutar exe, ya restaurado).
func BeginStart(dataDir, current string) (rolledBack bool, exe string) {
	path := filepath.Join(dataDir, pendingFile)
	var s state
	if !readJSON(path, &s) {
		return false, ""
	}
	if s.To != current {
		_ = os.Remove(path) // (otra versión: ya no hay nada que vigilar)
		return false, ""
	}
	s.Launches++
	if s.Launches < 2 {
		_ = writeJSON(path, &s)
		return false, ""
	}
	if err := DoRollback(dataDir, "the new version did not start correctly"); err != nil {
		_ = os.Remove(path) // (no se puede volver atrás: no seguir contando)
		return false, ""
	}
	return true, s.Exe
}

// DoRollback restaura el binario anterior y deja constancia para el panel.
func DoRollback(dataDir, reason string) error {
	var s state
	path := filepath.Join(dataDir, pendingFile)
	if !readJSON(path, &s) {
		return os.ErrNotExist
	}
	if err := Rollback(s.Exe); err != nil {
		return err
	}
	_ = writeJSON(filepath.Join(dataDir, rolledBackFile), &RolledBack{From: s.To, To: s.From, Reason: reason, At: time.Now()})
	_ = os.Remove(path)
	return nil
}

// MarkOK: el hub nuevo funciona; la actualización queda confirmada.
func MarkOK(dataDir, current string) {
	path := filepath.Join(dataDir, pendingFile)
	var s state
	if !readJSON(path, &s) || s.To != current {
		return
	}
	_ = writeJSON(filepath.Join(dataDir, okFile), map[string]any{"version": current, "from": s.From, "at": time.Now()})
	_ = os.Remove(path)
	_ = os.Remove(filepath.Join(dataDir, rolledBackFile))
}

// NoteCleanExit: el hub se para a propósito antes de confirmarse (Ctrl-C): no
// cuenta como un arranque fallido.
func NoteCleanExit(dataDir, current string) {
	path := filepath.Join(dataDir, pendingFile)
	var s state
	if readJSON(path, &s) && s.To == current && s.Launches > 0 {
		s.Launches--
		_ = writeJSON(path, &s)
	}
}

// LastRollback devuelve la vuelta atrás que el panel aún no ha descartado.
func LastRollback(dataDir string) *RolledBack {
	var r RolledBack
	if readJSON(filepath.Join(dataDir, rolledBackFile), &r) {
		return &r
	}
	return nil
}

// DismissRollback: el usuario ya lo vio.
func DismissRollback(dataDir string) { _ = os.Remove(filepath.Join(dataDir, rolledBackFile)) }
