package combat

// La mano izquierda: escudo o tótem según la pelea.
//
//   - El escudo para el daño que viene de delante: una flecha, un golpe, la
//     explosión de un creeper (levantado y mirándolo, no hace nada). Es lo
//     que se lleva peleando contra lo que se puede parar.
//   - El tótem salva de una muerte, venga de donde venga: fuera de pelea
//     (una caída, la lava, algo que no se vio venir) y con poca vida rodeado,
//     cuando el escudo ya no basta.
//
// Se cambia como una persona: con la tecla F (el objeto de la barra rápida
// pasa a la mano izquierda y lo de ella a la barra), que vale andando y
// peleando. Por eso, tranquilo, deja el otro (escudo o tótem) en la barra.

import (
	"context"
	"strings"

	pk "github.com/mj41/go-mc26/net/packet"
	"github.com/mj41/go-mc26/protocol/play"
	"github.com/mj41/go-mc26/protocol/types"
)

const (
	itemShield = "minecraft:shield"
	itemTotem  = "minecraft:totem_of_undying"

	// totemBelow: con menos vida y rodeado, el tótem; shieldFrom: con esta
	// vida o más vuelve el escudo (el margen evita cambiar a cada golpe).
	totemBelow = 8
	shieldFrom = 12
	totemLast  = 6 // con menos, el tótem aunque sea uno contra uno (un golpe más mata)
)

// wantOffhand es lo que conviene en la mano izquierda ahora ("" si da igual:
// no tiene ni escudo ni tótem).
func (f *Fighter) wantOffhand(foes []foe, health float32) string {
	inv, _ := f.inventory()
	has := map[string]bool{}
	for i := slotMainStart; i <= slotOffhand; i++ {
		has[nameOf(inv[i])] = true
	}
	cur := nameOf(inv[slotOffhand])
	switch {
	case !has[itemShield] && !has[itemTotem]:
		return ""
	case !has[itemShield]:
		return itemTotem
	case !has[itemTotem]:
		return itemShield
	}
	fighting, creeper, close := false, false, 0
	rushing := false // uno que pega de cerca a menos de 7: cambiar ahora reinicia el golpe y no da tiempo a subir el escudo
	for _, fo := range foes {
		if fo.avoid { // (de un warden se huye: el escudo no para lo suyo)
			continue
		}
		fighting = true
		if !rangedMobs[kind(fo.e)] && fo.e.Type != "minecraft:creeper" && fo.dist < 7 {
			rushing = true
		}
		if fo.e.Type == "minecraft:creeper" && fo.dist < 10 {
			creeper = true // su explosión, con el escudo, no hace nada
		}
		if fo.dist <= 8 {
			close++
		}
	}
	// contra un jugador que le supera (y sin hacha, que lo desactiva), el escudo
	// pase lo que pase: tapado no le entra nada, y cada cambio de mano es un
	// rato sin escudo (en el simulador, empezando a 7 con el tótem puesto: 5
	// golpes y muerto; con el escudo, 0 golpes en 40 s)
	outclassed := false
	for _, fo := range foes {
		if fo.id == f.matchID && fo.e.Type == "minecraft:player" && (f.stanceNow == stanceCautious || f.stanceNow == stanceAvoid) && !holdsAxe(fo.e) && !f.match.them.axe {
			outclassed = true
		}
	}
	switch {
	case outclassed:
		return itemShield
	case !fighting && f.armedPlayerNear(16):
		return itemShield // un jugador armado cerca: el escudo, listo antes de que venga
	case !fighting:
		return itemTotem
	case rushing && cur != itemShield && cur != "" && health >= totemLast:
		return cur // ya encima: con lo que hay (el golpe cargado vale más que el cambio)
	case creeper:
		return itemShield
	case close >= 2 && (health < totemBelow || cur == itemTotem && health < shieldFrom):
		return itemTotem // rodeado y sin vida: le pegan por la espalda, el escudo no basta
	case health < totemLast || cur == itemTotem && health < shieldFrom && health < totemBelow:
		return itemTotem // a un golpe de morir: lo que salva es el tótem (un hachazo, un crítico)
	}
	return itemShield
}

