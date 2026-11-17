package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPowerAndTarget(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MCBOT_SERVER_DIR", dir)
	t.Setenv("MCBOT_RCON", "minecraft:25575")
	t.Setenv("MCBOT_TEST_SERVER", "minecraft")
	if serverOff() || powerState(true) != "on" {
		t.Fatalf("sin marca: encendido")
	}
	if err := os.WriteFile(filepath.Join(dir, powerMarker), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !serverOff() || powerState(true) != "stopping" || powerState(false) != "off" {
		t.Fatalf("con marca: apagándose (%s) / apagado (%s)", powerState(true), powerState(false))
	}
	if err := stopServer(); err == nil {
		t.Fatal("apagar dos veces")
	}

	old := serverAddr
	defer func() { serverAddr = old }()
	for addr, kind := range map[string]string{"minecraft": "test", "minecraft:25565": "test", "MINECRAFT": "test", "minecraft:25570": "external", "play.ejemplo.net": "external", "192.168.1.20:25565": "external"} {
		serverAddr = addr
		if got := currentTarget().Kind; got != kind {
			t.Errorf("%s: %s, quería %s", addr, got, kind)
		}
	}
}
