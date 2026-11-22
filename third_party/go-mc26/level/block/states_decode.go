package block

import (
	"bufio"
	"encoding"
	"fmt"
	"io"
	"reflect"
)

// decodeStates reads block_states.nbt (a TAG_List of TAG_Compound, each a
// Name and, for a block with properties, a Properties compound of strings)
// straight into the blocks, without the reflective NBT decoder: that one made
// ~33 MB of garbage and took ~150 ms at every start of every program that
// imports this package. The result is the same (TestDecodeStatesMatchesNBT).
func decodeStates(r io.Reader) ([]Block, error) {
	d := stateReader{r: bufio.NewReaderSize(r, 32<<10), fields: map[reflect.Type]map[string]int{}}
	if t := d.byte(); t != 9 { // TAG_List
		return nil, fmt.Errorf("block states: root tag %d, want a list", t)
	}
	d.raw() // the root's name
	if t := d.byte(); t != 10 {
		return nil, fmt.Errorf("block states: list of %d, want compounds", t)
	}
	n := int(d.int32())
	if d.err != nil || n < 0 {
		return nil, fmt.Errorf("block states: %v", d.err)
	}
	out := make([]Block, 0, n)
	var props [][2]string
	for range n {
		name := ""
		props = props[:0]
		for {
			t := d.byte()
			if t == 0 || d.err != nil { // TAG_End
				break
			}
			key := d.string()
			switch {
			case t == 8 && key == "Name":
				name = d.string()
			case t == 10 && key == "Properties":
				for {
					pt := d.byte()
					if pt == 0 || d.err != nil {
						break
					}
					if pt != 8 {
						return nil, fmt.Errorf("block states: property of tag %d", pt)
					}
					k := d.string()
					props = append(props, [2]string{k, d.string()})
				}
			default:
				return nil, fmt.Errorf("block states: unexpected tag %d %q", t, key)
			}
		}
		if d.err != nil {
			return nil, fmt.Errorf("block states: %w", d.err)
		}
		b, err := d.block(name, props)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

type stateReader struct {
	r      *bufio.Reader
	err    error
	buf    []byte
	tmp    []byte
	fields map[reflect.Type]map[string]int // per block type: the field of each property
	names  map[string]string               // interned strings (the same names repeat)
}

// block is the block called name with these properties set, as State.Block
// makes it (each property through its UnmarshalText).
func (d *stateReader) block(name string, props [][2]string) (Block, error) {
	base, ok := FromID[name]
	if !ok {
		return nil, UnknownBlockErr{name}
	}
	if len(props) == 0 {
		return base, nil
	}
	t := reflect.TypeOf(base)
	idx, ok := d.fields[t]
	if !ok {
		idx = make(map[string]int, t.NumField())
		for i := range t.NumField() {
			if tag := t.Field(i).Tag.Get("nbt"); tag != "" {
				idx[tag] = i
			}
		}
		d.fields[t] = idx
	}
	v := reflect.New(t).Elem()
	for _, p := range props {
		i, ok := idx[p[0]]
		if !ok {
			return nil, fmt.Errorf("block states: %s has no property %q", name, p[0])
		}
		u, ok := v.Field(i).Addr().Interface().(encoding.TextUnmarshaler)
		if !ok {
			return nil, fmt.Errorf("block states: %s.%s is not a text property", name, p[0])
		}
		d.tmp = append(d.tmp[:0], p[1]...)
		if err := u.UnmarshalText(d.tmp); err != nil {
			return nil, fmt.Errorf("block states: %s: %w", name, err)
		}
	}
	return v.Interface().(Block), nil
}

func (d *stateReader) byte() byte {
	if d.err != nil {
		return 0
	}
	b, err := d.r.ReadByte()
	d.err = err
	return b
}

func (d *stateReader) int32() int32 {
	var n uint32
	for range 4 {
		n = n<<8 | uint32(d.byte())
	}
	return int32(n)
}

// raw reads a TAG_String's payload into a buffer reused by the next call.
func (d *stateReader) raw() []byte {
	n := int(d.byte())<<8 | int(d.byte())
	if cap(d.buf) < n {
		d.buf = make([]byte, n)
	}
	d.buf = d.buf[:n]
	if d.err == nil {
		_, d.err = io.ReadFull(d.r, d.buf)
	}
	return d.buf
}

// string reads a TAG_String, interned (the names are ASCII: modified UTF-8 is
// the same bytes).
func (d *stateReader) string() string {
	b := d.raw()
	if s, ok := d.names[string(b)]; ok {
		return s
	}
	if d.names == nil {
		d.names = map[string]string{}
	}
	s := string(b)
	d.names[s] = s
	return s
}
