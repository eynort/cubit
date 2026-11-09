package automation

import (
	"encoding/json"
	"log"
	"os"
	"sync"

	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/schematic"
)

// BuildState es la construcción apuntada (/build): qué schematic, dónde lo
// colocó el jugador que lo ve en Litematica, los cofres con los materiales y
// lo aprendido al poner bloques (cómo sale la orientación de cada uno). Se
// guarda en un archivo para seguir tras reiniciar.
type BuildState struct {
	mu   sync.Mutex
	file string

	Schematic string           `json:"schematic,omitempty"` // la ruta del archivo
	Origin    *[3]int          `json:"origin,omitempty"`
	Rotation  int              `json:"rotation"`
	Mirror    int              `json:"mirror"`
	Dim       string           `json:"dim,omitempty"`
	Chests    []world.BlockPos `json:"chests,omitempty"`
	NoClear   bool             `json:"no_clear,omitempty"`
	// Rules: por bloque (y su "face"), las reglas de orientación que siguen
	// valiendo (las que contradijo un bloque puesto se quitan)
	Rules map[string][]facingRule `json:"rules,omitempty"`
	// RotOff: por bloque, rotación = segmento del yaw + RotOff (carteles, estandartes, cabezas)
	RotOff map[string]int `json:"rot_off,omitempty"`
	// Loose: propiedades que no hay manera de dejar bien en este servidor (no se comparan)
	Loose map[string]bool `json:"loose,omitempty"`
}

func loadBuildState(file string) *BuildState {
	s := &BuildState{file: file}
	if file == "" {
		return s
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return s
	}
	if err := json.Unmarshal(data, s); err != nil {
		log.Printf("construcción: no leo %s: %v", file, err)
	}
	s.file = file
	return s
}

func (s *BuildState) save() {
	if s.file == "" {
		return
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(s.file, data, 0o644); err != nil {
		log.Printf("construcción: no guardo %s: %v", s.file, err)
	}
}

// Update cambia el estado y lo guarda.
func (s *BuildState) Update(f func(s *BuildState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s)
	s.save()
}

// View lee el estado (con el cerrojo puesto).
func (s *BuildState) View(f func(s *BuildState)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s)
}

// Placement es la colocación apuntada (ok false: falta el origen).
func (s *BuildState) Placement() (schematic.Placement, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Origin == nil {
		return schematic.Placement{}, false
	}
	o := *s.Origin
	return schematic.Placement{Origin: schematic.Pos{X: o[0], Y: o[1], Z: o[2]},
		Rotation: schematic.Rotation(s.Rotation & 3), Mirror: schematic.Mirror(s.Mirror)}, true
}

func (s *BuildState) rules(key string, def []facingRule) []facingRule {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.Rules[key]; ok && len(r) > 0 {
		return append([]facingRule(nil), r...)
	}
	return def
}

func (s *BuildState) setRules(key string, r []facingRule) {
	s.Update(func(s *BuildState) {
		if s.Rules == nil {
			s.Rules = map[string][]facingRule{}
		}
		s.Rules[key] = r
	})
}

func (s *BuildState) rotOff(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.RotOff[key]; ok {
		return v
	}
	return 8 // los carteles miran al jugador: el yaw más media vuelta
}

func (s *BuildState) setRotOff(key string, v int) {
	s.Update(func(s *BuildState) {
		if s.RotOff == nil {
			s.RotOff = map[string]int{}
		}
		s.RotOff[key] = v & 15
	})
}

func (s *BuildState) loose(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Loose[key]
}

func (s *BuildState) setLoose(key string) {
	s.Update(func(s *BuildState) {
		if s.Loose == nil {
			s.Loose = map[string]bool{}
		}
		s.Loose[key] = true
	})
}
