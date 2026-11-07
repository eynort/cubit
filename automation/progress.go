package automation

import (
	"fmt"

	"github.com/mj41/go-mc26-kit/bot/world"
)

// La progresión de un jugador hasta el hacha de hierro: madera → mesa y pico
// de madera → piedra → pico de piedra → hierro (cavando hasta la mena) →
// horno → lingotes → hacha de hierro. Lo que ya tenga se lo salta.

// pickTier es lo bueno que es el mejor pico que lleva (0 ninguno, 1 madera, 2 piedra, 3 hierro…).
func (j *Job) pickTier() int {
	items := j.Items()
	for i, t := range tiers {
		if items[t+"_pickaxe"] > 0 {
			return len(tiers) - i
		}
	}
	return 0
}

const (
	tierWood    = 1
	tierStone   = 3 // (tiers: netherite, diamond, iron, stone, golden, wooden → wooden 1, golden 2, stone 3, iron 4)
	tierIron    = 4
	tierDiamond = 5
)

// stoneOre: la piedra que da material de herramientas al picarla.
func stoneOre(name string) bool { return name == "stone" || name == "deepslate" } // no adoquines: suelen ser construcciones

// ironLevel: el hierro abunda hacia la altura 16.
const ironLevel = 16

// ironOre: las menas de hierro.
func ironOre(name string) bool { return name == "iron_ore" || name == "deepslate_iron_ore" }

// ensureLogs tala (con lo que tenga) hasta llevar n troncos o tablones equivalentes.
func (j *Job) ensureLogs(n int, kind string) error {
	have := func() int { return j.CountWhere(isLog) + j.CountWhere(isPlanks)/4 }
	skip := map[world.BlockPos]bool{}
	for tries := 0; have() < n && tries < 10; tries++ {
		j.Status("juntando madera (%d/%d)", have(), n)
		base, ok := j.findTree(kind, 40, skip)
		if !ok && kind != "" {
			base, ok = j.findTree("", 40, skip) // para empezar, cualquier madera vale
		}
		if !ok && tries == 0 { // recién llegado (un teletransporte): los chunks aún cargan
			if err := j.Wait(60); err != nil {
				return err
			}
			base, ok = j.findTree("", 40, skip)
		}
		if !ok {
			return fmt.Errorf("no encuentro árboles para empezar")
		}
		if _, err := j.fell(base, ""); err != nil {
			skip[base] = true
			j.Logf("árbol en %v: %v", base, err)
		}
		if err := j.Collect(10); err != nil {
			return err
		}
	}
	if have() < n {
		return fmt.Errorf("solo junté %d troncos de %d", have(), n)
	}
	return nil
}

// craftPickaxe hace un pico de material ("planks": de madera; si no
// "cobblestone"/la piedra que lleve, "iron_ingot", "diamond") en la mesa.
func (j *Job) craftPickaxe(material string) error {
	t, err := j.tablePos()
	if err != nil {
		return err
	}
	if err := j.EnsureSticks(2); err != nil {
		return err
	}
	mat, pick := material, ""
	switch material {
	case "planks":
		pick = "wooden_pickaxe"
		if mat, err = j.EnsureSamePlanks(3); err != nil {
			return fmt.Errorf("pico de madera: %w", err)
		}
	case "stone":
		mat, pick = j.most(stoneMaterial), "stone_pickaxe"
	case "iron_ingot":
		pick = "iron_pickaxe"
	case "diamond":
		pick = "diamond_pickaxe"
	}
	if mat == "" || j.Count(mat) < 3 {
		return fmt.Errorf("%s: faltan materiales (3 de %s)", pick, material)
	}
	before := j.Count(pick)
	if err := j.craftOnTable(t, []string{mat, mat, mat, "", "stick", "", "", "stick", ""}, 3, 1); err != nil {
		return fmt.Errorf("%s: %w", pick, err)
	}
	if j.Count(pick) <= before {
		return fmt.Errorf("%s: la mesa no lo dio", pick)
	}
	j.Logf("hice un %s", pick)
	return nil
}

// stoneMat son los adoquines (de la piedra que más lleva).
func (j *Job) stoneMat() int { return j.Count(j.most(stoneMaterial)) }

// ensureStonePickaxe: madera, pico de madera, piedra y pico de piedra (lo que
// falte), con extra adoquines de más (para un horno).
func (j *Job) ensureStonePickaxe(kind string, extra int) error {
	need := extra
	if j.pickTier() < tierStone {
		need += 3
	}
	if j.pickTier() >= tierStone && j.stoneMat() >= need {
		return nil
	}
	// madera para todo: mesa, picos, palos, combustible y los cofres de la base
	if err := j.ensureLogs(10, kind); err != nil {
		return err
	}
	if j.pickTier() == 0 && j.stoneMat() < need {
		j.Status("haciendo un pico de madera")
		if err := j.craftPickaxe("planks"); err != nil {
			return err
		}
	}
	j.ensureSword() // lo primero con la madera: algo con que defenderse
	if j.stoneMat() < need {
		// bajando en escalera desde aquí (bajo la tierra todo es piedra), no a por
		// la piedra que se vea (en una cueva o un barranco: sin donde pisar)
		if err := j.MineAt("piedra", stoneOre, j.stoneMat, need, j.Feet().Y-6, 6); err != nil {
			if j.stoneMat() < 3 {
				_ = j.Surface()
				return err
			}
			j.Logf("piedra: %v", err)
		}
		if err := j.Surface(); err != nil {
			j.Logf("%v", err)
		}
	}
	if j.pickTier() < tierStone {
		j.Status("haciendo un pico de piedra")
		if err := j.craftPickaxe("stone"); err != nil {
			return err
		}
	}
	j.ensureSword()
	return nil
}

