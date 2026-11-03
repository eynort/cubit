package pathing

import "strings"

// Falling: un bloque que cae si se le quita lo de debajo (grava, arena,
// cemento en polvo, yunque…). Romper lo que hay justo debajo de uno, con el
// bot debajo, es enterrarlo: se asfixia (en los logs, de 20 a 12 de vida en
// cuatro segundos cavando hacia arriba bajo grava en el Nether).
func Falling(state string) bool {
	name := strings.TrimPrefix(state, "minecraft:")
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i]
	}
	switch name {
	case "gravel", "sand", "red_sand", "suspicious_gravel", "suspicious_sand", "anvil", "chipped_anvil",
		"damaged_anvil", "dragon_egg", "pointed_dripstone":
		return true
	}
	return strings.HasSuffix(name, "_concrete_powder")
}
