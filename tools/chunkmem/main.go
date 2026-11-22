// chunkmem mide cuánta memoria ocupan los chunks de un mundo de verdad tal y
// como los guarda el bot (level.Chunk: secciones con paleta), sin servidor:
// lee una región .mca (por defecto, la del mundo de prueba de go-mc26),
// pasa cada chunk por la forma del paquete (como llega por la red) y lo
// guarda como lo guarda bot/world. docs/PERFORMANCE.md.
//
//	go run ./tools/chunkmem [-region r.0.0.mca]
package main

import (
	"flag"
	"fmt"
	"log"
	"runtime"
	"runtime/debug"

	"github.com/mj41/go-mc26/level"
	"github.com/mj41/go-mc26/save"
	"github.com/mj41/go-mc26/save/region"
)

var trimFlag = flag.Bool("trim", false, "sin mapas de alturas ni biomas (lo que el bot no lee)")

var regionFlag = flag.String("region", "third_party/go-mc26/save/testdata/world/region/r.0.0.mca", "archivo de región (.mca)")

func main() {
	flag.Parse()
	r, err := region.OpenReadOnly(*regionFlag)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()
	var wires [][]level.LevelChunkSection
	for i := 0; i < 32; i++ {
		for j := 0; j < 32; j++ {
			if !r.ExistSector(i, j) {
				continue
			}
			data, err := r.ReadSector(i, j)
			if err != nil {
				continue
			}
			var sc save.Chunk
			if err := sc.Load(data); err != nil || sc.Status != "minecraft:full" {
				continue // (solo los chunks acabados: los que manda un servidor)
			}
			c, err := level.ChunkFromSave(&sc)
			if err != nil {
				continue
			}
			wires = append(wires, c.WireSections())
		}
	}
	if len(wires) == 0 {
		log.Fatal("no chunks")
	}
	secs := len(wires[0])
	before := live()
	kept := make([]*level.Chunk, 0, len(wires))
	nonSingle := 0
	for _, w := range wires {
		if len(w) != secs {
			continue
		}
		c := level.EmptyChunk(secs)
		if err := c.PutSections(w); err != nil {
			log.Fatal(err)
		}
		if *trimFlag {
			c.HeightMaps = level.HeightMaps{}
			for i := range c.Sections {
				c.Sections[i].Biomes = nil
			}
		}
		for i := range c.Sections {
			if len(c.Sections[i].States.Palette()) > 1 {
				nonSingle++
			}
		}
		kept = append(kept, c)
	}
	after := live()
	per := float64(after-before) / float64(len(kept))
	fmt.Printf("%d chunks de %d secciones (%d con más de un bloque): %.1f KB por chunk\n", len(kept), secs, nonSingle, per/1024)
	for _, v := range []int{2, 4, 6, 8, 10, 12} {
		n := (2*v + 1) * (2*v + 1)
		fmt.Printf("  vista %2d (%3d chunks): %5.1f MB\n", v, n, per*float64(n)/(1<<20))
	}
	runtime.KeepAlive(kept)
	runtime.KeepAlive(wires) // (vivos en las dos medidas: lo que se mide es solo lo guardado)
}

func live() uint64 {
	debug.FreeOSMemory()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}
