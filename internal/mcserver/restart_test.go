package mcserver

import (
	"testing"
	"time"
)

func TestRestartCountdown(t *testing.T) {
	set := func(since time.Duration, expect time.Duration, p restartPhase) {
		srvRestart.Lock()
		srvRestart.since, srvRestart.expect, srvRestart.phase = time.Now().Add(-since), expect, p
		srvRestart.backAt, srvRestart.failed = time.Time{}, ""
		srvRestart.Unlock()
	}
	defer func() {
		srvRestart.Lock()
		srvRestart.since, srvRestart.backAt = time.Time{}, time.Time{}
		srvRestart.Unlock()
	}()

	set(10*time.Second, 30*time.Second, phaseStarting)
	st := currentRestart()
	if !st.Active || st.Phase != "arrancando" || st.ETA < 19.5 || st.ETA > 20.5 || st.Late || st.BackAgo != -1 {
		t.Fatalf("a mitad: %+v", st)
	}
	set(45*time.Second, 30*time.Second, phaseStarting)
	if st := currentRestart(); st.ETA != 0 || !st.Late {
		t.Fatalf("tarde: %+v", st)
	}
	srvRestart.Lock()
	srvRestart.since, srvRestart.backAt, srvRestart.took = time.Time{}, time.Now().Add(-2*time.Second), 33*time.Second
	srvRestart.Unlock()
	if st := currentRestart(); st.Active || st.BackAgo < 1.5 || st.BackAgo > 2.5 || st.Took != 33 {
		t.Fatalf("de vuelta: %+v", st)
	}
	if d := lastRestartTook(); d < 5*time.Second {
		t.Fatalf("estimación %v", d)
	}
}
