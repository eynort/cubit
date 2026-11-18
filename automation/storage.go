package automation

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// Storage guarda las zonas protegidas: donde el servidor repone lo que el bot
// rompe (la protección del spawn). Ahí no se cava. Se guarda en un archivo
// para que sobreviva a reinicios del bot.
type Storage struct {
	mu     sync.Mutex
	denied []denial // los "no" de ahora (NoteDenied), sin guardar
	file   string
	// Protected: sitios donde el servidor no deja romper ni poner
	Protected []world.BlockPos `json:"protected,omitempty"`
}

// protectRadius: lo que se evita alrededor de un sitio donde el servidor dijo que no.
const protectRadius = 16

// NoteProtected apunta que en p el servidor no deja construir ni romper.
func (s *Storage) NoteProtected(p world.BlockPos) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, q := range s.Protected {
		if abs(q.X-p.X) <= protectRadius/2 && abs(q.Z-p.Z) <= protectRadius/2 {
			return false // ya estaba (casi) apuntado
		}
	}
	s.Protected = append(s.Protected, p)
	s.save()
	return true
}

// denial es un "no tienes permiso" (o un bloque que el servidor devolvió):
// dónde y cuándo. Solo en memoria (las tareas que no se fían de las zonas
// guardadas, como /excavate, miran estas: precisas y de ahora).
type denial struct {
	p world.BlockPos
	t time.Time
}

// NoteDenied apunta que en p el servidor acaba de decir que no.
func (s *Storage) NoteDenied(p world.BlockPos) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.denied) > 512 {
		s.denied = s.denied[256:]
	}
	s.denied = append(s.denied, denial{p, time.Now()})
}

// DeniedNear: el servidor dijo que no a menos de r bloques (en x y z) de p desde since.
func (s *Storage) DeniedNear(p world.BlockPos, r int, since time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.denied {
		if !d.t.Before(since) && abs(d.p.X-p.X) <= r && abs(d.p.Z-p.Z) <= r {
			return true
		}
	}
	return false
}

// IsProtectedSince es IsProtected solo con las zonas apuntadas desde la
// from-ésima (las de antes no cuentan) y a r bloques de cada una.
func (s *Storage) IsProtectedSince(p world.BlockPos, from, r int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := max(from, 0); i < len(s.Protected); i++ {
		q := s.Protected[i]
		if abs(q.X-p.X) <= r && abs(q.Z-p.Z) <= r {
			return true
		}
	}
	return false
}

// ProtectedList son los sitios apuntados como protegidos (cada uno tapa protectRadius alrededor).
func (s *Storage) ProtectedList() []world.BlockPos {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]world.BlockPos(nil), s.Protected...)
}

// ClearProtected olvida las zonas protegidas: todas (near nil) o las que
// tapan near. Devuelve cuántas olvidó.
func (s *Storage) ClearProtected(near *world.BlockPos) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keep []world.BlockPos
	for _, q := range s.Protected {
		if near != nil && (abs(q.X-near.X) > protectRadius || abs(q.Z-near.Z) > protectRadius) {
			keep = append(keep, q)
		}
	}
	n := len(s.Protected) - len(keep)
	s.Protected = keep
	s.save()
	return n
}

// ProtectRadius: lo que tapa cada sitio apuntado como protegido (en x y en z).
const ProtectRadius = protectRadius

// IsProtected: p está en una zona donde el servidor no deja tocar nada.
func (s *Storage) IsProtected(p world.BlockPos) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, q := range s.Protected {
		if abs(q.X-p.X) <= protectRadius && abs(q.Z-p.Z) <= protectRadius {
			return true
		}
	}
	return false
}

func loadStorage(file string) *Storage {
	s := &Storage{file: file}
	if file == "" {
		return s
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return s
	}
	if err := json.Unmarshal(b, s); err != nil {
		log.Printf("zonas protegidas: %s no se pudo leer: %v", file, err)
	}
	return s
}

func (s *Storage) save() {
	if s.file == "" {
		return
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(s.file, b, 0o644); err != nil {
		log.Printf("almacén: %v", err)
	}
}
