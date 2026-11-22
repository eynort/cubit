package packet_test

import (
	"bytes"
	"math/rand"
	"testing"

	pk "github.com/mj41/go-mc26/net/packet"
)

// compressedStream is n packets framed with compression: some past the
// threshold (compressed), some under it, of varied contents.
func compressedStream(t testing.TB, n int) ([]pk.Packet, []byte) {
	rnd := rand.New(rand.NewSource(1))
	var out bytes.Buffer
	var ps []pk.Packet
	for i := range n {
		size := 16 + rnd.Intn(4000)
		data := make([]byte, size)
		for j := range data {
			data[j] = byte(rnd.Intn(8)) // compressible, as chunks are
		}
		p := pk.Packet{ID: int32(i % 100), Data: data}
		if err := p.Pack(&out, 256); err != nil {
			t.Fatal(err)
		}
		ps = append(ps, p)
	}
	return ps, out.Bytes()
}

// TestUnpackCompressedStream: packets read back one after another from one
// stream (the zlib reader reused between them) are the packets written.
func TestUnpackCompressedStream(t *testing.T) {
	want, stream := compressedStream(t, 300)
	r := bytes.NewReader(stream)
	var p pk.Packet
	for i, w := range want {
		if err := p.UnPack(r, 256); err != nil {
			t.Fatalf("packet %d: %v", i, err)
		}
		if p.ID != w.ID || !bytes.Equal(p.Data, w.Data) {
			t.Fatalf("packet %d: id %d len %d, want id %d len %d", i, p.ID, len(p.Data), w.ID, len(w.Data))
		}
	}
	if r.Len() != 0 {
		t.Fatalf("%d bytes left", r.Len())
	}
}

func BenchmarkUnpackCompressed(b *testing.B) {
	_, stream := compressedStream(b, 100)
	b.ReportAllocs()
	b.SetBytes(int64(len(stream)))
	var p pk.Packet
	for b.Loop() {
		r := bytes.NewReader(stream)
		for r.Len() > 0 {
			if err := p.UnPack(r, 256); err != nil {
				b.Fatal(err)
			}
		}
	}
}
