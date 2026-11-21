package main

// Los otros bots del mismo panel (el hub): cada uno vive en bots/<id>/bot.json,
// así que los hermanos son los bot.json de al lado. Son de los nuestros: ni se
// les planta cara ni se pelea con ellos (en guardia unos contra otros, se
// quedaban parados: la excavación esperaba a una pelea que no llegaba).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var siblings struct {
	sync.Mutex
	names map[string]bool
	at    time.Time
}

// hubSibling: name es otro bot del mismo panel (se relee cada 30 s: los que se crean o renombran).
func hubSibling(name string) bool {
	if name == "" {
		return false
	}
	siblings.Lock()
	defer siblings.Unlock()
	if time.Since(siblings.at) > 30*time.Second {
		siblings.names, siblings.at = readSiblings(cfgPath), time.Now()
	}
	return siblings.names[strings.ToLower(name)]
}

func readSiblings(cfg string) map[string]bool {
	out := map[string]bool{}
	abs, err := filepath.Abs(cfg)
	if err != nil {
		return out
	}
	dir := filepath.Dir(abs) // bots/<id>
	if filepath.Base(filepath.Dir(dir)) != "bots" {
		return out // (un bot suelto: no hay hermanos)
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(dir), "*", "bot.json"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var c struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(b, &c) == nil && c.Name != "" && !strings.EqualFold(c.Name, botName) {
			out[strings.ToLower(c.Name)] = true
		}
	}
	return out
}
