package main

// El autocompletado de los argumentos de una orden (la orden en sí la
// completa suggestions, en tui.go): subcomandos, jugadores, opciones, listas
// con comas, números, coordenadas, otra orden entera (/queue add) y las
// condiciones de parada (until). Cada sugerencia lleva su explicación: lo
// que hace esa opción, no la ayuda de toda la orden.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// untilCmds: órdenes que aceptaban "until …" (ya ninguna: se quitó).
var untilCmds = map[string]bool{}

// completeArgs completa lo que va tras "/name ": parts es la línea partida
// por espacios (parts[0] la orden, la última lo que se está escribiendo).
func completeArgs(c command, name string, parts []string) []suggestion {
	words := parts[1:]
	cur := words[len(words)-1]
	prev := words[:len(words)-1]
	base := "/" + strings.Join(parts[:len(parts)-1], " ") + " "

	args, at := c.args, len(prev) // los argumentos que tocan y cuál se escribe
	task := untilCmds[name]       // (en un subcomando, no: /mine chest no es un trabajo)
	if len(c.subs) > 0 {
		if len(prev) == 0 { // la primera palabra: un subcomando o el primer argumento
			var out []suggestion
			for _, s := range c.subs {
				if strings.HasPrefix(s.name, strings.ToLower(cur)) {
					val := base + s.name
					if len(s.args) > 0 {
						val += " "
					}
					out = append(out, suggestion{value: val, label: s.name, desc: s.desc.String()})
				}
			}
			if len(c.args) > 0 {
				out = append(out, completeArg(c, name, c.args, 0, parts, cur, base)...)
			}
			if len(out) == 0 {
				out = append(out, suggestion{label: usageOf(c, name), desc: tr("nothing starts with \"", "nada empieza por \"") + cur + "\""})
			}
			return out
		}
		if s, ok := findSub(c, prev[0]); ok {
			args, at, task = s.args, len(prev)-1, false
		}
	}
	if at >= len(args) {
		if n := len(args); n > 0 && args[n-1].rest {
			at = n - 1
		} else {
			if task && strings.HasPrefix("until", strings.ToLower(cur)) {
				return []suggestion{untilSuggestion(base)}
			}
			return []suggestion{{label: tr("(nothing more)", "(nada más)"), desc: usageOf(c, name)}}
		}
	}
	out := completeArg(c, name, args, at, parts, cur, base)
	// tras los argumentos obligatorios, un trabajo puede llevar "until"
	if task && cur != "" && strings.HasPrefix("until", strings.ToLower(cur)) {
		out = append(out, untilSuggestion(base))
	} else if task && cur == "" && (args[at].opt || at > 0) {
		out = append(out, untilSuggestion(base))
	}
	return out
}

func untilSuggestion(base string) suggestion {
	return suggestion{value: base + "until ", label: "until …",
		desc: tr("stop on its own: until got diamond 10 | time 20m", "parar sola: until got diamond 10 | time 20m")}
}

// findSub es el subcomando de esa palabra.
func findSub(c command, w string) (sub, bool) {
	for _, s := range c.subs {
		if strings.EqualFold(s.name, w) {
			return s, true
		}
	}
	return sub{}, false
}

// subUsage: " <x> <y> <z>" (los argumentos de un subcomando, para la etiqueta).
func subUsage(s sub) string {
	var b strings.Builder
	for _, a := range s.args {
		b.WriteString(" " + argUsage(a))
	}
	return b.String()
}

// argUsage: "<amount>", "[<amount>]", "<on|off>".
func argUsage(a arg) string {
	u := "<" + a.name + ">"
	if (a.kind == argChoice || a.kind == argList) && len(a.choices) > 0 && len(a.choices) <= 4 {
		u = "<" + strings.Join(a.choices, "|") + ">"
	}
	if a.opt {
		u = "[" + u + "]"
	}
	return u
}

// usageOf es el uso de la orden (el escrito a mano si lo hay).
func usageOf(c command, name string) string { return c.usage(name) }

// choicesOf son las opciones de un argumento: las fijas y las calculadas.
func choicesOf(a arg) []string {
	ch := append([]string(nil), a.choices...)
	if a.dyn != nil {
		ch = append(ch, a.dyn()...)
	}
	return ch
}

// descOf es la explicación de la opción ch (la suya, o la del argumento).
func descOf(a arg, ch string) string {
	if d, ok := a.descs[ch]; ok {
		return d.String()
	}
	if a.desc.en != "" {
		return a.desc.String()
	}
	return a.choiceDesc.String()
}

