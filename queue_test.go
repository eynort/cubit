package main

import (
	"strings"
	"testing"
	"time"
)

// fakeBot: lo que la cola ve del bot. Cada orden arrancada queda "en marcha"
// hasta que el test la termina.
type fakeBot struct {
	started []string
	running string
	direct  bool // algo directo en marcha
	clock   time.Time
}

func newTestQueue(f *fakeBot) *Queue {
	f.clock = time.Unix(0, 0)
	return &Queue{
		now:   func() time.Time { return f.clock },
		start: func(line string) error { f.started = append(f.started, line); f.running = line; return nil },
		busy:  func(kind string) bool { return f.running != "" && strings.HasPrefix(f.running, kind) },
		idle:  func() bool { return f.running == "" && !f.direct },
	}
}

// tick avanza el reloj más que la gracia y da una vuelta al ejecutor.
func (f *fakeBot) tick(q *Queue) {
	f.clock = f.clock.Add(queueGrace + time.Second)
	q.step()
}

func TestQueueEdit(t *testing.T) {
	q := newTestQueue(&fakeBot{})
	q.SetPaused(true)
	for _, l := range []string{"chop oak 20", "/goto 100 200", "mine debris 2"} {
		if _, err := q.Add(l, 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := q.Add("say hola", 0); err == nil {
		t.Errorf("/say no debería ir en la cola")
	}
	if _, err := q.Add("guard Alex 5", 1); err != nil { // la primera
		t.Fatal(err)
	}
	want := func(s string) {
		t.Helper()
		if got := strings.Join(q.Pending(), "|"); got != s {
			t.Fatalf("cola %q, quería %q", got, s)
		}
	}
	want("guard Alex 5|chop oak 20|goto 100 200|mine debris 2")
	if err := q.Move(4, 1); err != nil {
		t.Fatal(err)
	}
	want("mine debris 2|guard Alex 5|chop oak 20|goto 100 200")
	if err := q.Move(1, 3); err != nil {
		t.Fatal(err)
	}
	want("guard Alex 5|chop oak 20|mine debris 2|goto 100 200")
	if _, err := q.Remove(2); err != nil {
		t.Fatal(err)
	}
	want("guard Alex 5|mine debris 2|goto 100 200")
	if _, err := q.Remove(9); err == nil {
		t.Errorf("quitar una posición que no existe")
	}
	if err := q.Move(0, 2); err == nil {
		t.Errorf("mover desde la posición 0")
	}
}

func TestQueueRuns(t *testing.T) {
	f := &fakeBot{}
	q := newTestQueue(f)
	q.Add("goto 1 1", 0)
	q.Add("chop oak 5", 0)
	q.Add("mine debris 1", 0)

	f.tick(q)
	if q.Current() != "goto 1 1" {
		t.Fatalf("primera: %q", q.Current())
	}
	f.tick(q) // sigue en marcha: nada cambia
	if len(f.started) != 1 {
		t.Fatalf("arrancó de más: %v", f.started)
	}
	f.running = "" // llegó
	f.tick(q)
	if q.Current() != "chop oak 5" {
		t.Fatalf("segunda: %q", q.Current())
	}

	// una orden directa: la de la cola vuelve a ser la primera y la cola espera
	q.Interrupt()
	f.running, f.direct = "", true
	f.tick(q)
	if q.Current() != "" || strings.Join(q.Pending(), "|") != "chop oak 5|mine debris 1" {
		t.Fatalf("tras la directa: actual %q, cola %v", q.Current(), q.Pending())
	}
	f.direct = false // la directa terminó: se retoma
	f.tick(q)
	if q.Current() != "chop oak 5" {
		t.Fatalf("retomada: %q", q.Current())
	}

	// /stop: la actual se descarta y la cola se pausa
	q.Stop()
	f.running = ""
	f.tick(q)
	if q.Current() != "" {
		t.Fatalf("tras /stop siguió: %q", q.Current())
	}
	q.SetPaused(false)
	f.tick(q)
	if q.Current() != "mine debris 1" {
		t.Fatalf("tras /cola seguir: %q", q.Current())
	}
	// saltar: la siguiente (ninguna) — la cola queda vacía
	if s := q.Skip(); s != "mine debris 1" {
		t.Fatalf("saltar: %q", s)
	}
	f.running = ""
	f.tick(q)
	if q.Current() != "" || len(q.Pending()) != 0 || q.Short() != "" {
		t.Fatalf("al final: %q %v %q", q.Current(), q.Pending(), q.Short())
	}
	want := "goto 1 1|chop oak 5|chop oak 5|mine debris 1"
	if got := strings.Join(f.started, "|"); got != want {
		t.Errorf("arrancadas %q, quería %q", got, want)
	}
}

// Una orden que no arranca (un jugador desconectado) se quita y se sigue.
func TestQueueStartError(t *testing.T) {
	f := &fakeBot{}
	q := newTestQueue(f)
	q.start = func(line string) error {
		if strings.HasPrefix(line, "goto") {
			return errFake
		}
		f.started = append(f.started, line)
		f.running = line
		return nil
	}
	q.Add("goto Nadie", 0)
	q.Add("chop 3", 0)
	f.tick(q)
	f.tick(q)
	if q.Current() != "chop 3" {
		t.Fatalf("tras el error: %q", q.Current())
	}
}

var errFake = &fakeErr{}

type fakeErr struct{}

func (*fakeErr) Error() string { return "no conectado" }

func TestQueueWarnings(t *testing.T) {
	for line, warn := range map[string]bool{
		"chop oak": true, "chop oak 10": false, "guard Alex": true, "guard Alex 5": false,
		"fight in 0 0": true, "fight Steve": false, "goto 1 2": false, "farm": true,
	} {
		if got := queueWarning(line) != ""; got != warn {
			t.Errorf("%q: aviso %v, quería %v", line, got, warn)
		}
	}
}

func TestQueueSuggestions(t *testing.T) {
	got := labels(suggestions("/queue "))
	if len(got) < 5 || got[0] != "list" {
		t.Errorf(`"/queue ": %v`, got)
	}
}

// TestQueueUntil: una orden con "until" va a la cola tal cual (se aplica al
// ejecutarla); un until mal escrito se rechaza ya al añadirla.
func TestQueueUntil(t *testing.T) {
	q := newTestQueue(&fakeBot{})
	if _, err := q.Add("chop abeto until got logs 64 or time 20m", 0); err != nil {
		t.Errorf("until válido rechazado: %v", err)
	}
	if got := q.Pending(); len(got) != 1 || !strings.Contains(got[0], "until got logs 64 or time 20m") {
		t.Errorf("la cola no guardó el until: %q", got)
	}
	for _, bad := range []string{"chop abeto until", "chop until got logs", "chop until logs 64", "chop until time 5"} {
		if _, err := q.Add(bad, 0); err == nil {
			t.Errorf("%q: se aceptó", bad)
		}
	}
}

// TestSplitConds: el until se separa de los argumentos de una orden, salvo
// en las de texto libre (say, cmd, queue, ai), que lo llevan en su texto.
func TestSplitConds(t *testing.T) {
	args, conds, err := splitConds(commands["chop"], strings.Fields("abeto until got logs 5"))
	if err != nil || strings.Join(args, " ") != "abeto" || len(conds) != 1 {
		t.Errorf("chop: %q %v %v", args, conds, err)
	}
	if _, _, err := splitConds(commands["chop"], strings.Fields("abeto until nada")); err == nil {
		t.Errorf("chop con until malo: se aceptó")
	}
	for _, name := range []string{"say", "cmd", "queue", "ai"} {
		args, conds, err := splitConds(commands[name], strings.Fields("espera until got logs 5"))
		if err != nil || conds != nil || len(args) != 5 {
			t.Errorf("%s: %q %v %v", name, args, conds, err)
		}
	}
}
