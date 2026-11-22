package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	maxSmall  = 1 << 20   // SHA256SUMS y su firma
	maxBinary = 300 << 20 // un binario (pasarse es un servidor raro)
)

// Applier descarga, comprueba y sustituye el binario.
type Applier struct {
	Exe     string            // el binario en marcha
	DataDir string            // hub.json, bots/, backups/
	Current string            // la versión en marcha (para la copia de seguridad)
	PubKey  ed25519.PublicKey // la que comprueba la firma (PublicKey())
	Client  *http.Client
	GOOS    string // (por defecto, los del binario)
	GOARCH  string
	Phase   func(string) // avisa: downloading, verifying, installing
}

func (a *Applier) phase(p string) {
	if a.Phase != nil {
		a.Phase(p)
	}
}

func get(ctx context.Context, c *http.Client, url string, max int64, sink io.Writer) error {
	if c == nil {
		c = &http.Client{Timeout: 15 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", filepath.Base(url), resp.Status)
	}
	n, err := io.Copy(sink, io.LimitReader(resp.Body, max+1))
	if err != nil {
		return err
	}
	if n > max {
		return fmt.Errorf("download %s: too big", filepath.Base(url))
	}
	return nil
}

type buf struct{ b []byte }

func (w *buf) Write(p []byte) (int, error) { w.b = append(w.b, p...); return len(p), nil }

// Apply deja la versión rel instalada en Exe (el binario de antes queda en
// Exe+".old") y apunta el arranque de prueba; no reinicia nada: eso lo hace el
// llamador. Si algo falla antes de sustituir, no se ha tocado nada.
func (a *Applier) Apply(ctx context.Context, rel *Release) error {
	goos, goarch := a.GOOS, a.GOARCH
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	name := AssetName(rel.Tag, goos, goarch)
	asset, ok := rel.Asset(name)
	if !ok {
		return fmt.Errorf("release %s has no file for %s/%s (%s)", rel.Tag, goos, goarch, name)
	}
	sumsA, ok1 := rel.Asset("SHA256SUMS")
	sigA, ok2 := rel.Asset("SHA256SUMS.sig")
	if !ok1 || !ok2 {
		return errors.New("release has no signed SHA256SUMS: refusing to install")
	}
	a.phase("verifying")
	var sums, sig buf
	if err := get(ctx, a.Client, sumsA.URL, maxSmall, &sums); err != nil {
		return err
	}
	if err := get(ctx, a.Client, sigA.URL, maxSmall, &sig); err != nil {
		return err
	}
	if err := VerifySignature(a.PubKey, sums.b, sig.b); err != nil {
		return err
	}
	table := ParseSums(sums.b)
	if _, ok := table[name]; !ok {
		return fmt.Errorf("%s is not in SHA256SUMS", name)
	}

	a.phase("downloading")
	newPath := a.Exe + ".new"
	f, err := os.OpenFile(newPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	h := sha256.New()
	err = get(ctx, a.Client, asset.URL, maxBinary, io.MultiWriter(f, h))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = VerifyChecksum(table, name, hex.EncodeToString(h.Sum(nil)))
	}
	if err != nil {
		_ = os.Remove(newPath)
		return err
	}

	a.phase("installing")
	if err := BackupConfigs(a.DataDir, a.Current); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("backup before updating: %w", err)
	}
	if err := Replace(a.Exe, newPath); err != nil {
		_ = os.Remove(newPath)
		return err
	}
	if err := writeState(a.DataDir, &state{From: a.Current, To: rel.Tag, Exe: a.Exe, At: time.Now()}); err != nil {
		_ = Rollback(a.Exe) // (sin el marcador no habría vuelta atrás automática: mejor no actualizar)
		return err
	}
	return nil
}

// Replace pone newPath en lugar de exe: exe pasa a exe+".old" y newPath a exe.
// En Windows se puede renombrar un .exe en marcha, por eso vale igual. Si el
// segundo paso falla, exe vuelve a su sitio.
func Replace(exe, newPath string) error {
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("cannot move the current binary aside: %w", err)
	}
	if err := os.Rename(newPath, exe); err != nil {
		if rerr := os.Rename(old, exe); rerr != nil {
			return fmt.Errorf("install failed (%v) and restoring the old binary failed: %w", err, rerr)
		}
		return fmt.Errorf("cannot put the new binary in place: %w", err)
	}
	return nil
}

// Rollback devuelve exe+".old" a exe (el binario nuevo queda en exe+".bad" y se borra si se puede).
func Rollback(exe string) error {
	old := exe + ".old"
	if _, err := os.Stat(old); err != nil {
		return errors.New("there is no previous binary to roll back to")
	}
	if err := os.Rename(old, exe); err == nil { // (Unix: sustituye en un solo paso)
		return nil
	}
	bad := exe + ".bad" // (Windows: no se sobrescribe un .exe en marcha; se aparta)
	_ = os.Remove(bad)
	if err := os.Rename(exe, bad); err != nil {
		return err
	}
	if err := os.Rename(old, exe); err != nil {
		_ = os.Rename(bad, exe)
		return err
	}
	_ = os.Remove(bad)
	return nil
}

// BackupConfigs copia hub.json y bots/*/bot.json a <data>/backups/<version>/.
func BackupConfigs(dataDir, version string) error {
	v := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, version)
	if v == "" {
		v = "dev"
	}
	dst := filepath.Join(dataDir, "backups", v)
	files := []string{"hub.json"}
	if ents, err := os.ReadDir(filepath.Join(dataDir, "bots")); err == nil {
		for _, e := range ents {
			if e.IsDir() {
				files = append(files, filepath.Join("bots", e.Name(), "bot.json"))
			}
		}
	}
	for _, rel := range files {
		b, err := os.ReadFile(filepath.Join(dataDir, rel))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		out := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(out, b, 0o600); err != nil {
			return err
		}
	}
	return nil
}
