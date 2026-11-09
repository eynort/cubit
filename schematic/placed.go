package schematic

import "fmt"

// Placed es un schematic ya colocado: cada celda del mundo dentro de su caja
// sabe qué bloque lleva (o que no es suya).
type Placed struct {
	Placement Placement
	Palette   []State // ya girados y reflejados
	Min, Max  Pos     // la caja en el mundo (los dos extremos incluidos)
	size      Pos
	cells     []int32 // índice de Palette; -1: no es del schematic
}

// Place coloca el schematic.
func (s *Schematic) Place(p Placement) (*Placed, error) {
	lo, hi, ok := s.bounds()
	if !ok {
		return nil, fmt.Errorf("el schematic está vacío")
	}
	// la caja en el mundo: las 8 esquinas transformadas
	first := true
	var min, max Pos
	for _, x := range []int{lo.X, hi.X} {
		for _, y := range []int{lo.Y, hi.Y} {
			for _, z := range []int{lo.Z, hi.Z} {
				w := p.World(Pos{x, y, z})
				if first {
					min, max, first = w, w, false
					continue
				}
				min = Pos{mn(min.X, w.X), mn(min.Y, w.Y), mn(min.Z, w.Z)}
				max = Pos{mx(max.X, w.X), mx(max.Y, w.Y), mx(max.Z, w.Z)}
			}
		}
	}
	size := Pos{max.X - min.X + 1, max.Y - min.Y + 1, max.Z - min.Z + 1}
	if size.X*size.Y*size.Z > maxCells {
		return nil, fmt.Errorf("demasiado grande (%v)", size)
	}
	out := &Placed{Placement: p, Min: min, Max: max, size: size, cells: make([]int32, size.X*size.Y*size.Z)}
	for i := range out.cells {
		out.cells[i] = -1
	}
	out.Palette = make([]State, len(s.Palette))
	for i, st := range s.Palette {
		out.Palette[i] = p.TransformState(st)
	}
	for _, r := range s.Regions {
		for y := 0; y < r.Size.Y; y++ {
			for z := 0; z < r.Size.Z; z++ {
				for x := 0; x < r.Size.X; x++ {
					c := r.Cells[x+z*r.Size.X+y*r.Size.X*r.Size.Z]
					if c < 0 {
						continue
					}
					w := p.World(r.Min.Add(Pos{x, y, z}))
					out.cells[out.index(w)] = c
				}
			}
		}
	}
	return out, nil
}

func (p *Placed) index(w Pos) int {
	return (w.X - p.Min.X) + (w.Z-p.Min.Z)*p.size.X + (w.Y-p.Min.Y)*p.size.X*p.size.Z
}

// Inside: w está dentro de la caja.
func (p *Placed) Inside(w Pos) bool {
	return w.X >= p.Min.X && w.Y >= p.Min.Y && w.Z >= p.Min.Z && w.X <= p.Max.X && w.Y <= p.Max.Y && w.Z <= p.Max.Z
}

// At es el bloque que va en w (ok false: no es del schematic).
func (p *Placed) At(w Pos) (State, bool) {
	i := p.Index(w)
	if i < 0 {
		return State{}, false
	}
	return p.Palette[i], true
}

// Index es el índice de paleta de w (-1: no es del schematic).
func (p *Placed) Index(w Pos) int32 {
	if !p.Inside(w) {
		return -1
	}
	return p.cells[p.index(w)]
}

// Each recorre las celdas del schematic, de abajo arriba.
func (p *Placed) Each(f func(w Pos, palette int32)) {
	for y := 0; y < p.size.Y; y++ {
		for z := 0; z < p.size.Z; z++ {
			for x := 0; x < p.size.X; x++ {
				c := p.cells[x+z*p.size.X+y*p.size.X*p.size.Z]
				if c >= 0 {
					f(Pos{p.Min.X + x, p.Min.Y + y, p.Min.Z + z}, c)
				}
			}
		}
	}
}

// Size es el tamaño de la caja en el mundo.
func (p *Placed) Size() Pos { return p.size }
