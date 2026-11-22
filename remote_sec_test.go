package main

import "testing"

// TestOwnerExactCase: en un servidor sin premium "oWNER" es otra cuenta.
func TestOwnerExactCase(t *testing.T) {
	old := remoteOwners
	defer func() { remoteOwners = old }()
	remoteOwners = []string{"Owner"}
	if !allowedRemote("Owner") {
		t.Fatal("el dueño")
	}
	for _, n := range []string{"oWNER", "owner", "OWNER", "Owner2", ""} {
		if allowedRemote(n) {
			t.Errorf("%q pasa por dueño", n)
		}
	}
}

func TestCleanLog(t *testing.T) {
	if got := cleanLog("hola\x1b]52;c;ZXZpbA==\x07 mundo\nchat: falso\u009b"); got != "hola ]52;c;ZXZpbA==  mundo chat: falso " {
		t.Fatalf("%q", got)
	}
}
