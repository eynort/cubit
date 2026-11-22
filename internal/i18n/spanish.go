package i18n

import (
	"strings"
	"unicode"
)

// spanishWords: palabras que delatan un texto en español (las de los estados,
// las tareas y el combate). Sin las que también son inglesas o neutras ("a",
// "y" de "y=50", "no").
var spanishWords = map[string]bool{
	"tranquilo": true, "peleando": true, "retirándose": true, "comiendo": true, "guardia": true, "búnker": true, "maniobrando": true,
	"contra": true, "zona": true, "protegiendo": true, "igualado": true, "gano": true, "supera": true, "jugador": true,
	"talar": true, "excavar": true, "minar": true, "recoger": true, "quedarme": true, "quieto": true, "cosas": true,
	"buscando": true, "yendo": true, "subiendo": true, "bajando": true, "cofre": true, "capa": true, "capas": true,
	"llevo": true, "esperando": true, "pico": true, "árboles": true, "troncos": true, "recogiendo": true, "cavando": true,
	"túnel": true, "galería": true, "minando": true, "empezando": true, "excavando": true, "pasada": true, "fila": true,
	"de": true, "del": true, "la": true, "el": true, "los": true, "las": true, "en": true, "con": true, "sin": true,
	"hacia": true, "para": true, "por": true, "voy": true, "hasta": true, "que": true, "lo": true, "vida": true, "comida": true,
	"tarea": true, "cola": true, "camino": true, "bloques": true, "parado": true, "terminado": true, "atascada": true,
}

// SpanishWords dice qué palabras de s delatan que está en español (letras con
// tilde, ñ, ¿, ¡ o palabras de la lista): para los tests de que con /lang en
// no sale español.
func SpanishWords(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && r != '¿' && r != '¡' }) {
		if spanishWords[w] || strings.ContainsAny(w, "áéíóúñ¿¡") {
			out = append(out, w)
		}
	}
	return out
}
