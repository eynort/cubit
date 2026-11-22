package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateAddsSchema(t *testing.T) {
	out, from, changed, err := MigrateBot([]byte(`{"name":"Alex","owners":["a"]}`))
	if err != nil || !changed || from != 1 {
		t.Fatalf("%v %v %d", err, changed, from)
	}
	var m map[string]any
	json.Unmarshal(out, &m)
	if m["schema"] != float64(BotSchema) || m["name"] != "Alex" {
		t.Errorf("%s", out)
	}
	if _, _, changed, _ := MigrateBot(out); changed {
		t.Error("an up-to-date file must not change")
	}
}

func TestMigrateRunsStepsInOrder(t *testing.T) {
	var order []string
	migs := []Migration{
		func(m map[string]any) error { order = append(order, "1→2"); m["a"] = true; return nil },
		func(m map[string]any) error { order = append(order, "2→3"); m["b"] = m["a"]; return nil },
	}
	out, from, changed, err := Migrate([]byte(`{"schema":1}`), 3, migs)
	if err != nil || !changed || from != 1 || strings.Join(order, ",") != "1→2,2→3" {
		t.Fatalf("%v %v %d %v", err, changed, from, order)
	}
	var m map[string]any
	json.Unmarshal(out, &m)
	if m["schema"] != float64(3) || m["b"] != true {
		t.Errorf("%s", out)
	}
	order = nil
	if _, _, _, err := Migrate([]byte(`{"schema":2}`), 3, migs); err != nil || strings.Join(order, ",") != "2→3" {
		t.Errorf("starting at 2: %v %v", err, order)
	}
}

func TestMigrateRefusesNewerAndBroken(t *testing.T) {
	raw := []byte(`{"schema":99}`)
	out, _, changed, err := MigrateHub(raw)
	if err == nil || changed || string(out) != string(raw) {
		t.Error("a newer schema must be refused untouched")
	}
	if _, _, _, err := MigrateHub([]byte(`not json`)); err == nil {
		t.Error("broken JSON")
	}
	bad := []Migration{func(m map[string]any) error { return os.ErrInvalid }}
	if _, _, _, err := Migrate([]byte(`{"schema":1}`), 2, bad); err == nil {
		t.Error("a failing migration must report")
	}
}

func TestMigrateFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bot.json")
	if err := MigrateFile(p, MigrateBot); err != nil {
		t.Fatal("a missing file is not an error:", err)
	}
	os.WriteFile(p, []byte(`{"name":"Alex"}`), 0o600)
	if err := MigrateFile(p, MigrateBot); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), `"schema": 1`) {
		t.Errorf("%s", b)
	}
	if orig, _ := os.ReadFile(p + ".v1.bak"); string(orig) != `{"name":"Alex"}` {
		t.Error("the original must be kept as .bak")
	}
	os.WriteFile(p, []byte(`{"schema":50}`), 0o600)
	if MigrateFile(p, MigrateBot) == nil {
		t.Error("newer file must error")
	}
	if b, _ := os.ReadFile(p); string(b) != `{"schema":50}` {
		t.Error("newer file must be untouched")
	}
}
