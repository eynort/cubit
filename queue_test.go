package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSplitChain(t *testing.T) {
	cases := map[string][]string{
		"/mine iron ~ ~ ~ ; /chop oak 10 64 10": {"/mine iron ~ ~ ~", "/chop oak 10 64 10"},
		"/status":                               {"/status"},
		"/say hola ; adiós":                     {"/say hola ; adiós"},
		"hola ; adiós":                          {"hola ; adiós"},
		"/chop a;b ~ ~ ~":                       {"/chop a;b ~ ~ ~"},
		"/status ; /chop oak ~ ~ ~ ; /stop":     {"/status", "/chop oak ~ ~ ~", "/stop"},
	}
	for in, want := range cases {
		if got := splitChain(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitChain(%q) = %q, quiero %q", in, got, want)
		}
	}
}

func TestKindOf(t *testing.T) {
	for line, want := range map[string]cmdKind{
		"/chop oak ~ ~ ~": kindTask, "/mine iron ~ ~ ~": kindTask, "/excavate 1 2 3 4 5 6": kindTask,
		"/excavate chests list": kindInstant, "/goto 1 2 3": kindTask, "/goto Steve": kindOpenEnd,
		"/follow Steve": kindOpenEnd, "/status": kindInstant, "hola": kindInstant,
	} {
		if got := kindOf(line); got != want {
			t.Errorf("kindOf(%q) = %v, quiero %v", line, got, want)
		}
	}
}

// testBot: un Bot sin conexión y una orden "zz" que apunta lo que se le manda.
func testBot(t *testing.T) (*Bot, *[]string) {
	t.Helper()
	workQueue.reset()
	ran := &[]string{}
	commands["zz"] = command{run: func(_ *Bot, args []string) error {
		if len(args) > 0 && args[0] == "bad" {
			return errors.New("malo")
		}
		*ran = append(*ran, strings.Join(args, " "))
		return nil
	}}
	t.Cleanup(func() { delete(commands, "zz"); workQueue.reset() })
	return &Bot{}, ran
}

func TestQueueAdvancesAndFinishes(t *testing.T) {
	b, ran := testBot(t)
	_ = workQueue.add("/zz one", "")
	_ = workQueue.add("/zz two", "")
	b.onTaskEnd("talar", nil)
	b.onTaskEnd("talar", nil)
	if !reflect.DeepEqual(*ran, []string{"one", "two"}) {
		t.Fatalf("ran = %q", *ran)
	}
	if lines, _ := workQueue.snapshot(); len(lines) != 0 {
		t.Fatalf("la cola debía quedar vacía: %q", lines)
	}
}

func TestQueueFailurePauses(t *testing.T) {
	b, ran := testBot(t)
	_ = workQueue.add("/zz one", "")
	b.onTaskEnd("talar", errors.New("sin hacha"))
	lines, paused := workQueue.snapshot()
	if !paused || len(lines) != 1 || len(*ran) != 0 {
		t.Fatalf("paused=%v lines=%q ran=%q", paused, lines, *ran)
	}
	b.onTaskEnd("talar", nil) // acaba bien con la cola en pausa: no la sigue
	if len(*ran) != 0 {
		t.Fatal("no debía seguir en pausa")
	}
	workQueue.setPaused(false)
	b.onTaskEnd("talar", nil)
	if !reflect.DeepEqual(*ran, []string{"one"}) {
		t.Fatalf("ran = %q", *ran)
	}
}

func TestQueueIgnoresPickupTask(t *testing.T) {
	b, ran := testBot(t)
	_ = workQueue.add("/zz one", "")
	b.onTaskEnd("recoger", nil) // un recado, no el trabajo
	if len(*ran) != 0 {
		t.Fatal("recoger no debe avanzar la cola")
	}
}

func TestQueueStartFailurePauses(t *testing.T) {
	b, _ := testBot(t)
	_ = workQueue.add("/zz bad", "")
	_ = workQueue.add("/zz two", "")
	b.onTaskEnd("talar", nil)
	if lines, paused := workQueue.snapshot(); !paused || len(lines) != 1 {
		t.Fatalf("paused=%v lines=%q", paused, lines)
	}
}

func TestQueueCommands(t *testing.T) {
	b, _ := testBot(t)
	for _, l := range []string{"/chop oak ~ ~ ~", "/mine iron 1 2 3", "/goto 1 2 3"} {
		if err := cmdQueue(b, strings.Fields(l)); err != nil {
			t.Fatal(err)
		}
	}
	if err := cmdQueue(b, []string{"/follow", "Steve"}); err == nil {
		t.Fatal("follow no se encola")
	}
	if err := cmdQueue(b, []string{"remove", "2"}); err != nil {
		t.Fatal(err)
	}
	if err := cmdQueue(b, []string{"remove", "9"}); err == nil {
		t.Fatal("debía fallar")
	}
	lines, _ := workQueue.snapshot()
	if !reflect.DeepEqual(lines, []string{"/chop oak ~ ~ ~", "/goto 1 2 3"}) {
		t.Fatalf("lines = %q", lines)
	}
	if !strings.Contains(queueSummary(), "/chop") {
		t.Fatal("queueSummary")
	}
	_ = cmdQueue(b, []string{"clear"})
	if lines, _ := workQueue.snapshot(); len(lines) != 0 {
		t.Fatal("clear")
	}
}

func TestQueueSuggestions(t *testing.T) {
	has := func(line string, f func(suggestion) bool) bool {
		for _, s := range suggestions(line) {
			if f(s) {
				return true
			}
		}
		return false
	}
	if !has("/queue /ch", func(s suggestion) bool { return s.value == "/queue /chop " }) {
		t.Errorf("/queue /ch: falta /queue /chop: %+v", suggestions("/queue /ch"))
	}
	if !has("/queue ", func(s suggestion) bool { return s.label == "resume" }) {
		t.Error("/queue : falta resume")
	}
}

func TestChainInstantsRunInOrder(t *testing.T) {
	b, ran := testBot(t)
	if err := b.run("/zz a ; /zz b ; /zz c"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*ran, []string{"a", "b", "c"}) {
		t.Fatalf("ran = %q", *ran)
	}
	if lines, _ := workQueue.snapshot(); len(lines) != 0 {
		t.Fatalf("lines = %q", lines)
	}
}

// Una cadena que empieza por /goto x y z: al llegar, sigue la cola (probado
// contra un servidor: se quedaba parada tras «goto: llegué»).
func TestChainStartingWithGotoAdvances(t *testing.T) {
	b, ran := testBot(t)
	old := commands["goto"]
	commands["goto"] = command{run: func(_ *Bot, args []string) error {
		*ran = append(*ran, "goto "+strings.Join(args, " "))
		return nil
	}}
	t.Cleanup(func() { commands["goto"] = old })
	if err := b.run("/goto 1 2 3 ; /goto 4 5 6"); err != nil {
		t.Fatal(err)
	}
	if lines, _ := workQueue.snapshot(); len(lines) != 1 {
		t.Fatalf("lines = %q", lines)
	}
	b.moveEnded(true) // llegó
	if !reflect.DeepEqual(*ran, []string{"goto 1 2 3", "goto 4 5 6"}) {
		t.Fatalf("ran = %q", *ran)
	}
}
