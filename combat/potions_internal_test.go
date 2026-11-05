package combat

import "testing"

func TestPickPotion(t *testing.T) {
	ps := []potion{
		{slot: 10, kind: "healing", splash: false},
		{slot: 11, kind: "strong_healing", splash: true},
		{slot: 12, kind: "healing", splash: true},
		{slot: 13, kind: "long_swiftness", splash: false},
		{slot: 14, kind: "strength", splash: false},
	}
	if p, ok := pickPotion(ps, "healing", true); !ok || p.slot != 11 {
		t.Errorf("curar arrojadiza: %+v %v (quería la fuerte, 11)", p, ok)
	}
	if p, ok := pickPotion(ps, "healing", false); !ok || p.slot != 10 {
		t.Errorf("curar de beber: %+v", p)
	}
	if p, ok := pickPotion(ps, "speed", false); !ok || p.slot != 13 {
		t.Errorf("velocidad (swiftness): %+v", p)
	}
	if _, ok := pickPotion(ps, "strength", true); ok {
		t.Error("fuerza arrojadiza: no hay")
	}
}
