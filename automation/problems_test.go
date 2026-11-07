package automation_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"

	"minecraft-bot/automation"
)

// TestProblemSnapshot: un problema deja su ficha en la traza (dónde, qué
// lleva, los bloques de alrededor con su leyenda, el agua o lava al lado) y
// una línea en el índice; el mismo problema repetido, solo 5 fichas.
func TestProblemSnapshot(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{Setup: []string{
			"give Tester diamond_pickaxe 1", "fill 1 63 0 1 63 0 minecraft:lava",
		}})
		r := runner(t, g)
		var mu sync.Mutex
		var traces, index []string
		r.SetTrace(func(format string, args ...any) {
			mu.Lock()
			traces = append(traces, fmt.Sprintf(format, args...))
			mu.Unlock()
		})
		r.SetProblemIndex(func(n int, task, msg string) {
			mu.Lock()
			index = append(index, fmt.Sprintf("#%d [%s] %s", n, task, msg))
			mu.Unlock()
		})
		g.Step(5)
		summary := ""
		r.Start(context.Background(), "prueba", func(j *automation.Job) error {
			j.Status("probando")
			for i := 0; i < 7; i++ {
				j.Problem("no llego a %d 60 0: no hay camino", 10+i) // (la misma clase: cambian los números)
			}
			j.Problem("otra cosa: lava al lado")
			summary = j.ProblemSummary()
			return nil
		})
		g.Until(100, func() bool { return r.Status() == "" })
		mu.Lock()
		defer mu.Unlock()
		var fichas []string
		for _, tr := range traces {
			if strings.HasPrefix(tr, "PROBLEMA #") {
				fichas = append(fichas, tr)
			}
		}
		if len(fichas) != 6 || len(index) != 6 {
			t.Fatalf("fichas %d e índice %d; quería 6 (5 de la clase repetida y 1 de la otra)", len(fichas), len(index))
		}
		t.Log("\n" + fichas[0])
		for _, want := range []string{"[prueba] no llego a 10 60 0", `estado: "probando"`, "dónde:", "diamond_pickaxe", "y=", "(los pies)", "@", "leyenda:", "lava en 1 63 0"} {
			if !strings.Contains(fichas[0], want) {
				t.Errorf("la ficha no tiene %q", want)
			}
		}
		if !strings.Contains(summary, "6 problemas") {
			t.Errorf("resumen: %q", summary)
		}
	})
}
