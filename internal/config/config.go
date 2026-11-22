// Package config: el formato de bot.json, el que comparten el bot (que lo
// lee y lo guarda) y el hub (que lo crea para cada bot y lo lee para el panel).
package config

// Bot es bot.json.
type Bot struct {
	Server     string   `json:"server"` // el último
	Name       string   `json:"name"`   // el último
	Owners     []string `json:"owners"`
	PanelToken string   `json:"panel_token,omitempty"` // la clave del panel web (panel.go)
	Seeded     bool     `json:"seeded,omitempty"`      // ya se usaron los flags una vez (--defaults)
	LLM        LLM      `json:"llm,omitempty"`         // el control por lenguaje natural (llm.go)
}

// LLM: el control por lenguaje natural con un modelo local (Ollama, llm.go).
// Encendido por defecto; no hay claves.
type LLM struct {
	Off   bool   `json:"off,omitempty"`   // apagado desde el panel
	Model string `json:"model,omitempty"` // vacío = OLLAMA_MODEL o el de siempre
	URL   string `json:"url,omitempty"`   // vacío = --ollama, OLLAMA_URL o localhost
}
