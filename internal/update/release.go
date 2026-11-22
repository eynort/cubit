// Package update: saber si hay una versión nueva de Cubit en GitHub y, según
// cómo se instaló, ponerla (descarga comprobada con SHA256 y firma ed25519,
// sustitución atómica del binario y vuelta atrás si la nueva no arranca) o
// decir qué comando hay que ejecutar.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LatestURL: la última versión publicada (sin cuenta, sin cookies, sin datos nuestros).
const LatestURL = "https://api.github.com/repos/eynort/cubit/releases/latest"

// UserAgent: genérico, no identifica a nadie.
const UserAgent = "cubit-updater"

// Asset es un archivo de una release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release es lo que se usa de la respuesta de GitHub.
type Release struct {
	Tag        string  `json:"tag_name"`
	Name       string  `json:"name"`
	Page       string  `json:"html_url"` // las notas de la versión
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Asset busca un archivo por nombre.
func (r *Release) Asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// AssetName es el binario de esta release para un sistema: cubit-<tag>-<os>-<arch>[.exe].
func AssetName(tag, goos, goarch string) string {
	n := fmt.Sprintf("cubit-%s-%s-%s", tag, goos, goarch)
	if goos == "windows" {
		n += ".exe"
	}
	return n
}

// ParseRelease lee la respuesta de /releases/latest.
func ParseRelease(b []byte) (*Release, error) {
	var r Release
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("unreadable release: %w", err)
	}
	if r.Tag == "" || !ValidVersion(r.Tag) {
		return nil, fmt.Errorf("release without a valid tag (%q)", r.Tag)
	}
	if r.Draft {
		return nil, errors.New("draft release")
	}
	return &r, nil
}

// Errores que no son un fallo: no hay (o no se ve) ninguna release.
var (
	ErrNoRelease   = errors.New("no public release found (private repo or none yet)")
	ErrRateLimited = errors.New("GitHub rate limit")
)

// FetchLatest pide la última release a url (un GET sin nada más que el User-Agent).
func FetchLatest(ctx context.Context, c *http.Client, url string) (*Release, error) {
	if c == nil {
		c = &http.Client{Timeout: 20 * time.Second}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized:
		return nil, ErrNoRelease // (un repositorio privado se ve como un 404)
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden:
		return nil, ErrRateLimited
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return ParseRelease(b)
}

// short quita el prefijo "v" para mostrar.
func short(v string) string { return strings.TrimPrefix(v, "v") }
