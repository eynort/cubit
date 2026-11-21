package combat

// Los recursos de la pelea que no son el arma: pociones (beberlas o
// tirárselas encima).
//
//   - Curar: con poca vida, una arrojadiza de curación a los pies (es al
//     momento); si no hay, una de beber si no tiene a nadie encima (beber son
//     1,6 s quieto: con un jugador pegado, es comerse dos golpes).
//   - Prepararse: al empezar contra un jugador, de lejos, fuerza (+3 por golpe,
//     el triple con II: un golpe menos para matarle), velocidad (escaparse,
//     alcanzarle) y regeneración si falta vida. Lo que ya tiene puesto no se
//     repite.
//   - De un jugador no se huye, ni con la perla de ender (combat.go).

import (
	"context"
	"strings"

	"github.com/mj41/go-mc26-kit/bot/act"
	"github.com/mj41/go-mc26-kit/bot/basic"
	"github.com/mj41/go-mc26/data/registryid"
	"github.com/mj41/go-mc26/level/component"
)

// potion es una poción del inventario: qué es (healing, strong_strength…) y si se tira.
type potion struct {
	slot   int
	kind   string
	splash bool
}

// effect es el efecto que da (sin long_ ni strong_): healing, strength…
func (p potion) effect() string {
	k := strings.TrimPrefix(strings.TrimPrefix(p.kind, "long_"), "strong_")
	if k == "swiftness" {
		return "speed"
	}
	return k
}

// potions son las pociones que lleva (de beber y arrojadizas; las persistentes no).
func (f *Fighter) potions() []potion {
	inv, _ := f.inventory()
	var out []potion
	for i := slotMainStart; i <= slotOffhand; i++ {
		n := nameOf(inv[i])
		if n != "minecraft:potion" && n != "minecraft:splash_potion" {
			continue
		}
		pc := act.Component[*component.PotionContents](inv[i])
		if pc == nil || !pc.Potion.Has {
			continue
		}
		id := int(pc.Potion.Val)
		if id < 0 || id >= len(registryid.Potion) {
			continue
		}
		out = append(out, potion{slot: i, kind: strings.TrimPrefix(registryid.Potion[id], "minecraft:"), splash: n == "minecraft:splash_potion"})
	}
	return out
}

// pickPotion: la mejor poción con ese efecto (la arrojadiza si splash; la
// fuerte antes que la normal).
func pickPotion(ps []potion, effect string, splash bool) (potion, bool) {
	var best potion
	found := false
	for _, p := range ps {
		if p.effect() != effect || p.splash != splash {
			continue
		}
		if !found || strings.HasPrefix(p.kind, "strong_") && !strings.HasPrefix(best.kind, "strong_") {
			best, found = p, true
		}
	}
	return best, found
}

// usePotions decide si toca una poción; dice si usó una (el paso se fue en eso).
func (f *Fighter) usePotions(ctx context.Context, st basic.Status, target *foe, near float64) bool {
	if f.steps-f.potionAt < 20 || f.d.Hands.Busy() {
		return false
	}
	ps := f.potions()
	if len(ps) == 0 {
		return false
	}
	// curar
	if st.Health <= 8 {
		if p, ok := pickPotion(ps, "healing", true); ok {
			return f.throwDown(ctx, p, "me curo")
		}
		if p, ok := pickPotion(ps, "healing", false); ok && near >= 4 {
			return f.drink(ctx, p, "me curo")
		}
	}
	if target == nil || target.e.Type != "minecraft:player" {
		return false
	}
	// prepararse contra un jugador: lo que no tenga ya puesto
	for _, want := range []string{"strength", "speed", "regeneration"} {
		if _, on := st.Effects["minecraft:"+want]; on || want == "regeneration" && st.Health >= 14 {
			continue
		}
		if p, ok := pickPotion(ps, want, true); ok && near >= 3 {
			return f.throwDown(ctx, p, "antes de la pelea")
		}
		if p, ok := pickPotion(ps, want, false); ok && near >= 10 {
			return f.drink(ctx, p, "antes de la pelea")
		}
	}
	return false
}

// throwDown tira una arrojadiza a los pies (mirando abajo: le cae encima).
func (f *Fighter) throwDown(ctx context.Context, p potion, why string) bool {
	if !f.holdChecked(ctx, p.slot, "minecraft:splash_potion") {
		return false
	}
	f.potionAt = f.steps
	f.d.Hands.UseItemLooking(f.d.Player.Position().Yaw, 90, nil)
	f.logf("poción arrojadiza de %s a los pies (%s)", p.kind, why)
	_ = f.d.Ctl.WaitTicks(ctx, 2)
	return true
}

// drink bebe una poción (como comer: quieto, 1,6 s).
func (f *Fighter) drink(ctx context.Context, p potion, why string) bool {
	f.potionAt = f.steps
	f.logf("bebo %s (%s)", p.kind, why)
	pos := f.d.Player.Position()
	f.eat(ctx, food{slot: p.slot}, pos.X, pos.Z)
	return true
}

// holdChecked pone en la mano lo de slot y espera a verlo (lo confirma el
// servidor: con retraso, unos ticks).
func (f *Fighter) holdChecked(ctx context.Context, slot int, name string) bool {
	if err := f.hold(ctx, slot); err != nil {
		return false
	}
	for i := 0; i < 10; i++ {
		inv, h := f.inventory()
		if nameOf(inv[slotHotbar+h]) == name {
			return true
		}
		if f.d.Ctl.WaitTicks(ctx, 1) != nil {
			return false
		}
	}
	return false
}
