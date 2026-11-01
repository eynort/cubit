package main

import (
	"sort"
	"strings"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/mj41/go-mc26-kit/bot"
	"github.com/mj41/go-mc26/data/packetid"
	pk "github.com/mj41/go-mc26/net/packet"
)

// onlineNames es la lista de jugadores conectados (tab), copiada cada vez que
// cambia en el mismo hilo que la actualiza: la consola la lee sin carreras.
var onlineNames atomic.Pointer[[]string]

// nameByUUID son los nombres de los jugadores conectados por uuid (misma copia).
var nameByUUID atomic.Pointer[map[uuid.UUID]string]

// nameOf es el nombre del jugador uid ("" si no está en la lista).
func nameOf(uid uuid.UUID) string {
	if m := nameByUUID.Load(); m != nil {
		return (*m)[uid]
	}
	return ""
}

// watchPlayers mantiene onlineNames al día (prioridad baja: después de que
// playerlist procese el paquete).
func (b *Bot) watchPlayers() {
	update := func(pk.Packet) error {
		var names []string
		byID := map[uuid.UUID]string{}
		for id, p := range b.Players.PlayerInfos {
			byID[id] = p.Name
			if p.Name != "" && !strings.EqualFold(p.Name, botName) {
				names = append(names, p.Name)
			}
		}
		sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
		onlineNames.Store(&names)
		nameByUUID.Store(&byID)
		return nil
	}
	b.Client.Events.AddListener(
		bot.PacketHandler{Priority: -10, ID: packetid.ClientboundPlayPlayerInfoUpdate, F: update},
		bot.PacketHandler{Priority: -10, ID: packetid.ClientboundPlayPlayerInfoRemove, F: update},
	)
}

// playerNames son los jugadores conectados cuyo nombre empieza por prefix.
func playerNames(prefix string) []string {
	p := onlineNames.Load()
	if p == nil {
		return nil
	}
	var out []string
	for _, n := range *p {
		if strings.HasPrefix(strings.ToLower(n), strings.ToLower(prefix)) {
			out = append(out, n)
		}
	}
	return out
}

// isOnline dice si name está en la lista de conectados.
func isOnline(name string) bool {
	if p := onlineNames.Load(); p != nil {
		for _, n := range *p {
			if strings.EqualFold(n, name) {
				return true
			}
		}
	}
	return false
}
