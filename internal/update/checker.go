package update

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"
)

// CheckEvery: cada cuánto mira si hay versión nueva.
const CheckEvery = 24 * time.Hour

// Checker mira la última release (al arrancar y cada 24 h) y la recuerda.
type Checker struct {
	Current string       // la versión en marcha ("" o "dev": compilación de desarrollo, nunca avisa)
	APIURL  string       // por defecto LatestURL
	Client  *http.Client // por defecto, uno con tiempo máximo
	Enabled func() bool  // el ajuste "buscar actualizaciones" (nil = sí)
	Logf    func(string, ...any)

	mu      sync.Mutex
	rel     *Release
	checked time.Time
	logged  string // el último fallo que se contó (para no repetirlo cada día)
}

func (c *Checker) logf(f string, a ...any) {
	if c.Logf != nil {
		c.Logf(f, a...)
	} else {
		log.Printf(f, a...)
	}
}

func (c *Checker) enabled() bool { return c.Enabled == nil || c.Enabled() }

// Check pide la última release. manual (el botón "Comprobar ahora") vale aunque
// el ajuste esté apagado; la comprobación automática no hace nada (ni una
// petición) con el ajuste apagado. Los fallos de red, un 404 (repositorio
// privado) o el límite de GitHub se cuentan una vez en el log y no molestan.
func (c *Checker) Check(ctx context.Context, manual bool) error {
	if !manual && !c.enabled() {
		return nil
	}
	url := c.APIURL
	if url == "" {
		url = LatestURL
	}
	rel, err := FetchLatest(ctx, c.Client, url)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checked = time.Now()
	if err != nil {
		msg := err.Error()
		if c.logged != msg {
			c.logged = msg
			switch {
			case errors.Is(err, ErrNoRelease):
				c.logf("update: no release to compare with (%v)", err)
			default:
				c.logf("update: check failed: %v", err)
			}
		}
		return err
	}
	c.logged = ""
	c.rel = rel
	return nil
}

// Loop comprueba al arrancar y cada 24 h hasta que ctx acabe.
func (c *Checker) Loop(ctx context.Context) {
	if !ValidVersion(c.Current) {
		return // (compilación de desarrollo: no hay con qué comparar)
	}
	for {
		_ = c.Check(ctx, false)
		select {
		case <-ctx.Done():
			return
		case <-time.After(CheckEvery):
		}
	}
}

// Status es lo que ve el panel.
type Status struct {
	Enabled   bool       `json:"enabled"` // el ajuste
	Current   string     `json:"current"`
	Latest    string     `json:"latest,omitempty"`
	Available bool       `json:"available"`       // hay una versión más nueva
	Notes     string     `json:"notes,omitempty"` // la página de las notas de la versión
	Checked   *time.Time `json:"checked,omitempty"`
	Method    string     `json:"method"`
	Plan      Plan       `json:"plan"`
}

// Release devuelve la última release conocida (nil si aún no hay).
func (c *Checker) Release() *Release {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rel
}

// Status resume el estado (method: EffectiveMethod; writable: ver PlanFor).
func (c *Checker) Status(method string, writable bool) Status {
	c.mu.Lock()
	rel, checked := c.rel, c.checked
	c.mu.Unlock()
	s := Status{Enabled: c.enabled(), Current: c.Current, Method: method}
	if !checked.IsZero() {
		s.Checked = &checked
	}
	if rel != nil && Newer(rel.Tag, c.Current) {
		s.Available, s.Latest, s.Notes = true, rel.Tag, rel.Page
		s.Plan = PlanFor(method, writable, rel)
	}
	return s
}
