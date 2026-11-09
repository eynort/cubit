// Package schematic lee construcciones guardadas (.litematic de Litematica,
// .schem de WorldEdit/Sponge, .nbt de bloques de estructura) y las coloca en
// el mundo igual que lo hace Litematica: un origen, una rotación y un espejo.
//
// La colocación la decide el jugador que tiene el schematic en su pantalla
// (Litematica es del cliente: el bot no la ve). Por eso la colocación son
// exactamente los tres datos del menú "Placement settings" de Litematica:
//
//   - Origin: la esquina 0,0,0 del schematic, en el mundo;
//   - Rotation: none, CW 90, CW 180 (180), CCW 90;
//   - Mirror: none, left-right, front-back.
//
// Las cuentas son las de Litematica (PositionUtils.getTransformedBlockPos y
// BlockState.mirror(..).rotate(..)): primero el espejo, luego la rotación,
// las dos alrededor del origen.
package schematic

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mj41/go-mc26/nbt"
)

// Pos es una posición de bloque.
type Pos struct{ X, Y, Z int }

func (p Pos) Add(q Pos) Pos { return Pos{p.X + q.X, p.Y + q.Y, p.Z + q.Z} }

func (p Pos) String() string { return fmt.Sprintf("%d %d %d", p.X, p.Y, p.Z) }

// State es un estado de bloque: el nombre sin "minecraft:" y sus propiedades.
type State struct {
	Name  string
	Props map[string]string
}

// String es como lo escribe el juego: "oak_stairs[facing=north,half=bottom]".
func (s State) String() string {
	if len(s.Props) == 0 {
		return s.Name
	}
	keys := make([]string, 0, len(s.Props))
	for k := range s.Props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(s.Name)
	b.WriteByte('[')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k + "=" + s.Props[k])
	}
	b.WriteByte(']')
	return b.String()
}

// IsAir: aire (cualquiera).
func (s State) IsAir() bool {
	return s.Name == "air" || s.Name == "cave_air" || s.Name == "void_air"
}

// Region es un trozo del schematic (Litematica puede tener varios): un
// paralelepípedo de bloques con su esquina mínima en coordenadas del schematic.
type Region struct {
	Name string
	Min  Pos // la esquina mínima, relativa al origen del schematic
	Size Pos // siempre positivo
	// Cells: un índice de Palette por celda (x + z*Size.X + y*Size.X*Size.Z);
	// -1: la celda no es del schematic (structure_void: se deja lo que haya).
	Cells []int32
}

// Schematic es una construcción leída de un archivo.
type Schematic struct {
	Name    string
	Author  string
	Palette []State
	Regions []Region
}

