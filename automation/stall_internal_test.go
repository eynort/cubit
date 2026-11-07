package automation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// TestStallRestart: una tarea bloqueada sin dar señales de vida se detecta:
// deja su pila en la traza (con la función donde estaba) y se vuelve a
// empezar; la que sí da señales (Status) no se toca.
func TestStallRestart(t *testing.T) {
	stallAfter, watchEvery = 300*time.Millisecond, 20*time.Millisecond
	defer func() { stallAfter, watchEvery = 2*time.Minute, 5*time.Second }()
	var mu sync.Mutex
	var traces []string
	r := New(Deps{Trace: func(format string, args ...any) {
		mu.Lock()
		traces = append(traces, fmt.Sprintf(format, args...))
		mu.Unlock()
	}})
	var starts atomic.Int32
	block := make(chan struct{})
	defer close(block)
	r.Start(context.Background(), "atasco", func(j *Job) error {
		if starts.Add(1) == 1 {
			blockedForever(block)
		}
		return nil
	})
	deadline := time.Now().Add(3 * time.Second)
	for starts.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if starts.Load() < 2 {
		t.Fatalf("no se reinició")
	}
	mu.Lock()
	defer mu.Unlock()
	stack, ficha := false, false
	for _, tr := range traces {
		stack = stack || strings.Contains(tr, "TAREA ATASCADA") && strings.Contains(tr, "blockedForever")
		ficha = ficha || strings.HasPrefix(tr, "PROBLEMA #") && strings.Contains(tr, "atascada")
	}
	if !stack || !ficha {
		t.Fatalf("la traza no dice dónde estaba (pilas %v, ficha %v): %v", stack, ficha, traces)
	}

	// una tarea que trabaja (da señales) no es un atasco
	traces = traces[:0]
	mu.Unlock()
	var ran atomic.Int32
	r.Start(context.Background(), "trabajo", func(j *Job) error {
		for i := 0; i < 40; i++ {
			j.Status("paso %d", i)
			time.Sleep(20 * time.Millisecond)
		}
		ran.Add(1)
		return nil
	})
	time.Sleep(1200 * time.Millisecond)
	mu.Lock()
	if ran.Load() != 1 || len(traces) != 0 {
		t.Fatalf("la tarea que trabaja: terminó %d veces, trazas %d", ran.Load(), len(traces))
	}
}

func blockedForever(c chan struct{}) { <-c }

// TestProgressWatch: ir y venir entre dos bloques (con señales de vida) no es
// avanzar; alejarse, cambiar el inventario o una pausa (pelea) sí cuentan.
func TestProgressWatch(t *testing.T) {
	var w progressWatch
	t0 := time.Unix(1000, 0)
	a, b := world.BlockPos{X: 210, Y: -19, Z: -477}, world.BlockPos{X: 211, Y: -19, Z: -477}
	var still time.Duration
	for i := 0; i <= 36; i++ { // 3 minutos, cada 5 s, de 210 a 211 y vuelta
		p := a
		if i%2 == 1 {
			p = b
		}
		still = w.update(t0.Add(time.Duration(i)*5*time.Second), p, 289, true)
	}
	if still < 3*time.Minute {
		t.Fatalf("yendo y viniendo 3 minutos: sin avanzar %v", still)
	}
	// alejarse más de stuckMove: vuelve a cero
	if s := w.update(t0.Add(185*time.Second), world.BlockPos{X: 216, Y: -19, Z: -477}, 289, true); s != 0 {
		t.Errorf("alejándose: %v", s)
	}
	// minar sin moverse (el inventario cambia): avanza
	w.update(t0.Add(190*time.Second), world.BlockPos{X: 216, Y: -19, Z: -477}, 289, true)
	if s := w.update(t0.Add(250*time.Second), world.BlockPos{X: 216, Y: -19, Z: -477}, 290, true); s != 0 {
		t.Errorf("cogiendo algo: %v", s)
	}
	// quieto a propósito (Rest) o sin saber: no cuenta
	if s := w.update(t0.Add(500*time.Second), world.BlockPos{X: 216, Y: -19, Z: -477}, 290, false); s != 0 {
		t.Errorf("descansando: %v", s)
	}
}