// completeArg completa el argumento i de args.
func completeArg(c command, name string, args []arg, i int, parts []string, cur, base string) []suggestion {
	a := args[i]
	more := ""
	if i+1 < len(args) {
		more = " "
	}
	hint := func() suggestion {
		d := a.desc.String()
		if d == "" {
			d = usageOf(c, name)
		}
		return suggestion{label: argUsage(a), desc: d}
	}
	var out []suggestion
	switch a.kind {
	case argPlayer:
		return completePlayer(c, a, i, parts, cur, base, more)
	case argChoice:
		for _, ch := range choicesOf(a) {
			if strings.HasPrefix(strings.ToLower(ch), strings.ToLower(cur)) {
				out = append(out, suggestion{value: base + ch + more, label: ch, desc: descOf(a, ch)})
			}
		}
		if len(out) == 0 {
			out = append(out, suggestion{label: argUsage(a), desc: tr("none starts with \"", "ninguna empieza por \"") + cur + "\": " + strings.Join(choicesOf(a), ", ")})
		}
	case argList: // "diamond,ir" → completa "ir", sin repetir las de antes
		done := strings.Split(cur, ",")
		last := done[len(done)-1]
		done = done[:len(done)-1]
		prefix := ""
		if len(done) > 0 {
			prefix = strings.Join(done, ",") + ","
		}
		for _, ch := range choicesOf(a) {
			if slices.Contains(done, ch) || !strings.HasPrefix(ch, strings.ToLower(last)) {
				continue
			}
			out = append(out, suggestion{value: base + prefix + ch + more, label: prefix + ch, desc: descOf(a, ch)})
		}
		if len(out) == 0 {
			out = append(out, suggestion{label: argUsage(a), desc: tr("none starts with \"", "ninguno empieza por \"") + last + "\": " + strings.Join(choicesOf(a), ", ")})
		} else if last == "" && len(done) == 0 {
			out = append(out, suggestion{label: tr("a,b", "a,b"), desc: tr("several, separated by commas (no spaces)", "varios, separados por comas (sin espacios)")})
		}
	case argNumber:
		out = append(out, hint())
		for _, ch := range choicesOf(a) {
			if strings.HasPrefix(ch, strings.ToLower(cur)) {
				out = append(out, suggestion{value: base + ch + more, label: ch, desc: descOf(a, ch)})
			}
		}
		if cur != "" {
			if _, err := strconv.Atoi(cur); err != nil && len(out) == 1 {
				out[0].desc = fmt.Sprintf(tr("\"%s\" is not a number", "\"%s\" no es un número"), cur)
			}
		}
	case argCoord:
		out = append(out, hint())
		// en la primera de un grupo de coordenadas, todas de golpe: donde está el bot
		if n := coordGroup(args, i); cur == "" && n >= 2 {
			out = append(out, suggestion{value: base + strings.TrimSpace(strings.Repeat("~ ", n)), label: strings.TrimSpace(strings.Repeat("~ ", n)),
				desc: tr("where the bot is (change it: ~2 ~-3)", "donde está el bot (cámbialo: ~2 ~-3)")})
		}
		for _, ch := range choicesOf(a) { // otras formas en su lugar (here, off…)
			if strings.HasPrefix(ch, strings.ToLower(cur)) {
				out = append(out, suggestion{value: base + ch, label: ch, desc: descOf(a, ch)})
			}
		}
	case argCommand: // otra orden: lo de después, completado como esa orden
		start := len(parts) - 1 - (len(parts) - 2 - i) // la palabra donde empieza
		if s, ok := findSub(c, parts[1]); ok && len(c.subs) > 0 {
			_ = s
			start = 2 + i
		} else {
			start = 1 + i
		}
		if start >= len(parts) {
			start = len(parts) - 1
		}
		rest := append([]string(nil), parts[start:]...)
		slash := "" // (/queue /chop … y /queue chop … valen las dos)
		if strings.HasPrefix(rest[0], "/") {
			slash, rest[0] = "/", strings.TrimPrefix(rest[0], "/")
		}
		inner := suggestions("/" + strings.Join(rest, " "))
		head := "/" + strings.Join(parts[:start], " ") + " " + slash
		for _, s := range inner {
			if s.value != "" {
				s.value = head + strings.TrimPrefix(s.value, "/")
			}
			s.label = strings.TrimPrefix(s.label, "/")
			out = append(out, s)
		}
		if len(out) == 0 {
			out = append(out, hint())
		}
	default: // texto libre
		if a.amountOf > 0 {
			return completePlayer(c, a, i, parts, cur, base, more)
		}
		for _, ch := range choicesOf(a) {
			if strings.HasPrefix(strings.ToLower(ch), strings.ToLower(cur)) {
				out = append(out, suggestion{value: base + ch + more, label: ch, desc: descOf(a, ch)})
			}
		}
		out = append(out, hint())
	}
	return out
}

