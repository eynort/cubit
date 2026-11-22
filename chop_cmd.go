package main

import (
	"log"
	"math"

	"github.com/mj41/go-mc26-kit/bot/world"

	"cubit/automation"
)

// cmdChop: /chop <tipos> [<x> <y> <z>] (sin coordenadas, donde está) — ir hasta ese punto (con ~ relativo al
// bot) y talar los árboles enteros de esas maderas (separadas por comas) que
// haya a menos de automation.ChopRadius bloques.
func cmdChop(b *Bot, args []string) error {
	if len(args) == 1 { // sin coordenadas: donde está (/chop oak)
		args = append(args, "~", "~", "~")
	}
	if len(args) != 4 {
		return errf("usage: /chop <woods> [<x> <y> <z>] (e.g. /chop oak,birch ~ ~ ~)", "uso: /chop <maderas> [<x> <y> <z>] (ej: /chop oak,birch ~ ~ ~)")
	}
	kinds, err := automation.ParseWoods(args[0])
	if err != nil {
		return errf("%v", "%v", err)
	}
	p := b.Player.Position()
	base := []float64{p.X, p.Y + 1e-6, p.Z}
	var v [3]int
	for i, a := range args[1:] {
		n, err := coord(a, base[i])
		if err != nil {
			return err
		}
		v[i] = int(math.Floor(n))
	}
	center := world.BlockPos{X: v[0], Y: v[1], Z: v[2]}
	log.Printf("talar %v alrededor de %v (radio %d; /stop para parar)", kinds, center, automation.ChopRadius)
	b.startWork("talar", automation.Chop(kinds, center))
	return nil
}