// manageOffhand pone en la mano izquierda lo que diga wantOffhand: con la F
// si está en la barra rápida; si está en el inventario, con un click (solo sin
// nadie encima: abrir el inventario en plena pelea es recibir golpes).
func (f *Fighter) manageOffhand(ctx context.Context, foes []foe, health float32, near float64) {
	inv, held := f.inventory()
	// tras pedir el cambio, se ve cuando contesta el servidor: hasta entonces
	// (2 s como mucho) no se vuelve a pedir, que lo desharía
	if f.offhandWant != "" && nameOf(inv[slotOffhand]) != f.offhandWant {
		if f.steps-f.offhandAt < 40 {
			return
		}
		// no llegó: otro intento (con el otro método), y a los 3, un descanso
		// (en los logs: una F cada 2 s durante 13 minutos)
		if f.offhandFails++; f.offhandFails >= 3 {
			var bar []string
			for i := slotHotbar; i <= slotHotbar+8; i++ {
				bar = append(bar, strings.TrimPrefix(nameOf(inv[i]), "minecraft:"))
			}
			f.logf("mano izquierda: no consigo poner %s (tengo %q; barra %q, casilla %d): lo dejo 2 min",
				strings.TrimPrefix(f.offhandWant, "minecraft:"), strings.TrimPrefix(nameOf(inv[slotOffhand]), "minecraft:"), bar, held)
			f.offhandRest, f.offhandFails = f.steps+2400, 0
		}
	} else if f.offhandWant != "" {
		f.offhandFails = 0 // llegó
	}
	f.offhandWant = ""
	if f.steps < f.offhandRest || f.steps-f.offhandAt < 8 || f.d.Hands.Busy() || f.d.Walker.Building() {
		return
	}
	if _, open := f.d.Screens.Open(); open {
		return // (una tarea usa un cofre, una mesa)
	}
	want := f.wantOffhand(foes, health)
	if want == "" || nameOf(inv[slotOffhand]) == want {
		return
	}
	from := -1
	for i := slotHotbar; i <= slotHotbar+8; i++ { // la barra primero: la F
		if nameOf(inv[i]) == want {
			from = i
			break
		}
	}
	if from < 0 {
		for i := slotMainStart; i < slotHotbar; i++ {
			if nameOf(inv[i]) == want {
				from = i
				break
			}
		}
	}
	if from < 0 {
		return
	}
	why := "tranquilo"
	if len(foes) > 0 {
		why = "pelea"
	}
	// con la F si está en la barra (vale andando); si no, o si la F ya falló,
	// con un click sobre su casilla (no depende de la casilla seleccionada:
	// solo quieto, como quien abre el inventario)
	if from >= slotHotbar && f.offhandFails == 0 {
		// la selección se manda siempre, justo antes: si el servidor tiene otra
		// (o el constructor cambió la mano entre medias), la F se llevaría otra cosa
		if err := f.d.Screens.SelectHotbar(from - slotHotbar); err != nil {
			f.logf("mano izquierda: %v", err)
			return
		}
		f.attackReset.Store(true)
		if inv2, h := f.inventory(); h != from-slotHotbar || nameOf(inv2[from]) != want {
			return // (cambió entre medias: luego)
		}
		a := play.PlayerAction{Action: types.PlayerActionActionSwapItemWithOffhand}
		if err := f.d.Client.Conn.WritePacket(pk.Marshal(a.PacketID(), a)); err != nil {
			f.logf("mano izquierda: %v", err)
			return
		}
	} else {
		if near < 3.5 || f.moving() {
			return // alguien encima, o andando: luego
		}
		if err := f.d.Screens.Click(0, from, offhandButton, types.ContainerInputSwap); err != nil {
			f.logf("mano izquierda: %v", err)
			return
		}
	}
	f.offhandAt, f.offhandWant = f.steps, want
	f.offhandSwapped.Store(true) // (Think: un escudo levantado ya no lo está)
	f.logf("mano izquierda: %s (%s, vida %.0f)", strings.TrimPrefix(want, "minecraft:"), why, health)
	_ = f.d.Ctl.WaitTicks(ctx, 2)
}

// moving: el bot anda (teclas o velocidad): un click en el inventario ahora
// no lo haría una persona (con el inventario abierto no se anda) y un
// anticheat lo rechaza.
func (f *Fighter) moving() bool {
	st := f.d.Ctl.State()
	in := st.Input
	if in.Forward || in.Backward || in.Left || in.Right || in.Jump {
		return true
	}
	if p := f.d.Mover.Player(); p != nil && p.Vel.HorizontalSqr() > 1e-4 {
		return true
	}
	return false
}

// stageOffhand: tranquilo, deja en la barra rápida el escudo o el tótem que
// no está en la mano izquierda, para cambiarlos con la F en plena pelea.
func (f *Fighter) stageOffhand(ctx context.Context) {
	if f.steps-f.stagedAt < 60 || f.d.Walker.Building() || f.d.Hands.Busy() {
		return
	}
	f.stagedAt = f.steps
	inv, held := f.inventory()
	for _, want := range []string{itemShield, itemTotem} {
		if nameOf(inv[slotOffhand]) == want {
			continue
		}
		inBar, at := false, -1
		for i := slotMainStart; i <= slotHotbar+8; i++ {
			if nameOf(inv[i]) == want {
				if i >= slotHotbar {
					inBar = true
				} else if at < 0 {
					at = i
				}
			}
		}
		if inBar || at < 0 {
			continue
		}
		h := f.spareHotbar()
		if h == held {
			return // la barra está llena y la mano la usa una tarea: luego
		}
		if err := f.d.Screens.Click(0, at, h, types.ContainerInputSwap); err != nil {
			f.logf("barra rápida: %v", err)
			return
		}
		f.logf("%s a la barra rápida (para cambiarlo con la F)", strings.TrimPrefix(want, "minecraft:"))
		_ = f.d.Ctl.WaitTicks(ctx, 2)
		return
	}
}

// armedPlayerNear: hay un jugador armado (que no es de los nuestros ni el
// protegido) a menos de r bloques.
func (f *Fighter) armedPlayerNear(r float64) bool {
	p := f.d.Player.Position()
	for _, e := range f.d.Entities.Nearby(p.X, p.Y, p.Z, r) {
		if e.Type == "minecraft:player" && e.ID != int32(f.d.Player.Login.PlayerID) && !f.isGuarded(e) && !dead(e) &&
			(f.d.Friend == nil || !f.d.Friend(e.UUID)) && armedEntity(e) {
			return true
		}
	}
	return false
}
