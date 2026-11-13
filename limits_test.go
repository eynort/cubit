package main

import (
	"testing"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// TestLimits: la lista de bloques (con comodín), la zona y /limits.
func TestLimits(t *testing.T) {
	var l limits
	for name, want := range map[string]bool{"chest": true, "red_bed": true, "oak_sign": true, "oak_wall_hanging_sign": true,
		"spruce_door": true, "white_shulker_box": true, "shulker_box": true, "stone": false, "oak_log": false, "chipped_anvil": true} {
		if l.forbidden(name) != want {
			t.Errorf("forbidden(%s) = %v", name, !want)
		}
	}
	l.Area = &[4]int{10, -5, 0, 5}
	if !l.inArea(world.BlockPos{X: 3, Y: 200, Z: -5}) || l.inArea(world.BlockPos{X: 11, Z: 0}) {
		t.Error("zona mal")
	}

	cfgPath = ""
	cfg = botConfig{}
	for _, c := range [][]string{{"area", "1", "2", "3", "4"}, {"pvp", "off"}, {"allow", "chest"}, {"deny", "diamond_block"}} {
		if err := cmdLimits(nil, c); err != nil {
			t.Fatal(c, err)
		}
	}
	got := currentLimits()
	if got.Area == nil || *got.Area != [4]int{1, 2, 3, 4} || !got.NoPvP || got.forbidden("chest") || !got.forbidden("diamond_block") || !got.forbidden("barrel") {
		t.Errorf("/limits: %+v", got)
	}
	_ = cmdLimits(nil, []string{"reset"})
	if l := currentLimits(); l.Area != nil || l.NoPvP || !l.forbidden("chest") {
		t.Errorf("reset: %+v", l)
	}
}
