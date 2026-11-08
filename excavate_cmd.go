package main

import (
	"log"
	"math"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/world"

	"minecraft-bot/automation"
)

// cmdExcavate: /excavate <x1> <y1> <z1> <x2> <y2> <z2> — vaciar la caja
// entre esas dos esquinas (con ~ relativo al bot).
func cmdExcavate(b *Bot, args []string) error {
	if len(args) != 6 {
		return errf("usage: /excavate <x1> <y1> <z1> <x2> <y2> <z2> (two opposite corners)",
			"uso: /excavate <x1> <y1> <z1> <x2> <y2> <z2> (dos esquinas opuestas)")
	}
	p := b.Player.Position()
	base := []float64{p.X, p.Y + 1e-6, p.Z}
	var v [6]int
	for i, a := range args {
		n, err := coord(a, base[i%3])
		if err != nil {
			return err
		}
		v[i] = int(math.Floor(n))
	}
	box := automation.NewBox(world.BlockPos{X: v[0], Y: v[1], Z: v[2]}, world.BlockPos{X: v[3], Y: v[4], Z: v[5]})
	if n := box.Volume(); n > automation.ExcavateMax {
		return errf("that box has %d blocks: too many (at most %d); check the coordinates",
			"esa caja tiene %d bloques: demasiados (como mucho %d); revisa las coordenadas", n, automation.ExcavateMax)
	}
	where := "lo cavado al almacén"
	if c, ok := b.Tasks.Store.MineChest(string(b.Player.Spawn.Dimension)); ok {
		where = "lo cavado y los picos, en el cofre de minar de " + c.String()
	}
	log.Printf("excavar %s (%d bloques como mucho): %s (/stop para parar)", box, box.Volume(), where)
	b.startWork("excavar", automation.Excavate(box))
	return nil
}

// cmdProtected: /protected [list | here | clear] — los sitios donde el
// servidor dijo "no tienes permiso".
func cmdProtected(b *Bot, args []string) error {
	st := b.Tasks.Store
	action := "list"
	if len(args) > 0 {
		action = strings.ToLower(args[0])
	}
	switch action {
	case "list":
		l := st.ProtectedList()
		if len(l) == 0 {
			log.Print(tr("protected places: none", "sitios protegidos: ninguno"))
			return nil
		}
		for _, p := range l {
			log.Printf(tr("protected: %v (x %d…%d, z %d…%d)", "protegido: %v (x %d…%d, z %d…%d)"), p,
				p.X-automation.ProtectRadius, p.X+automation.ProtectRadius, p.Z-automation.ProtectRadius, p.Z+automation.ProtectRadius)
		}
		return nil
	case "here":
		p := b.Player.Position()
		here := world.BlockPos{X: int(math.Floor(p.X)), Y: int(math.Floor(p.Y)), Z: int(math.Floor(p.Z))}
		log.Printf(tr("protected: forgot %d around %v", "protegido: olvido %d alrededor de %v"), st.ClearProtected(&here), here)
		return nil
	case "clear":
		log.Printf(tr("protected: forgot all (%d)", "protegido: los olvido todos (%d)"), st.ClearProtected(nil))
		return nil
	}
	return errf("usage: /protected [list | here | clear]", "uso: /protected [list | here | clear]")
}
