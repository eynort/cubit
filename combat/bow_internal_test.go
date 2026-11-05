package combat

import (
	"math"
	"testing"
)

// TestBallistic: el ángulo que da la trayectoria lleva la flecha al sitio
// (de vuelta por la misma simulación), a varias distancias y alturas.
func TestBallistic(t *testing.T) {
	for _, c := range [][2]float64{{10, 0}, {25, 0}, {40, -5}, {20, 6}} {
		p, n, ok := ballistic(c[0], c[1], arrowSpeed)
		if !ok {
			t.Errorf("%v: no llega", c)
			continue
		}
		r := -p * math.Pi / 180
		x, y, vx, vy := 0.0, 0.0, arrowSpeed*math.Cos(r), arrowSpeed*math.Sin(r)
		for i := 0; i < n; i++ {
			px, py := x, y
			x, y = x+vx, y+vy
			vx, vy = vx*arrowDrag, vy*arrowDrag-arrowGravity
			if x >= c[0] {
				x, y = c[0], py+(y-py)*(c[0]-px)/(x-px)
			}
		}
		t.Logf("%v: pitch %.1f, %d ticks, cae a %.1f %.2f", c, p, n, x, y)
		if math.Abs(y-c[1]) > 0.5 {
			t.Errorf("%v: llega a altura %.2f", c, y)
		}
	}
	if _, _, ok := ballistic(200, 0, arrowSpeed); ok {
		t.Error("200 bloques: no llega una flecha")
	}
}
