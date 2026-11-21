package main

// La referencia de las órdenes, sacada de las mismas descripciones que usan
// /help y el autocompletado (así no se queda vieja):
//
//	go run . --gen-docs docs     # docs/COMMANDS.md y docs/ORDENES.md

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var genDocsFlag = flag.String("gen-docs", "", "escribir la referencia de las órdenes (COMMANDS.md en inglés, ORDENES.md en español) en esa carpeta y salir")

func pick(t text, es bool) string {
	if es && t.es != "" {
		return t.es
	}
	return t.en
}

// commandsDoc: la referencia en markdown, en inglés o en español.
func commandsDoc(es bool) string {
	var sb strings.Builder
	if es {
		sb.WriteString("# Órdenes\n\nSe escriben en la consola del bot, en el panel web o por `/msg <bot> <orden>` (solo los dueños). Siempre en inglés; las explicaciones salen en el idioma de `/lang`.\n\n_Generado con `go run . --gen-docs docs`: no lo edites a mano._\n\n")
	} else {
		sb.WriteString("# Commands\n\nType them in the bot's console, in the web panel, or whisper them with `/msg <bot> <command>` (owners only).\n\n_Generated with `go run . --gen-docs docs`: don't edit by hand._\n\n")
	}
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c := commands[n]
		fmt.Fprintf(&sb, "## /%s\n\n`%s`\n\n%s\n\n", n, c.usage(n), capital(pick(c.desc, es)))
		args := func(as []arg, indent string) {
			for _, a := range as {
				d := pick(a.desc, es)
				if d == "" {
					d = pick(a.hint, es)
				}
				opt := ""
				if a.opt {
					opt = map[bool]string{false: " (optional)", true: " (opcional)"}[es]
				}
				if d != "" || len(a.descs) > 0 {
					fmt.Fprintf(&sb, "%s- `<%s>`%s%s\n", indent, a.name, opt, prefix(": ", d))
				}
				keys := make([]string, 0, len(a.descs))
				for k := range a.descs {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					fmt.Fprintf(&sb, "%s  - `%s`: %s\n", indent, k, pick(a.descs[k], es))
				}
			}
		}
		args(c.args, "")
		for _, s := range c.subs {
			fmt.Fprintf(&sb, "- `%s%s`%s\n", s.name, subUsage(s), prefix(": ", pick(s.desc, es)))
			args(s.args, "  ")
		}
		sb.WriteString("\n")
		sb.WriteString(examplesDoc(n, es))
	}
	return sb.String()
}

func capital(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:]) + map[bool]string{true: "", false: "."}[strings.HasSuffix(s, ".") || strings.HasSuffix(s, ")")]
}

func prefix(p, s string) string {
	if s == "" {
		return ""
	}
	return p + s
}

// genDocs escribe los dos archivos.
func genDocs(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "COMMANDS.md"), []byte(commandsDoc(false)), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "ORDENES.md"), []byte(commandsDoc(true)), 0o644)
}
