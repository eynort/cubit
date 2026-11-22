// Package memtune ajusta el recolector de Go para un proceso pequeño y de
// larga vida (un bot, el hub): poca memoria viva (10-30 MB), pocas
// asignaciones por segundo (≈30 KB/s quieto, ≈300 KB/s caminando) y picos al
// arrancar y al cargar el mundo. Las medidas: docs/PERFORMANCE.md.
package memtune

import (
	"context"
	"os"
	"runtime"
	"runtime/debug"
	"time"
)

// GCPercent es el GOGC por defecto (el de Go es 100: el montón crece hasta el
// doble de lo vivo antes de recoger). Con 50 crece la mitad; con tan pocas
// asignaciones el coste en CPU no se nota (una recolección cada ~20 s
// caminando, de ~1 ms).
const GCPercent = 50

// Apply pone GOGC (si no viene en el entorno) y el límite blando de memoria
// en MB (si limitMB > 0 y GOMEMLIMIT no viene en el entorno).
func Apply(limitMB int) {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(GCPercent)
	}
	if os.Getenv("GOMEMLIMIT") == "" && limitMB > 0 {
		debug.SetMemoryLimit(int64(limitMB) << 20)
	}
}

// idleSlack: la memoria del montón libre pero aún no devuelta al sistema a
// partir de la cual merece la pena devolverla de golpe.
const idleSlack = 4 << 20

// ReleaseSoon devuelve al sistema la memoria libre del montón pasado after
// (después de un pico: el arranque, cargar el mundo), si es bastante. El
// recolector la devuelve solo, pero despacio y no toda.
func ReleaseSoon(ctx context.Context, after time.Duration) {
	go func() {
		select {
		case <-ctx.Done():
		case <-time.After(after):
			ReleaseIfIdle()
		}
	}()
}

// ReleaseIfIdle devuelve la memoria libre del montón si pasa de idleSlack
// (debug.FreeOSMemory: una recolección completa y devolverlo todo; ~1-3 ms
// con el montón de un bot). Dice si lo hizo.
func ReleaseIfIdle() bool {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	if m.HeapIdle-m.HeapReleased < idleSlack {
		return false
	}
	debug.FreeOSMemory()
	return true
}