// ensureSword hace una espada si no lleva una tan buena como lo que tiene a
// mano (de piedra si hay adoquines, si no de madera): sin arma, cualquier
// mob es un peligro (en difícil dos zombis matan a puñetazos de diferencia).
func (j *Job) EnsureSword() { j.ensureSword() }

func (j *Job) ensureSword() {
	have := 0
	for i, t := range tiers {
		if j.Count(t+"_sword") > 0 {
			have = len(tiers) - i
			break
		}
	}
	mat, tier, sword := "", 0, ""
	switch {
	case j.most(stoneMaterial) != "" && j.Count(j.most(stoneMaterial)) >= 2:
		mat, tier, sword = j.most(stoneMaterial), tierStone, "stone_sword"
	default:
		tier, sword = tierWood, "wooden_sword"
	}
	if have >= tier {
		return
	}
	j.Status("haciendo una espada")
	// la mesa primero (se hace con tablones), luego palo y tablones
	prepare := func() error {
		if err := j.EnsureSticks(1); err != nil {
			return err
		}
		if mat == "" {
			p, err := j.EnsureSamePlanks(2)
			if err != nil {
				return err
			}
			mat = p
		}
		return nil
	}
	if _, err := j.tablePos(); err != nil {
		j.Logf("espada: %v", err)
		return
	}
	if err := prepare(); err != nil {
		j.Logf("espada: %v", err)
		return
	}
	if err := j.CraftAtTable([]string{mat, mat, "stick"}, 1, 1, nil); err != nil {
		j.Logf("espada: %v", err)
		return
	}
	if j.Count(sword) > 0 {
		j.Logf("hice una %s (para defenderme)", sword)
	} else {
		j.Logf("espada: la mesa no dio la %s", sword)
	}
}

// ensureIronIngots consigue n lingotes de hierro: mina el hierro que falte
// (con pico de piedra) y lo funde (con horno: el de cerca o uno nuevo).
func (j *Job) ensureIronIngots(n int, kind string) error {
	if j.Count("iron_ingot") >= n {
		return nil
	}
	_, furnaceNear := j.near(24, func(b string) bool { return b == "furnace" })
	extra := 0
	if !furnaceNear && j.Count("furnace") == 0 {
		extra = 8
	}
	if err := j.ensureStonePickaxe(kind, extra); err != nil {
		return err
	}
	have := func() int { return j.Count("iron_ingot") + j.Count("raw_iron") }
	if have() < n {
		j.Logf("buscando hierro (%d)", n-have())
		if err := j.MineAt("hierro", ironOre, have, n, min(ironLevel, j.Feet().Y-8), 20); err != nil {
			_ = j.Surface()
			return fmt.Errorf("hierro: %w", err)
		}
		if err := j.Surface(); err != nil {
			j.Logf("%v", err)
		}
	}
	if need := n - j.Count("iron_ingot"); need > 0 {
		j.Status("fundiendo hierro")
		if err := j.Smelt("raw_iron", min(need, j.Count("raw_iron"))); err != nil {
			return fmt.Errorf("fundir hierro: %w", err)
		}
	}
	if j.Count("iron_ingot") < n {
		return fmt.Errorf("solo tengo %d lingotes de hierro de %d", j.Count("iron_ingot"), n)
	}
	return nil
}

// EnsureIronAxe sigue la progresión hasta tener un hacha de hierro.
func (j *Job) EnsureIronAxe(kind string) error {
	if j.axeTier() >= tierIron {
		return nil
	}
	j.Logf("objetivo: hacha de hierro (pico de piedra, hierro, horno)")
	if err := j.ensureIronIngots(3, kind); err != nil {
		return err
	}
	j.Status("haciendo el hacha de hierro")
	axe, err := j.CraftAxe()
	if err != nil {
		return err
	}
	if axe != "iron_axe" {
		return fmt.Errorf("salió un %s, no el de hierro", axe)
	}
	j.Logf("¡hacha de hierro lista!")
	return nil
}

// EnsureIronPickaxe sigue la progresión hasta tener un pico de hierro (o mejor).
func (j *Job) EnsureIronPickaxe() error {
	if j.pickTier() >= tierIron {
		return nil
	}
	j.Logf("objetivo: pico de hierro")
	if err := j.ensureIronIngots(3, ""); err != nil {
		return err
	}
	return j.craftPickaxe("iron_ingot")
}

// diamondLevel es a qué altura se buscan diamantes: muchos, pero por encima
// del nivel de los lagos de lava de abajo del todo (-55).
const diamondLevel = -53

func diamondOre(n string) bool { return n == "diamond_ore" || n == "deepslate_diamond_ore" }

// EnsureDiamondPickaxe sigue la progresión hasta tener un pico de diamante
// (o de netherita): pico de hierro y 3 diamantes, cavando hasta su altura.
func (j *Job) EnsureDiamondPickaxe() error {
	if j.pickTier() >= tierDiamond {
		return nil
	}
	if err := j.EnsureIronPickaxe(); err != nil {
		return err
	}
	if j.Count("diamond") < 3 {
		j.Logf("objetivo: 3 diamantes (a la altura %d)", diamondLevel)
		have := func() int { return j.Count("diamond") }
		err := j.MineAt("diamantes", diamondOre, have, 3, diamondLevel, 40)
		if serr := j.Surface(); serr != nil {
			j.Logf("%v", serr)
		}
		if err != nil {
			return fmt.Errorf("diamantes: %w", err)
		}
	}
	return j.craftPickaxe("diamond")
}

// best es la herramienta de mejor material que lleva de ese tipo ("axe"), "" ninguna.
func (j *Job) best(kind string) string {
	items := j.Items()
	for _, t := range tiers {
		if items[t+"_"+kind] > 0 {
			return t + "_" + kind
		}
	}
	return ""
}