// completePlayer: un jugador conectado (y lo que lleva el bot, para /drop;
// opciones fijas como "in"; la pista de las coordenadas).
func completePlayer(c command, a arg, i int, parts []string, cur, base, more string) []suggestion {
	var out []suggestion
	if a.amountOf > 0 { // la cantidad de un objeto: lo que lleva de él (sin cantidad: todo)
		item := normItem(parts[a.amountOf])
		if n := carriedCount(item); n > 0 {
			out = append(out, suggestion{label: "<" + a.name + ">", desc: fmt.Sprintf(tr("1-%d; without it, all of it (%d %s)", "1-%d; sin ella, todo (%d %s)"), n, n, item)})
			if cur == "" {
				out = append(out, suggestion{value: base + strconv.Itoa(n) + more, label: strconv.Itoa(n), desc: tr("all it carries", "todo lo que lleva")})
			}
			if a.orPlayer { // o directamente el jugador al que dárselo todo
				for _, p := range playerNames(cur) {
					out = append(out, suggestion{value: base + p, label: p, desc: fmt.Sprintf(tr("give them all the %s", "darle todo el %s"), item)})
				}
			}
			return out
		}
		return []suggestion{{label: "<" + a.name + ">", desc: usageOf(c, parts[0])}}
	}
	if i > 0 && len(parts) > i && c.args[i-1].orPlayer && !isDropAmount(parts[i]) {
		return nil // el argumento de antes ya era el jugador (/drop dirt Alex …)
	}
	if a.items { // lo que lleva el bot (/drop): primero los objetos, luego los jugadores
		for _, it := range carriedItems(cur) {
			out = append(out, suggestion{value: base + it.name + more, label: it.name,
				desc: fmt.Sprintf(tr("×%d in the inventory", "×%d en el inventario"), it.count)})
		}
	}
	for _, ch := range choicesOf(a) {
		if strings.HasPrefix(ch, strings.ToLower(cur)) {
			out = append(out, suggestion{value: base + ch + " ", label: ch, desc: descOf(a, ch)})
		}
	}
	for _, n := range playerNames(cur) {
		out = append(out, suggestion{value: base + n + more, label: n, desc: tr("online player", "jugador conectado")})
	}
	if a.hint.en != "" { // otra forma de rellenarlo (coordenadas): la pista, y con Tab la posición actual
		h := strings.SplitN(a.hint.String(), ":", 2)
		out = append(out, suggestion{label: h[0], desc: strings.TrimSpace(h[len(h)-1])})
		if cur == "" {
			tildes := strings.TrimSpace(strings.Repeat("~ ", strings.Count(h[0], "<"))) // una por coordenada de la pista
			out = append(out, suggestion{value: base + tildes, label: tildes, desc: tr("where the bot is (change it: ~10 ~ ~-5 …)", "donde está el bot (cámbialo: ~10 ~ ~-5 …)")})
		}
	}
	if len(out) == 0 && a.items {
		out = append(out, suggestion{label: "<" + a.name + "> | <player>", desc: tr("it carries nothing and no online player starts with \"", "no lleva nada ni hay jugador conectado que empiece por \"") + cur + "\""})
	} else if len(out) == 0 {
		out = append(out, suggestion{label: "<" + a.name + ">", desc: tr("no online player starts with \"", "ningún jugador conectado empieza por \"") + cur + "\""})
	}
	return out
}

// coordGroup: si el argumento i empieza un grupo de coordenadas, cuántas son
// (las obligatorias seguidas; si todas son opcionales, hasta 3). 0 si no.
func coordGroup(args []arg, i int) int {
	if i > 0 && args[i-1].kind == argCoord && !args[i].corner {
		return 0
	}
	n := 0
	for k := i; k < len(args) && args[k].kind == argCoord && !args[k].opt && (k == i || !args[k].corner); k++ {
		n++
	}
	if n == 0 {
		for k := i; k < len(args) && k < i+3 && args[k].kind == argCoord; k++ {
			n++
		}
	}
	return n
}
