package main

// Presupuesto de contexto de la IA, como el autocompactado del harness de
// Claude Code: lo que se le manda al modelo en cada petición (el system, el
// resumen, los mensajes y lo que se reserva para la respuesta) no pasa nunca
// de aiBudget (por defecto el 30 %) de su contexto (num_ctx). Un modelo
// pequeño contesta peor y más despacio cuanto más largo es lo que lee.
//
//  1. Medir: la estimación (runas / 3) se calibra con lo que Ollama dice que
//     contó (prompt_eval_count) en cada respuesta.
//  2. Antes de cada petición (también entre una ronda de herramientas y la
//     siguiente), si no cabe, por orden, hasta que quepa:
//     a. microcompactado: los resultados de herramientas de turnos ya
//     contestados se recortan (lo que importaba ya está en la respuesta);
//     b. compactado: lo viejo se resume (un resumen con secciones, como
//     /compact) y queda solo lo último, hasta aiKeepTail del presupuesto;
//     c. lo que quede grande del turno en curso (un resultado enorme, un
//     mensaje muy largo) se recorta, y el resumen si hace falta.
//  3. Tras contestar, al pasar de aiCompactAt del presupuesto, se compacta en
//     segundo plano: la siguiente pregunta no espera al resumen.
//
// La petición del resumen también cabe en el presupuesto: si lo viejo es
// mucho, se resume por trozos (el resumen de un trozo entra con el siguiente).

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"strings"
	"unicode/utf8"
)

var aiBudgetFlag = flag.Float64("ai-budget", 0.30, "parte del contexto de la IA que se usa como mucho (0.30 = 30 %): al llegar, resume lo viejo")

const (
	aiCompactAt = 0.80 // tras contestar, se compacta al pasar de esta parte del presupuesto
	// tras compactar queda poco (como en Claude Code): el resumen y lo último,
	// y mucho sitio antes de la siguiente (si no, se compactaría cada 2 mensajes)
	aiKeepTail   = 0.10 // lo último que queda sin resumir: esta parte del presupuesto, como mucho
	aiSummaryMax = 0.08 // el resumen: como mucho esta parte del presupuesto
	aiToolKept   = 200  // un resultado de herramienta ya contestado se queda en estas runas
)

// budget son los tokens que puede ocupar una petición (con la respuesta).
func (a *AI) budget() int {
	f := a.budgetFrac
	if f <= 0 || f > 1 {
		f = 0.30
	}
	return int(f * float64(a.numCtx))
}

// tokens es lo que ocupa (calibrado con lo que contó Ollama) system + msgs.
func (a *AI) tokens(sys string, msgs []aiMsg) int {
	a.mu.Lock()
	r := a.ratio
	a.mu.Unlock()
	if r <= 0 {
		r = 1
	}
	return int(math.Ceil(float64(a.estimate(sys, msgs)) * r))
}

// need es lo que pide una petición: lo que se manda más la respuesta.
func (a *AI) need(sys string, msgs []aiMsg) int { return a.tokens(sys, msgs) + aiReplyTokens }

// calibrate ajusta la estimación con lo que Ollama contó de verdad de lo
// mandado (prompt_eval_count: cuenta también lo que tenía en caché).
func (a *AI) calibrate(sys string, msgs []aiMsg, counted int) {
	est := a.estimate(sys, msgs)
	if counted <= 0 || est <= 0 {
		return
	}
	got := min(1.5, max(0.5, float64(counted)/float64(est)))
	a.mu.Lock()
	if a.ratio <= 0 {
		a.ratio = got
	} else {
		a.ratio = 0.7*a.ratio + 0.3*got
	}
	a.mu.Unlock()
}

