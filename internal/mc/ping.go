package mc

// Server List Ping: qué versión de Minecraft dice ser un servidor (para
// decidir si el bot entra directo o por ViaProxy).

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"time"

	"github.com/mj41/go-mc26-kit/bot"
)

// Status: lo que se lee de la respuesta de estado de un servidor.
type Status struct {
	Protocol int    // número de protocolo (el de la versión del servidor)
	Name     string // nombre que anuncia ("1.21.4", "Paper 1.21.4"...)
	Version  string // la versión limpia ("1.21.4"), sacada de Name
	Online   int
	Max      int
}

var verRe = regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)

// CleanVersion saca la versión de Minecraft de un nombre anunciado
// ("Paper 1.21.4" → "1.21.4"; "BungeeCord 1.8.x-1.21.x" → "1.8"). Vacío si no hay.
func CleanVersion(name string) string { return verRe.FindString(name) }

// ParseStatus lee el JSON de estado de un servidor.
func ParseStatus(data []byte) (Status, error) {
	var raw struct {
		Version struct {
			Name     string `json:"name"`
			Protocol int    `json:"protocol"`
		} `json:"version"`
		Players struct {
			Online int `json:"online"`
			Max    int `json:"max"`
		} `json:"players"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Status{}, fmt.Errorf("respuesta de estado ilegible: %w", err)
	}
	if raw.Version.Protocol == 0 && raw.Version.Name == "" {
		return Status{}, fmt.Errorf("la respuesta de estado no dice la versión")
	}
	return Status{Protocol: raw.Version.Protocol, Name: raw.Version.Name, Version: CleanVersion(raw.Version.Name),
		Online: raw.Players.Online, Max: raw.Players.Max}, nil
}

// Ping pregunta el estado a addr (host o host:puerto; con registro SRV si no hay puerto).
func Ping(ctx context.Context, addr string, timeout time.Duration) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	data, _, err := bot.PingAndListContext(ctx, addr)
	if err != nil {
		return Status{}, err
	}
	return ParseStatus(data)
}
