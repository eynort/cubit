package block

import (
	"bytes"
	"compress/gzip"
	"reflect"
	"testing"

	"github.com/mj41/go-mc26/nbt"
)

// TestDecodeStatesMatchesNBT: the fast decoder gives exactly the blocks the
// reflective NBT decoder gave (State.Block for each state), in the same order.
func TestDecodeStatesMatchesNBT(t *testing.T) {
	z, err := gzip.NewReader(bytes.NewReader(blockStates))
	if err != nil {
		t.Fatal(err)
	}
	var states []State
	if _, err := nbt.NewDecoder(z).Decode(&states); err != nil {
		t.Fatal(err)
	}
	if len(states) != len(StateList) {
		t.Fatalf("%d states by NBT, %d by decodeStates", len(states), len(StateList))
	}
	for i, s := range states {
		want, err := s.Block()
		if err != nil {
			t.Fatal(err)
		}
		if reflect.TypeOf(want) != reflect.TypeOf(StateList[i]) || !reflect.DeepEqual(want, StateList[i]) {
			t.Fatalf("state %d: NBT %#v, decodeStates %#v", i, want, StateList[i])
		}
		if ToStateID[want] != StateID(i) {
			t.Fatalf("state %d: ToStateID %d", i, ToStateID[want])
		}
	}
}

func BenchmarkDecodeStates(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		z, _ := gzip.NewReader(bytes.NewReader(blockStates))
		if _, err := decodeStates(z); err != nil {
			b.Fatal(err)
		}
	}
}
