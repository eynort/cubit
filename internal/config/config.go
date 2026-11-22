// Package config: el formato de bot.json, el que comparten el bot (que lo
// lee y lo guarda) y el hub (que lo crea para cada bot y lo lee para el panel).
package config

// Bot es bot.json.
type Bot struct {
	Schema     int      `json:"schema"` // el formato (migrate.go)
	Server     string   `json:"server"` // el último
	Name       string   `json:"name"`   // el último
	Owners     []string `json:"owners"`
	PanelToken string   `json:"panel_token,omitempty"` // la clave del panel web (panel.go)
	Seeded     bool     `json:"seeded,omitempty"`      // ya se usaron los flags una vez (--defaults)
}
