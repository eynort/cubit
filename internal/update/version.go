package update

import (
	"strconv"
	"strings"
)

// semver: lo justo para comparar versiones vX.Y.Z[-pre].
type semver struct {
	n   [3]int
	pre string
}

func parseVersion(s string) (semver, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "v"), "V")
	if i := strings.IndexByte(s, '+'); i >= 0 {
		s = s[:i] // (metadatos de compilación: no cuentan)
	}
	var v semver
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s, v.pre = s[:i], s[i+1:]
	}
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v.n[i] = n
	}
	return v, true
}

// ValidVersion: si s es una versión comparable (un desarrollo "" o "dev" no).
func ValidVersion(s string) bool { _, ok := parseVersion(s); return ok }

// Compare devuelve -1, 0 o 1 (a menor, igual o mayor que b); ok=false si alguna no es una versión.
func Compare(a, b string) (c int, ok bool) {
	x, ok1 := parseVersion(a)
	y, ok2 := parseVersion(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	for i := range x.n {
		if x.n[i] != y.n[i] {
			if x.n[i] < y.n[i] {
				return -1, true
			}
			return 1, true
		}
	}
	switch {
	case x.pre == y.pre:
		return 0, true
	case x.pre == "": // (1.0.0 > 1.0.0-rc1)
		return 1, true
	case y.pre == "":
		return -1, true
	}
	return strings.Compare(x.pre, y.pre), true
}

// Newer: latest es una versión más nueva que current. Con una versión que no se
// entiende (compilación de desarrollo), nunca.
func Newer(latest, current string) bool {
	c, ok := Compare(latest, current)
	return ok && c > 0
}
