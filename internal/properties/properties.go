// Package properties lee y cambia archivos .properties de Java (el
// server.properties del servidor de Minecraft) sin tocar lo demás: los
// comentarios, el orden y las claves que no se cambian se quedan como estaban.
package properties

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Read lee el archivo path: clave → valor.
func Read(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		k, v, ok := splitProp(sc.Text())
		if ok {
			m[k] = v
		}
	}
	return m, nil
}

func splitProp(line string) (k, v string, ok bool) {
	t := strings.TrimSpace(line)
	if t == "" || t[0] == '#' || t[0] == '!' {
		return "", "", false
	}
	i := strings.IndexAny(t, "=:")
	if i < 0 {
		return "", "", false
	}
	return strings.TrimSpace(t[:i]), unescapeProp(strings.TrimLeft(t[i+1:], " \t")), true
}

func unescapeProp(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			sb.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			sb.WriteByte('\n')
		case 't':
			sb.WriteByte('\t')
		case 'u':
			if i+4 < len(s) {
				if n, err := strconv.ParseUint(s[i+1:i+5], 16, 32); err == nil {
					sb.WriteRune(rune(n))
					i += 4
					continue
				}
			}
			sb.WriteByte('u')
		default:
			sb.WriteByte(s[i])
		}
	}
	return sb.String()
}

func escapeProp(s string) string {
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r == '\\', r == ':', r == '=':
			sb.WriteByte('\\')
			sb.WriteRune(r)
		case r == '\n':
			sb.WriteString(`\n`)
		case r < 0x20 || r > 0x7e:
			if r > 0xffff {
				r1, r2 := utf16Pair(r)
				fmt.Fprintf(&sb, `\u%04x\u%04x`, r1, r2)
			} else {
				fmt.Fprintf(&sb, `\u%04x`, r)
			}
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func utf16Pair(r rune) (rune, rune) {
	r -= 0x10000
	return 0xd800 + (r>>10)&0x3ff, 0xdc00 + r&0x3ff
}

// Update cambia esas claves y deja lo demás como estaba (escribe encima, sin
// cambiar el dueño del archivo: el servidor lo usa como 1000).
func Update(path string, changes map[string]string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	done := map[string]bool{}
	for i, l := range lines {
		if k, _, ok := splitProp(l); ok {
			if v, ok := changes[k]; ok {
				lines[i] = k + "=" + escapeProp(v)
				done[k] = true
			}
		}
	}
	for k, v := range changes {
		if !done[k] {
			lines = append(lines, k+"="+escapeProp(v))
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
