package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestHubSiblings: los bot.json de al lado (bots/<id>/bot.json) son los
// otros bots del panel; el propio no cuenta, y un bot suelto no tiene.
func TestHubSiblings(t *testing.T) {
	root := t.TempDir()
	for id, name := range map[string]string{"a": "Uno", "b": "Dos", "c": "Tres"} {
		d := filepath.Join(root, "bots", id)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "bot.json"), []byte(`{"name":"`+name+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := botName
	botName = "Uno"
	defer func() { botName = old }()
	got := readSiblings(filepath.Join(root, "bots", "a", "bot.json"))
	if !got["dos"] || !got["tres"] || got["uno"] || len(got) != 2 {
		t.Errorf("hermanos: %v (quería dos y tres)", got)
	}
	if n := readSiblings(filepath.Join(root, "bot.json")); len(n) != 0 {
		t.Errorf("un bot suelto no tiene hermanos: %v", n)
	}
}
