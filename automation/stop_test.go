package automation_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mj41/go-mc26-kit/bot/simworld"
	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/automation"
)

func TestParseUntil(t *testing.T) {
	ok := map[string]string{
		"got logs 64":                  "got logs 64",
		"have oak_log 10 or time 20m":  "have oak_log 10|time 20m0s",
		"count trees 3 OR got *_log 5": "count trees 3|got *_log 5",
		"time 1h30m":                   "time 1h30m0s",
		"got minecraft:Wheat 32":       "got minecraft:wheat 32",
		"got wool 8 or have beds 3":    "got wool 8|have beds 3",
	}
	for in, want := range ok {
		conds, err := automation.ParseUntil(strings.Fields(in))
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		var got []string
		for _, c := range conds {
			got = append(got, c.String())
		}
		if strings.Join(got, "|") != want {
			t.Errorf("%q: %q, quería %q", in, strings.Join(got, "|"), want)
		}
	}
	for _, in := range []string{"", "got logs", "got logs 0", "got logs x", "time 5", "time -1m", "logs 64",
		"got logs 64 time 2m", "got logs 64 or", "got [ 3"} {
		if _, err := automation.ParseUntil(strings.Fields(in)); err == nil {
			t.Errorf("%q: se aceptó", in)
		}
	}
	cmd, until := automation.SplitUntil(strings.Fields("abeto 32 until got logs 5"))
	if strings.Join(cmd, " ") != "abeto 32" || strings.Join(until, " ") != "got logs 5" {
		t.Errorf("SplitUntil: %q %q", cmd, until)
	}
	if _, until := automation.SplitUntil(strings.Fields("abeto 32")); until != nil {
		t.Errorf("SplitUntil sin until: %q", until)
	}
}

// TestUntilStopsAnyTask: una tarea que no sabe nada de condiciones (no mira
// StopReached) se para en un Wait cuando se cumple, y acaba bien (sin error:
// la cola sigue). count lee lo que la tarea publica con Tally.
func TestUntilStopsAnyTask(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		g := simworld.Start(t, simworld.Options{})
		r := runner(t, g)
		var ended error = context.Canceled
		ticks := 0
		task := func(j *automation.Job) error {
			for {
				ticks++
				j.Tally("vueltas", 1)
				if err := j.Wait(1); err != nil {
					return err
				}
			}
		}
		conds, err := automation.ParseUntil(strings.Fields("count vueltas 40 or time 1h"))
		if err != nil {
			t.Fatal(err)
		}
		done := false
		r.Start(context.Background(), "prueba", automation.Until(func(j *automation.Job) error {
			err := task(j)
			ended, done = err, true
			return err
		}, conds))
		g.Step(5)
		if s := r.Status(); !strings.Contains(s, "[hasta: vueltas") {
			t.Errorf("el estado no dice cómo va: %q", s)
		}
		g.Until(60*20, func() bool { return r.Status() == "" })
		if !done || r.Status() != "" {
			t.Fatalf("no se paró (estado %q, %d vueltas)", r.Status(), ticks)
		}
		if ticks < 40 || ticks > 80 {
			t.Errorf("se paró tras %d vueltas (quería 40, con poco de margen)", ticks)
		}
		if ended == nil || !strings.Contains(ended.Error(), "condición de parada") {
			t.Errorf("la tarea no se paró por la condición: %v", ended)
		}
	})
}

// TestChopUntilGotLogs: /chop sin cantidad (sin parar) hasta recoger 5
// troncos: tala el primer árbol entero (no lo deja a medias: la tala mira la
// condición entre árbol y árbol) y no empieza el segundo.
func TestChopUntilGotLogs(t *testing.T) {
	simworld.Run(t, func(t *testing.T) {
		setup := append(tree(6, 0, 5), tree(-6, 4, 5)...)
		setup = append(setup, "give Tester dirt 16", "give Tester iron_axe 1")
		g := simworld.Start(t, simworld.Options{Setup: setup})
		r := runner(t, g)
		g.Step(5)
		conds, err := automation.ParseUntil(strings.Fields("got logs 5"))
		if err != nil {
			t.Fatal(err)
		}
		r.Start(context.Background(), "talar", automation.Until(automation.Woodcut("oak", 0), conds))
		g.Until(300*20, func() bool { return r.Status() == "" })
		if s := r.Status(); s != "" {
			t.Fatalf("no acabó: %q", s)
		}
		standing := map[[2]int]int{}
		for y := 64; y <= 68; y++ {
			for _, c := range [][2]int{{6, 0}, {-6, 4}} {
				if st, ok := g.B.World.BlockAt(world.BlockPos{X: c[0], Y: y, Z: c[1]}); ok && strings.Contains(world.StateString(st), "_log") {
					standing[c]++
				}
			}
		}
		t.Logf("en pie: %v", standing)
		felled := 0
		for _, c := range [][2]int{{6, 0}, {-6, 4}} {
			switch standing[c] {
			case 0:
				felled++
			case 5:
			default:
				t.Errorf("árbol de %v a medias: quedan %d troncos", c, standing[c])
			}
		}
		if felled != 1 {
			t.Errorf("taló %d árboles; con 5 troncos basta uno", felled)
		}
	})
}

// TestChopMovesEmptyFarBase: una base lejos y sin cofres (una que no se llegó
// a montar) se pone donde tala; una lejos con cofres se respeta (guarda cosas).
func TestChopMovesEmptyFarBase(t *testing.T) {
	for _, withChest := range []bool{false, true} {
		simworld.Run(t, func(t *testing.T) {
			setup := append(tree(6, 0, 5), "give Tester dirt 16", "give Tester iron_axe 1")
			g := simworld.Start(t, simworld.Options{Setup: setup})
			r := runner(t, g)
			far := world.BlockPos{X: 1000, Y: 70, Z: 64}
			r.Store.SetBase(far)
			if withChest {
				r.Store.Chests = []automation.Chest{{Pos: world.BlockPos{X: 1001, Y: 70, Z: 64}, Item: "madera"}}
			}
			g.Step(5)
			r.Start(context.Background(), "talar", automation.Woodcut("oak", 1))
			g.Until(120*20, func() bool { return r.Status() == "" })
			b := r.Store.Base
			moved := b != nil && *b != far
			t.Logf("con cofre %v: base %v", withChest, b)
			if moved == withChest {
				t.Errorf("con cofre %v: base %v (movida %v)", withChest, b, moved)
			}
		})
	}
}