// fit deja la conversación de c dentro del presupuesto para la siguiente
// petición (sysOf da el system, que lleva el resumen: cambia al compactar).
func (a *AI) fit(ctx context.Context, c *aiConv, sysOf func() string) {
	budget := a.budget()
	if a.need(sysOf(), c.msgs) <= budget {
		return
	}
	// a. microcompactado
	if n := microcompact(c.msgs); n > 0 {
		log.Printf("[IA] %s: recorto %d resultados de herramientas viejos (contexto al límite)", c.who, n)
		if a.need(sysOf(), c.msgs) <= budget {
			return
		}
	}
	// b. compactado
	if err := a.compact(ctx, c); err != nil {
		log.Printf("[IA] no pude compactar: %v", err)
	}
	if a.need(sysOf(), c.msgs) <= budget {
		return
	}
	// c. lo grande del turno en curso, y el resumen
	for _, limit := range []int{600, 200} {
		for i := range c.msgs {
			if c.msgs[i].Role == "tool" || c.msgs[i].Role == "user" && i < len(c.msgs)-1 {
				c.msgs[i].Content = shorten(c.msgs[i].Content, limit)
			}
		}
		if a.need(sysOf(), c.msgs) <= budget {
			return
		}
	}
	if last := len(c.msgs) - 1; last >= 0 && c.msgs[last].Role == "user" {
		c.msgs[last].Content = shorten(c.msgs[last].Content, 400)
	}
	for c.summary != "" && a.need(sysOf(), c.msgs) > budget {
		n := utf8.RuneCountInString(c.summary)
		if n < 100 {
			c.summary = ""
			break
		}
		c.summary = truncRunes(c.summary, n*2/3)
	}
	if need := a.need(sysOf(), c.msgs); need > budget {
		a.warnOnce.Do(func() {
			log.Printf("[IA] el system solo ya ocupa ~%d tokens y el presupuesto es %d (%.0f%% de %d): sube --ai-budget o --ai-ctx, o acorta ia_servidor.md",
				a.tokens(sysOf(), nil)+aiReplyTokens, budget, 100*a.budgetFrac, a.numCtx)
		})
	}
}

// microcompact recorta los resultados de herramientas de los turnos ya
// contestados (antes del último mensaje del jugador). Dice cuántos recortó.
func microcompact(msgs []aiMsg) int {
	lastUser := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			lastUser = i
			break
		}
	}
	n := 0
	for i := 0; i < lastUser; i++ {
		if msgs[i].Role == "tool" && utf8.RuneCountInString(msgs[i].Content) > aiToolKept+20 {
			msgs[i].Content = shorten(msgs[i].Content, aiToolKept)
			n++
		}
	}
	return n
}

// dropOldStates quita el [estado: …] de los mensajes del jugador ya
// contestados: era el de entonces (vida, posición, quién estaba), no sirve y
// cada uno son unos 60 tokens que se arrastran en cada petición.
func dropOldStates(msgs []aiMsg) {
	for i := range msgs {
		if m := &msgs[i]; m.Role == "user" && strings.HasPrefix(m.Content, "[estado: ") {
			if j := strings.Index(m.Content, "]\n"); j > 0 {
				m.Content = m.Content[j+2:]
			}
		}
	}
}

// shorten deja s en n runas como mucho, avisando de que se recortó.
func shorten(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return truncRunes(s, n) + " …(recortado)"
}

// summaryPrompt es lo que se le pide al modelo para resumir (con secciones,
// como el /compact de Claude Code, pero corto: es un modelo pequeño).
func summaryPrompt(who string, words int) string {
	return fmt.Sprintf(`Eres el que resume la conversación entre %s y el bot %s, para que el bot la siga sin el texto original.
Escribe en texto plano, en el idioma de la conversación, con estas secciones (una o dos frases cada una; si una no tiene nada, "nada"):
Quién es y cómo habla: el jugador, su tono, de qué va la charla.
Qué pidió y qué se hizo: las órdenes que se ejecutaron y cómo salieron (errores incluidos).
Datos a recordar: coordenadas, nombres, objetos, sitios, acuerdos.
Pendiente: lo que quedó por hacer o por contestar.
Como mucho %d palabras. Si hay un resumen anterior, intégralo (no lo repitas aparte).`, who, botName, words)
}

// summaryRunes: las runas que puede tener el resumen.
func (a *AI) summaryRunes() int { return max(300, int(aiSummaryMax*float64(a.budget())*3)) }

// chunks parte transcript en trozos que, con el prompt y el resumen que lleve,
// caben en el presupuesto (por líneas: no corta un mensaje si no hace falta).
func (a *AI) chunks(transcript string, prompt string) []string {
	room := (a.budget() - a.tokens(prompt, nil) - 400 - a.summaryRunes()/3) * 3 // runas para el trozo
	room = max(room, 600)
	var out []string
	var cur strings.Builder
	n := 0
	for _, line := range strings.SplitAfter(transcript, "\n") {
		l := utf8.RuneCountInString(line)
		if n > 0 && n+l > room {
			out = append(out, cur.String())
			cur.Reset()
			n = 0
		}
		if l > room { // una línea enorme: lo último de ella
			line = "…" + string([]rune(line)[l-room:])
			l = room
		}
		cur.WriteString(line)
		n += l
	}
	if n > 0 {
		out = append(out, cur.String())
	}
	return out
}
