package schematic

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/mj41/go-mc26/nbt"
)

// pack empaqueta índices como LitematicaBitArray (los valores pueden cruzar de un long al siguiente).
func pack(vals []int, bits int) []int64 {
	out := make([]int64, (len(vals)*bits+63)/64)
	for i, v := range vals {
		start := i * bits
		si, off := start>>6, uint(start&63)
		out[si] |= int64(uint64(v) << off)
		if off+uint(bits) > 64 {
			out[si+1] |= int64(uint64(v) >> (64 - off))
		}
	}
	return out
}

type testLitRegion struct {
	Position          litVec            `nbt:"Position"`
	Size              litVec            `nbt:"Size"`
	BlockStatePalette []litPaletteEntry `nbt:"BlockStatePalette"`
	BlockStates       []int64           `nbt:"BlockStates"`
}

type testLit struct {
	Version  int32 `nbt:"Version"`
	Metadata struct {
		Name string `nbt:"Name"`
	} `nbt:"Metadata"`
	Regions map[string]testLitRegion `nbt:"Regions"`
}

func writeGz(t *testing.T, path string, v any) {
	t.Helper()
	data, err := nbt.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write(data)
	zw.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLitematic: un .litematic con dos regiones (una con tamaño negativo, que
// crece hacia atrás desde su posición) y una paleta de 5 estados (3 bits por
// celda: los valores cruzan de un long a otro).
func TestLitematic(t *testing.T) {
	pal := []litPaletteEntry{
		{Name: "minecraft:air"},
		{Name: "minecraft:stone"},
		{Name: "minecraft:oak_stairs", Properties: map[string]string{"facing": "north", "half": "bottom", "shape": "straight", "waterlogged": "false"}},
		{Name: "minecraft:oak_log", Properties: map[string]string{"axis": "x"}},
		{Name: "minecraft:glass"},
	}
	// región A: 5×3×2 en (0,0,0); el índice es x + z*5 + y*10
	sx, sy, sz := 5, 3, 2
	vals := make([]int, sx*sy*sz)
	for i := range vals {
		vals[i] = i % 5
	}
	// región B: tamaño (2,1,-3) en (10,0,10): ocupa z 8..10
	valsB := []int{1, 2, 3, 4, 1, 2}
	lit := testLit{Version: 6, Regions: map[string]testLitRegion{
		"A": {Size: litVec{int32(sx), int32(sy), int32(sz)}, BlockStatePalette: pal, BlockStates: pack(vals, 3)},
		"B": {Position: litVec{10, 0, 10}, Size: litVec{2, 1, -3}, BlockStatePalette: pal, BlockStates: pack(valsB, 3)},
	}}
	lit.Metadata.Name = "prueba"
	path := filepath.Join(t.TempDir(), "casa.litematic")
	writeGz(t, path, lit)
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "prueba" || len(s.Regions) != 2 {
		t.Fatalf("nombre %q, %d regiones", s.Name, len(s.Regions))
	}
	p, err := s.Place(Placement{Origin: Pos{100, 64, 200}})
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < sy; y++ {
		for z := 0; z < sz; z++ {
			for x := 0; x < sx; x++ {
				want := pal[vals[x+z*sx+y*sx*sz]].Name[len("minecraft:"):]
				got, ok := p.At(Pos{100 + x, 64 + y, 200 + z})
				if !ok || got.Name != want {
					t.Fatalf("A (%d,%d,%d): %v, quiero %s", x, y, z, got, want)
				}
			}
		}
	}
	// B: mínimo en z = 10-3+1 = 8
	for z := 0; z < 3; z++ {
		for x := 0; x < 2; x++ {
			want := pal[valsB[x+z*2]].Name[len("minecraft:"):]
			got, ok := p.At(Pos{110 + x, 64, 208 + z})
			if !ok || got.Name != want {
				t.Fatalf("B (%d,%d): %v, quiero %s", x, z, got, want)
			}
		}
	}
	if p.Min != (Pos{100, 64, 200}) || p.Max != (Pos{111, 66, 210}) {
		t.Fatalf("caja %v → %v", p.Min, p.Max)
	}
	// entre las regiones: no es del schematic
	if _, ok := p.At(Pos{106, 64, 205}); ok {
		t.Fatal("una celda fuera de las regiones cuenta")
	}
}

// TestTransform: las cuentas de Litematica (espejo y luego rotación) para
// posiciones y estados, comprobadas a mano.
func TestTransform(t *testing.T) {
	cases := []struct {
		rot  Rotation
		mir  Mirror
		in   Pos
		want Pos
		face string // facing north girado
	}{
		{0, MirrorNone, Pos{1, 2, 3}, Pos{1, 2, 3}, "north"},
		{1, MirrorNone, Pos{1, 0, 0}, Pos{0, 0, 1}, "east"}, // CW: el este pasa al sur, el norte al este
		{1, MirrorNone, Pos{0, 0, -1}, Pos{1, 0, 0}, "east"},
		{2, MirrorNone, Pos{1, 0, 2}, Pos{-1, 0, -2}, "south"},
		{3, MirrorNone, Pos{1, 0, 0}, Pos{0, 0, -1}, "west"},
		{0, MirrorLeftRight, Pos{1, 0, 2}, Pos{1, 0, -2}, "south"},
		{0, MirrorFrontBack, Pos{1, 0, 2}, Pos{-1, 0, 2}, "north"},
		{1, MirrorLeftRight, Pos{1, 0, 2}, Pos{2, 0, 1}, "west"}, // z→-2, luego CW: (2, 1)
	}
	for _, c := range cases {
		pl := Placement{Rotation: c.rot, Mirror: c.mir}
		if got := pl.Transform(c.in); got != c.want {
			t.Errorf("rot %v mir %v: %v → %v, quiero %v", c.rot, c.mir, c.in, got, c.want)
		}
		st := pl.TransformState(State{Name: "oak_stairs", Props: map[string]string{"facing": "north"}})
		if st.Props["facing"] != c.face {
			t.Errorf("rot %v mir %v: facing north → %s, quiero %s", c.rot, c.mir, st.Props["facing"], c.face)
		}
	}
	// un bloque al norte del origen con la escalera mirando al norte: tras girar, los dos al este
	pl := Placement{Rotation: 1}
	if pl.Transform(Pos{0, 0, -1}) != (Pos{1, 0, 0}) {
		t.Error("la posición y el facing no giran igual")
	}
	// ejes, rotación de 16 y lados
	st := Placement{Rotation: 1}.TransformState(State{Name: "oak_log", Props: map[string]string{"axis": "x"}})
	if st.Props["axis"] != "z" {
		t.Errorf("axis x girado: %s", st.Props["axis"])
	}
	st = Placement{Rotation: 1}.TransformState(State{Name: "oak_sign", Props: map[string]string{"rotation": "14"}})
	if st.Props["rotation"] != "2" {
		t.Errorf("rotation 14 + CW: %s", st.Props["rotation"])
	}
	st = Placement{Mirror: MirrorLeftRight}.TransformState(State{Name: "oak_sign", Props: map[string]string{"rotation": "3"}})
	if st.Props["rotation"] != "13" {
		t.Errorf("rotation 3 espejo LR: %s", st.Props["rotation"])
	}
	st = Placement{Rotation: 1}.TransformState(State{Name: "oak_fence", Props: map[string]string{"north": "true", "east": "false", "south": "false", "west": "false"}})
	if st.Props["east"] != "true" || st.Props["north"] != "false" {
		t.Errorf("valla girada: %v", st.Props)
	}
	st = Placement{Mirror: MirrorFrontBack}.TransformState(State{Name: "oak_door", Props: map[string]string{"facing": "east", "hinge": "left"}})
	if st.Props["facing"] != "west" || st.Props["hinge"] != "right" {
		t.Errorf("puerta en espejo: %v", st.Props)
	}
}

// TestPlacedRotatedBox: la caja de un schematic girado empieza en el origen y
// se extiende hacia donde lleva la rotación.
func TestPlacedRotatedBox(t *testing.T) {
	cells := make([]int32, 4*1*2)
	s := &Schematic{Palette: []State{{Name: "stone"}}, Regions: []Region{{Size: Pos{4, 1, 2}, Cells: cells}}}
	p, err := s.Place(Placement{Origin: Pos{0, 70, 0}, Rotation: 1})
	if err != nil {
		t.Fatal(err)
	}
	// x 0..3, z 0..1 → CW: x' = -z (−1..0), z' = x (0..3)
	if p.Min != (Pos{-1, 70, 0}) || p.Max != (Pos{0, 70, 3}) {
		t.Fatalf("caja %v → %v", p.Min, p.Max)
	}
}

func TestItems(t *testing.T) {
	cases := []struct {
		st   State
		item string
		n    int
		ok   bool
	}{
		{State{Name: "stone"}, "stone", 1, true},
		{State{Name: "wall_torch", Props: map[string]string{"facing": "east"}}, "torch", 1, true},
		{State{Name: "oak_wall_sign", Props: map[string]string{"facing": "east"}}, "oak_sign", 1, true},
		{State{Name: "oak_door", Props: map[string]string{"half": "upper"}}, "", 0, true},
		{State{Name: "oak_door", Props: map[string]string{"half": "lower"}}, "oak_door", 1, true},
		{State{Name: "red_bed", Props: map[string]string{"part": "head"}}, "", 0, true},
		{State{Name: "oak_slab", Props: map[string]string{"type": "double"}}, "oak_slab", 2, true},
		{State{Name: "redstone_wire"}, "redstone", 1, true},
		{State{Name: "candle", Props: map[string]string{"candles": "3"}}, "candle", 3, true},
		{State{Name: "water"}, "", 0, false},
		{State{Name: "potted_poppy"}, "flower_pot", 1, true},
	}
	for _, c := range cases {
		item, n, ok := Item(c.st)
		if item != c.item || n != c.n || ok != c.ok {
			t.Errorf("%v: %s %d %v, quiero %s %d %v", c.st, item, n, ok, c.item, c.n, c.ok)
		}
	}
	want := State{Name: "oak_stairs", Props: map[string]string{"facing": "north", "half": "bottom", "shape": "straight", "waterlogged": "false"}}
	have := State{Name: "oak_stairs", Props: map[string]string{"facing": "north", "half": "bottom", "shape": "inner_left", "waterlogged": "true"}}
	if !Matches(want, have) {
		t.Error("la forma y el agua no deberían contar")
	}
	have.Props["facing"] = "south"
	if Matches(want, have) {
		t.Error("el facing sí cuenta")
	}
}

func TestParseRotation(t *testing.T) {
	for in, want := range map[string]Rotation{"none": 0, "90": 1, "cw_90": 1, "CLOCKWISE_90": 1, "180": 2, "270": 3, "ccw_90": 3, "-90": 3, "COUNTERCLOCKWISE_90": 3} {
		got, err := ParseRotation(in)
		if err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	for in, want := range map[string]Mirror{"none": 0, "lr": 1, "LEFT_RIGHT": 1, "front_back": 2, "fb": 2} {
		got, err := ParseMirror(in)
		if err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
}

func varints(vals []int) []byte {
	var out []byte
	for _, v := range vals {
		for v >= 0x80 {
			out = append(out, byte(v&0x7f|0x80))
			v >>= 7
		}
		out = append(out, byte(v))
	}
	return out
}

// TestSponge: .schem de WorldEdit, v2 (todo en la raíz) y v3 (dentro de
// "Schematic", los bloques en "Blocks"), con un índice de paleta de 2 bytes.
func TestSponge(t *testing.T) {
	// 2×1×2: x + z*2
	palette := map[string]int32{"minecraft:air": 0, "minecraft:stone": 1, "minecraft:oak_log[axis=z]": 200}
	data := varints([]int{1, 200, 0, 1})
	type v2 struct {
		Version   int32            `nbt:"Version"`
		Width     int16            `nbt:"Width"`
		Height    int16            `nbt:"Height"`
		Length    int16            `nbt:"Length"`
		Palette   map[string]int32 `nbt:"Palette"`
		BlockData []byte           `nbt:"BlockData"`
	}
	type blocks struct {
		Palette map[string]int32 `nbt:"Palette"`
		Data    []byte           `nbt:"Data"`
	}
	type v3inner struct {
		Version int32  `nbt:"Version"`
		Width   int16  `nbt:"Width"`
		Height  int16  `nbt:"Height"`
		Length  int16  `nbt:"Length"`
		Blocks  blocks `nbt:"Blocks"`
	}
	type v3 struct {
		Schematic v3inner `nbt:"Schematic"`
	}
	dir := t.TempDir()
	writeGz(t, filepath.Join(dir, "a.schem"), v2{2, 2, 1, 2, palette, data})
	writeGz(t, filepath.Join(dir, "b.schem"), v3{v3inner{3, 2, 1, 2, blocks{palette, data}}})
	for _, f := range []string{"a.schem", "b.schem"} {
		s, err := Load(filepath.Join(dir, f))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		p, err := s.Place(Placement{Origin: Pos{10, 70, 10}})
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, c := range []Pos{{10, 70, 10}, {11, 70, 10}, {10, 70, 11}, {11, 70, 11}} {
			st, _ := p.At(c)
			got = append(got, st.String())
		}
		want := []string{"stone", "oak_log[axis=z]", "air", "stone"}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s: %v, quiero %v", f, got, want)
				break
			}
		}
	}
}

// TestStructureNBT: el .nbt de un bloque de estructura (la lista de bloques con su posición).
func TestStructureNBT(t *testing.T) {
	type pe struct {
		Name       string            `nbt:"Name"`
		Properties map[string]string `nbt:"Properties,omitempty"`
	}
	type bl struct {
		Pos   []int32 `nbt:"pos,list"`
		State int32   `nbt:"state"`
	}
	type st struct {
		DataVersion int32   `nbt:"DataVersion"`
		Size        []int32 `nbt:"size,list"`
		Palette     []pe    `nbt:"palette"`
		Blocks      []bl    `nbt:"blocks"`
	}
	f := st{Size: []int32{2, 2, 1}, Palette: []pe{{Name: "minecraft:stone"}, {Name: "minecraft:oak_stairs", Properties: map[string]string{"facing": "east"}}},
		Blocks: []bl{{[]int32{0, 0, 0}, 0}, {[]int32{1, 1, 0}, 1}}}
	path := filepath.Join(t.TempDir(), "x.nbt")
	writeGz(t, path, f)
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Place(Placement{Origin: Pos{0, 64, 0}, Rotation: 2})
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := p.At(Pos{0, 64, 0}); !ok || a.Name != "stone" {
		t.Errorf("piedra: %v %v", a, ok)
	}
	if a, ok := p.At(Pos{-1, 65, 0}); !ok || a.Name != "oak_stairs" || a.Props["facing"] != "west" {
		t.Errorf("escalera girada 180: %v %v", a, ok)
	}
	if _, ok := p.At(Pos{-1, 64, 0}); ok {
		t.Error("una celda que no está en la lista cuenta (debería dejarse como está)")
	}
}