// Load lee un schematic (.litematic, .schem, .schematic o .nbt).
func Load(path string) (*Schematic, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data, err := gunzip(raw)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	var s *Schematic
	switch strings.ToLower(filepath.Ext(path)) {
	case ".litematic":
		s, err = readLitematic(data)
	case ".schem", ".schematic":
		s, err = readSponge(data)
	case ".nbt":
		s, err = readStructure(data)
	default:
		// sin extensión conocida: se prueba cada formato
		if s, err = readLitematic(data); err != nil {
			if s, err = readSponge(data); err != nil {
				s, err = readStructure(data)
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if s.Name == "" {
		s.Name = name
	}
	return s, nil
}

func gunzip(raw []byte) ([]byte, error) {
	if len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
		return raw, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

// ---- Litematica (.litematic) ----

type litVec struct {
	X int32 `nbt:"x"`
	Y int32 `nbt:"y"`
	Z int32 `nbt:"z"`
}

type litPaletteEntry struct {
	Name       string            `nbt:"Name"`
	Properties map[string]string `nbt:"Properties"`
}

type litRegion struct {
	Position          litVec            `nbt:"Position"`
	Size              litVec            `nbt:"Size"`
	BlockStatePalette []litPaletteEntry `nbt:"BlockStatePalette"`
	BlockStates       []int64           `nbt:"BlockStates"`
}

type litFile struct {
	Version  int32 `nbt:"Version"`
	Metadata struct {
		Name   string `nbt:"Name"`
		Author string `nbt:"Author"`
	} `nbt:"Metadata"`
	Regions map[string]litRegion `nbt:"Regions"`
}

func readLitematic(data []byte) (*Schematic, error) {
	var f litFile
	if err := nbt.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if len(f.Regions) == 0 {
		return nil, errors.New("no es un .litematic (sin regiones)")
	}
	s := &Schematic{Name: f.Metadata.Name, Author: f.Metadata.Author}
	pal := newPaletteBuilder(s)
	names := make([]string, 0, len(f.Regions))
	for n := range f.Regions {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		r := f.Regions[n]
		pos := Pos{int(r.Position.X), int(r.Position.Y), int(r.Position.Z)}
		size := Pos{int(r.Size.X), int(r.Size.Y), int(r.Size.Z)}
		// Litematica: un tamaño negativo crece hacia atrás desde Position
		// (getRelativeEndPositionFromAreaSize + getMinCorner)
		min := pos
		abs := size
		if size.X < 0 {
			min.X, abs.X = pos.X+size.X+1, -size.X
		}
		if size.Y < 0 {
			min.Y, abs.Y = pos.Y+size.Y+1, -size.Y
		}
		if size.Z < 0 {
			min.Z, abs.Z = pos.Z+size.Z+1, -size.Z
		}
		vol := abs.X * abs.Y * abs.Z
		if vol <= 0 {
			continue
		}
		if vol > maxCells {
			return nil, fmt.Errorf("la región %q es demasiado grande (%d bloques)", n, vol)
		}
		if len(r.BlockStatePalette) == 0 {
			return nil, fmt.Errorf("la región %q no tiene paleta", n)
		}
		local := make([]int32, len(r.BlockStatePalette))
		for i, e := range r.BlockStatePalette {
			local[i] = pal.add(e.Name, e.Properties)
		}
		bits := 2
		for (1 << bits) < len(r.BlockStatePalette) {
			bits++
		}
		if need := (int64(vol)*int64(bits) + 63) / 64; int64(len(r.BlockStates)) < need {
			return nil, fmt.Errorf("la región %q está incompleta (%d de %d longs)", n, len(r.BlockStates), need)
		}
		mask := uint64(1)<<bits - 1
		cells := make([]int32, vol)
		for i := 0; i < vol; i++ {
			start := int64(i) * int64(bits)
			si, ei := start>>6, (int64(i+1)*int64(bits)-1)>>6
			off := uint(start & 63)
			v := uint64(r.BlockStates[si]) >> off
			if si != ei {
				v |= uint64(r.BlockStates[ei]) << (64 - off)
			}
			idx := int(v & mask)
			if idx >= len(local) {
				return nil, fmt.Errorf("la región %q tiene un índice de paleta malo (%d)", n, idx)
			}
			cells[i] = local[idx]
		}
		s.Regions = append(s.Regions, Region{Name: n, Min: min, Size: abs, Cells: cells})
	}
	return s, nil
}

// maxCells es el tamaño máximo que se acepta (para no reventar la memoria del teléfono).
const maxCells = 8 << 20

// paletteBuilder junta las paletas de las regiones en una sola (sin repetir).
type paletteBuilder struct {
	s     *Schematic
	index map[string]int32
}

func newPaletteBuilder(s *Schematic) *paletteBuilder {
	return &paletteBuilder{s: s, index: map[string]int32{}}
}

// add devuelve el índice del estado (-1: structure_void, la celda no cuenta).
func (p *paletteBuilder) add(name string, props map[string]string) int32 {
	name = strings.TrimPrefix(name, "minecraft:")
	if name == "structure_void" {
		return -1
	}
	st := State{Name: name, Props: props}
	if len(st.Props) == 0 {
		st.Props = nil
	}
	key := st.String()
	if i, ok := p.index[key]; ok {
		return i
	}
	i := int32(len(p.s.Palette))
	p.s.Palette = append(p.s.Palette, st)
	p.index[key] = i
	return i
}

// parseStateText lee "minecraft:oak_stairs[facing=north,half=bottom]".
func parseStateText(text string) (string, map[string]string) {
	name, rest, ok := strings.Cut(text, "[")
	if !ok {
		return name, nil
	}
	props := map[string]string{}
	for _, kv := range strings.Split(strings.TrimSuffix(rest, "]"), ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			props[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return name, props
}

// ---- WorldEdit / Sponge (.schem) ----

type spongeBlocks struct {
	Palette map[string]int32 `nbt:"Palette"`
	Data    []byte           `nbt:"Data"`
}

type spongeFile struct {
	Version int32 `nbt:"Version"`
	Width   int16 `nbt:"Width"`
	Height  int16 `nbt:"Height"`
	Length  int16 `nbt:"Length"`
	// v1 y v2
	Palette   map[string]int32 `nbt:"Palette"`
	BlockData []byte           `nbt:"BlockData"`
	// v3
	Blocks *spongeBlocks `nbt:"Blocks"`
}

func readSponge(data []byte) (*Schematic, error) {
	var f spongeFile
	if err := nbt.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.Width == 0 && f.Blocks == nil {
		// v3: todo dentro de un compuesto "Schematic"
		var wrap struct {
			Schematic spongeFile `nbt:"Schematic"`
		}
		if err := nbt.Unmarshal(data, &wrap); err == nil {
			f = wrap.Schematic
		}
	}
	palette, blocks := f.Palette, f.BlockData
	if f.Blocks != nil {
		palette, blocks = f.Blocks.Palette, f.Blocks.Data
	}
	w, h, l := int(uint16(f.Width)), int(uint16(f.Height)), int(uint16(f.Length))
	if w == 0 || h == 0 || l == 0 || len(palette) == 0 {
		return nil, errors.New("no es un .schem (sin tamaño o sin paleta)")
	}
	vol := w * h * l
	if vol > maxCells {
		return nil, fmt.Errorf("demasiado grande (%d bloques)", vol)
	}
	s := &Schematic{}
	pal := newPaletteBuilder(s)
	byID := map[int32]int32{}
	for text, id := range palette {
		name, props := parseStateText(text)
		byID[id] = pal.add(name, props)
	}
	cells := make([]int32, vol)
	// los índices van en varint, en orden x, luego z, luego y (como el nuestro)
	i, n := 0, 0
	for i < vol {
		v, shift := 0, 0
		for {
			if n >= len(blocks) {
				return nil, errors.New("los datos de bloques se acaban antes de tiempo")
			}
			b := blocks[n]
			n++
			v |= int(b&0x7f) << shift
			if b&0x80 == 0 {
				break
			}
			shift += 7
		}
		idx, ok := byID[int32(v)]
		if !ok {
			return nil, fmt.Errorf("índice de paleta %d desconocido", v)
		}
		cells[i] = idx
		i++
	}
	s.Regions = []Region{{Name: "main", Size: Pos{w, h, l}, Cells: cells}}
	return s, nil
}

// ---- Bloque de estructura (.nbt) ----

type structFile struct {
	Size    []int32 `nbt:"size,list"`
	Palette []struct {
		Name       string            `nbt:"Name"`
		Properties map[string]string `nbt:"Properties"`
	} `nbt:"palette"`
	Blocks []struct {
		Pos   []int32 `nbt:"pos,list"`
		State int32   `nbt:"state"`
	} `nbt:"blocks"`
}

func readStructure(data []byte) (*Schematic, error) {
	var f structFile
	if err := nbt.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if len(f.Size) != 3 || len(f.Palette) == 0 {
		return nil, errors.New("no es una estructura .nbt (sin tamaño o sin paleta)")
	}
	size := Pos{int(f.Size[0]), int(f.Size[1]), int(f.Size[2])}
	vol := size.X * size.Y * size.Z
	if vol <= 0 || vol > maxCells {
		return nil, fmt.Errorf("tamaño raro (%v)", size)
	}
	s := &Schematic{}
	pal := newPaletteBuilder(s)
	local := make([]int32, len(f.Palette))
	for i, e := range f.Palette {
		local[i] = pal.add(e.Name, e.Properties)
	}
	cells := make([]int32, vol)
	for i := range cells {
		cells[i] = -1 // lo que no está en la lista no es de la estructura
	}
	for _, b := range f.Blocks {
		if len(b.Pos) != 3 || int(b.State) >= len(local) || b.State < 0 {
			continue
		}
		x, y, z := int(b.Pos[0]), int(b.Pos[1]), int(b.Pos[2])
		if x < 0 || y < 0 || z < 0 || x >= size.X || y >= size.Y || z >= size.Z {
			continue
		}
		cells[x+z*size.X+y*size.X*size.Z] = local[b.State]
	}
	s.Regions = []Region{{Name: "main", Size: size, Cells: cells}}
	return s, nil
}

// Blocks son los bloques que no son aire (para contar materiales sin colocar).
func (s *Schematic) Blocks() int {
	n := 0
	for _, r := range s.Regions {
		for _, c := range r.Cells {
			if c >= 0 && !s.Palette[c].IsAir() {
				n++
			}
		}
	}
	return n
}

// Size es el tamaño de la caja que lo encierra (sin colocar).
func (s *Schematic) Size() Pos {
	min, max, ok := s.bounds()
	if !ok {
		return Pos{}
	}
	return Pos{max.X - min.X + 1, max.Y - min.Y + 1, max.Z - min.Z + 1}
}

func (s *Schematic) bounds() (min, max Pos, ok bool) {
	for i, r := range s.Regions {
		lo, hi := r.Min, r.Min.Add(Pos{r.Size.X - 1, r.Size.Y - 1, r.Size.Z - 1})
		if i == 0 {
			min, max = lo, hi
			ok = true
			continue
		}
		min = Pos{mn(min.X, lo.X), mn(min.Y, lo.Y), mn(min.Z, lo.Z)}
		max = Pos{mx(max.X, hi.X), mx(max.Y, hi.Y), mx(max.Z, hi.Z)}
	}
	return
}

func mn(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func mx(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// atoi para propiedades numéricas (0 si no lo es).
func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
