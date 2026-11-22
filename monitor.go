package main

// Recursos y monitorización, para correr el bot en un teléfono (Termux):
//
//	--view 6          distancia de visión que se le pide al servidor (chunks)
//	--memlimit 150    límite blando de memoria del runtime de Go (MB; 0 = sin límite)
//	--monitor :8080   el panel web (panel.go): estado, inventario, log y órdenes
//
// /mem en la consola muestra lo mismo que la página.

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"cubit/internal/memtune"
)

var (
	viewFlag     = flag.Int("view", 6, "distancia de visión pedida al servidor, en chunks (2-32): cada chunk de más cuesta memoria; el bot busca a 48 bloques como mucho")
	memLimitFlag = flag.Int("memlimit", 150, "límite blando de memoria de Go, en MB (0 = sin límite; GOMEMLIMIT manda si está puesto)")
	monitorFlag  = flag.String("monitor", "", "dirección de la página de monitorización (ej: :8080 o 127.0.0.1:8080); vacío = sin página")
)

var started = time.Now()

// applyMemLimit pone el límite blando (al acercarse, el recolector trabaja más
// en vez de pedirle más memoria al sistema) y un GOGC más bajo que el de Go
// (internal/memtune; GOGC y GOMEMLIMIT del entorno mandan).
func applyMemLimit() { memtune.Apply(*memLimitFlag) }

// viewDistance es --view, dentro de lo que acepta el juego.
func viewDistance() int { return min(max(*viewFlag, 2), 32) }

// stats es lo que muestra el monitor.
type stats struct {
	Name      string  `json:"name"`
	Uptime    string  `json:"uptime"`
	Health    float32 `json:"health"`
	Food      int32   `json:"food"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Z         float64 `json:"z"`
	Combat    string  `json:"combat"`
	Task      string  `json:"task"`
	Chunks    int     `json:"chunks"`
	Entities  int     `json:"entities"`
	Players   int     `json:"players"`
	HeapMB    float64 `json:"heap_mb"`     // memoria en uso por los objetos de Go
	SysMB     float64 `json:"sys_mb"`      // lo que Go le pidió al sistema (≈ lo que ocupa)
	RSSMB     float64 `json:"rss_mb"`      // lo que ocupa de verdad en RAM (Linux/Android)
	LimitMB   float64 `json:"limit_mb"`    // el límite blando (0 = sin límite)
	NumGC     uint32  `json:"gc"`          // recolecciones desde el arranque
	GCPauseMS float64 `json:"gc_pause_ms"` // la última pausa del recolector
	Goroutine int     `json:"goroutines"`
}

func (b *Bot) stats() stats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	s := stats{
		Name:      botName,
		Uptime:    time.Since(started).Round(time.Second).String(),
		HeapMB:    mb(m.HeapAlloc),
		SysMB:     mb(m.Sys - m.HeapReleased),
		RSSMB:     rssMB(),
		NumGC:     m.NumGC,
		GCPauseMS: float64(m.PauseNs[(m.NumGC+255)%256]) / 1e6,
		Goroutine: runtime.NumGoroutine(),
	}
	if l := debug.SetMemoryLimit(-1); l < 1<<62 {
		s.LimitMB = mb(uint64(l))
	}
	if b.Player != nil {
		st, p := b.Player.Status(), b.Player.Position()
		s.Health, s.Food, s.X, s.Y, s.Z = float32(st.Health), int32(st.Food), p.X, p.Y, p.Z
	}
	if b.Combat != nil {
		s.Combat = b.Combat.Status()
	}
	if b.Tasks != nil {
		s.Task = b.Tasks.Status()
	}
	if b.World != nil {
		b.World.RLock()
		s.Chunks = len(b.World.Columns)
		b.World.RUnlock()
	}
	if b.Entities != nil && b.Player != nil {
		p := b.Player.Position()
		s.Entities = len(b.Entities.Nearby(p.X, p.Y, p.Z, 1e9))
	}
	if n := onlineNames.Load(); n != nil {
		s.Players = len(*n)
	}
	return s
}

func mb(n uint64) float64 { return float64(n) / (1 << 20) }

// rssMB lee la memoria residente de /proc (0 donde no hay /proc).
func rssMB() float64 {
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	var size, rss uint64
	if _, err := fmt.Sscan(string(b), &size, &rss); err != nil {
		return 0
	}
	return mb(rss * uint64(os.Getpagesize()))
}

func (s stats) String() string {
	return fmt.Sprintf("RAM %.1f MB (heap %.1f, Go %.1f, límite %.0f) · GC %d (última pausa %.2f ms) · goroutines %d · chunks %d · entidades %d · activo %s",
		s.RSSMB, s.HeapMB, s.SysMB, s.LimitMB, s.NumGC, s.GCPauseMS, s.Goroutine, s.Chunks, s.Entities, s.Uptime)
}

// startMonitor sirve la página de estado en --monitor (si se pidió).
