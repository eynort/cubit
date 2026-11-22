package mcserver

// La instalación del servidor nativo, con lo oficial y comprobando todo:
//
//	server.jar  piston-meta.mojang.com: version_manifest_v2.json → el json de la
//	            versión (sha1 del manifiesto) → downloads.server (url, sha1) y
//	            javaVersion.majorVersion (el Java que pide esa versión)
//	Java        api.adoptium.net (Eclipse Temurin, JRE): el paquete de ese Java
//	            para este sistema y procesador, con su sha256
//
// Cada descarga va a <archivo>.part y sigue donde se quedó (Range) si se corta
// o si se vuelve a pulsar Instalar; solo cuando la suma coincide pasa a su sitio,
// así que lo que está en su sitio siempre está comprobado.
//
// La EULA (eula.txt) solo se escribe cuando el jugador la acepta (Setup).

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mj41/go-mc26/data/version"

	"cubit/internal/i18n"
)

// serverVersion: la versión de Minecraft del servidor (la que habla el bot).
func serverVersion() string { return version.Name }

// Direcciones oficiales (variables para las pruebas).
var (
	manifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	adoptiumAPI = "https://api.adoptium.net"
	httpClient  = &http.Client{Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: 15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 30 * time.Second,
	}}
	retryDelay    = 2 * time.Second
	dlAttempts    = 4
	installOSArch = func() (string, string) { return runtime.GOOS, runtime.GOARCH }
)

type installState struct {
	busy          bool
	step          string
	stepN, stepOf int
	done, total   int64
	java          int
	err           error
}

func (n *nativeState) setStep(step string, i int) {
	n.mu.Lock()
	n.inst.step, n.inst.stepN, n.inst.done, n.inst.total = step, i, 0, 0
	n.mu.Unlock()
}

func (n *nativeState) progress(done, total int64) {
	n.mu.Lock()
	n.inst.done, n.inst.total = done, total
	n.mu.Unlock()
}

// Setup: el jugador acepta la EULA y empieza la instalación (en segundo plano).
// Sin aceptar, no hace nada ni escribe eula.txt. Volver a llamarlo tras un error
// sigue donde se quedó.
func Setup(acceptEULA bool, seed string) error {
	if !nativeActive() {
		return i18n.Errorf("there is no local test server to set up here", "aquí no hay servidor de pruebas local que instalar")
	}
	if !acceptEULA {
		return i18n.Errorf("you need to accept the Minecraft EULA ("+EULAURL+") to install the server", "tienes que aceptar la EULA de Minecraft ("+EULAURL+") para instalar el servidor")
	}
	seed, err := checkSeedOpt(seed)
	if err != nil {
		return err
	}
	nat.mu.Lock()
	if nat.inst.busy {
		nat.mu.Unlock()
		return i18n.Errorf("the installation is already running", "la instalación ya está en marcha")
	}
	nat.inst = installState{busy: true, step: "resolve", stepN: 1, stepOf: 4}
	ctx := nat.ctx
	nat.mu.Unlock()
	go func() {
		err := install(ctx, seed)
		nat.mu.Lock()
		nat.inst.busy, nat.inst.err = false, err
		if err == nil {
			nat.fresh = true
		}
		nat.mu.Unlock()
		if err != nil {
			log.Printf("[servidor] instalación: %v", err)
			return
		}
		applyRCONOverride()
		log.Print("[servidor] servidor de pruebas instalado")
	}()
	return nil
}

// checkSeedOpt: la semilla de la primera vez ("" = al azar al generar el mundo).
func checkSeedOpt(seed string) (string, error) {
	seed = strings.TrimSpace(seed)
	if seed == "" {
		return "", nil
	}
	return checkSeed(seed)
}

func install(ctx context.Context, seed string) error {
	dir := nativeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// la EULA: el jugador la ha aceptado (Setup lo comprueba)
	if err := os.WriteFile(filepath.Join(dir, "eula.txt"), []byte("# Accepted by the user from the Cubit panel (https://aka.ms/MinecraftEULA)\neula=true\n"), 0o644); err != nil {
		return err
	}
	nat.setStep("resolve", 1)
	ver, err := resolveVersion(ctx, serverVersion())
	if err != nil {
		return err
	}
	nat.mu.Lock()
	nat.inst.java = ver.Java
	nat.mu.Unlock()

	// server.jar
	nat.setStep("server", 2)
	jar, stamp := filepath.Join(dir, "server.jar"), filepath.Join(dir, ".cubit-server")
	if b, _ := os.ReadFile(stamp); string(b) != ver.SHA1 || !fileExists(jar) {
		if err := download(ctx, ver.ServerURL, jar, sha1.New, ver.SHA1, ver.Size, nat.progress, "server.jar"); err != nil {
			return err
		}
		_ = os.WriteFile(stamp, []byte(ver.SHA1), 0o644)
	}

	// Java
	nat.setStep("java", 3)
	javaDir, jstamp := filepath.Join(dir, "java"), filepath.Join(dir, "java", ".cubit-java")
	if b, _ := os.ReadFile(jstamp); string(b) != fmt.Sprint(ver.Java) || javaPath() == "" {
		goos, goarch := installOSArch()
		pkg, err := resolveJava(ctx, ver.Java, goos, goarch)
		if err != nil {
			return err
		}
		dl := filepath.Join(dir, "java-download", path.Base(pkg.Name))
		if err := os.MkdirAll(filepath.Dir(dl), 0o755); err != nil {
			return err
		}
		if err := download(ctx, pkg.Link, dl, sha256.New, pkg.Checksum, pkg.Size, nat.progress, "Java"); err != nil {
			return err
		}
		nat.setStep("extract", 4)
		tmp := javaDir + ".tmp"
		_ = os.RemoveAll(tmp)
		if err := extractArchive(dl, tmp); err != nil {
			_ = os.RemoveAll(tmp)
			return i18n.Errorf("could not unpack Java: %v", "no pude descomprimir Java: %v", err)
		}
		_ = os.RemoveAll(javaDir)
		if err := os.Rename(tmp, javaDir); err != nil {
			return err
		}
		_ = os.WriteFile(jstamp, []byte(fmt.Sprint(ver.Java)), 0o644)
		_ = os.RemoveAll(filepath.Dir(dl))
		if javaPath() == "" {
			return i18n.Errorf("the Java package has no bin/java inside", "el paquete de Java no trae bin/java")
		}
	}
	return ensureProps(seed)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// ---- Mojang ----

type mcVersion struct {
	ServerURL, SHA1 string
	Size            int64
	Java            int
}

func fetchBytes(ctx context.Context, u, what string) ([]byte, error) {
	var last error
	for try := 0; try < dlAttempts; try++ {
		if try > 0 {
			sleepCtx(ctx, retryDelay)
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		req.Header.Set("User-Agent", "cubit-test-server-installer")
		resp, err := httpClient.Do(req)
		if err != nil {
			last = err
			continue
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			last = fmt.Errorf("HTTP %d %v", resp.StatusCode, err)
			continue
		}
		return b, nil
	}
	return nil, i18n.Errorf("could not reach %s (check your internet connection): %v", "no pude conectar con %s (¿hay internet?): %v", what, last)
}

func fetchJSON(ctx context.Context, u string, v any, what string) error {
	b, err := fetchBytes(ctx, u, what)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return i18n.Errorf("%s answered with something unexpected", "%s ha contestado algo inesperado", what)
	}
	return nil
}

func resolveVersion(ctx context.Context, id string) (mcVersion, error) {
	var man struct {
		Versions []struct {
			ID   string `json:"id"`
			URL  string `json:"url"`
			SHA1 string `json:"sha1"`
		} `json:"versions"`
	}
	if err := fetchJSON(ctx, manifestURL, &man, "Mojang"); err != nil {
		return mcVersion{}, err
	}
	var vurl, vsha string
	for _, v := range man.Versions {
		if v.ID == id {
			vurl, vsha = v.URL, v.SHA1
		}
	}
	if vurl == "" {
		return mcVersion{}, i18n.Errorf("Mojang has no server for Minecraft %s", "Mojang no tiene servidor para Minecraft %s", id)
	}
	// el json de la versión: se comprueba su sha1 del manifiesto
	b, err := getChecked(ctx, vurl, vsha)
	if err != nil {
		return mcVersion{}, err
	}
	var vj struct {
		Downloads struct {
			Server struct {
				SHA1 string `json:"sha1"`
				Size int64  `json:"size"`
				URL  string `json:"url"`
			} `json:"server"`
		} `json:"downloads"`
		JavaVersion struct {
			Major int `json:"majorVersion"`
		} `json:"javaVersion"`
	}
	if err := json.Unmarshal(b, &vj); err != nil {
		return mcVersion{}, i18n.Errorf("Mojang answered with something unexpected", "Mojang ha contestado algo inesperado")
	}
	s := vj.Downloads.Server
	if s.URL == "" || s.SHA1 == "" {
		return mcVersion{}, i18n.Errorf("Mojang offers no server download for Minecraft %s", "Mojang no ofrece descarga de servidor para Minecraft %s", id)
	}
	if vj.JavaVersion.Major < 8 {
		vj.JavaVersion.Major = 21
	}
	return mcVersion{ServerURL: s.URL, SHA1: strings.ToLower(s.SHA1), Size: s.Size, Java: vj.JavaVersion.Major}, nil
}

// getChecked: un archivo pequeño en memoria, con su sha1 si se sabe.
func getChecked(ctx context.Context, u, sha string) ([]byte, error) {
	out, err := fetchBytes(ctx, u, "Mojang")
	if err != nil {
		return nil, err
	}
	if sha != "" {
		sum := sha1.Sum(out)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), sha) {
			return nil, i18n.Errorf("the Mojang version file does not match its checksum: not using it", "el archivo de versión de Mojang no coincide con su suma: no lo uso")
		}
	}
	return out, nil
}

// ---- Adoptium ----

type javaPkg struct {
	Link, Checksum, Name string
	Size                 int64
}

// adoptiumNames: runtime.GOOS / GOARCH → los nombres de Adoptium.
func adoptiumNames(goos, goarch string) (string, string, error) {
	osn := map[string]string{"linux": "linux", "windows": "windows", "darwin": "mac"}[goos]
	arch := map[string]string{"amd64": "x64", "arm64": "aarch64"}[goarch]
	if osn == "" || arch == "" {
		return "", "", i18n.Errorf("no Java download for %s/%s: install Java yourself and use Docker or an external server", "no hay Java para %s/%s: instala Java tú y usa Docker o un servidor externo", goos, goarch)
	}
	return osn, arch, nil
}

func resolveJava(ctx context.Context, major int, goos, goarch string) (javaPkg, error) {
	osn, arch, err := adoptiumNames(goos, goarch)
	if err != nil {
		return javaPkg{}, err
	}
	u := fmt.Sprintf("%s/v3/assets/latest/%d/hotspot?architecture=%s&image_type=jre&os=%s&vendor=eclipse", adoptiumAPI, major, arch, osn)
	var rel []struct {
		Binary struct {
			OS, Architecture string
			ImageType        string `json:"image_type"`
			Package          struct {
				Link     string `json:"link"`
				Checksum string `json:"checksum"`
				Name     string `json:"name"`
				Size     int64  `json:"size"`
			} `json:"package"`
		} `json:"binary"`
	}
	if err := fetchJSON(ctx, u, &rel, "Adoptium (Java)"); err != nil {
		return javaPkg{}, err
	}
	for _, r := range rel {
		b := r.Binary
		p := b.Package
		if b.OS != osn || b.Architecture != arch || (b.ImageType != "" && b.ImageType != "jre") {
			continue
		}
		if p.Link == "" || len(p.Checksum) != 64 || !(strings.HasSuffix(p.Name, ".tar.gz") || strings.HasSuffix(p.Name, ".zip")) {
			continue
		}
		if pu, err := url.Parse(p.Link); err != nil || (pu.Scheme != "https" && pu.Scheme != "http") {
			continue
		}
		return javaPkg{Link: p.Link, Checksum: strings.ToLower(p.Checksum), Name: p.Name, Size: p.Size}, nil
	}
	return javaPkg{}, i18n.Errorf("Adoptium has no Java %d for %s/%s", "Adoptium no tiene Java %d para %s/%s", major, osn, arch)
}

// ---- descarga ----

var errChecksum = errors.New("checksum")

// download baja u a dest (por dest.part, siguiendo donde se quedó) y solo lo
// deja en su sitio si la suma coincide.
func download(ctx context.Context, u, dest string, newHash func() hash.Hash, want string, size int64, progress func(done, total int64), what string) error {
	want = strings.ToLower(want)
	var last error
	for try := 0; try < dlAttempts; try++ {
		if try > 0 {
			sleepCtx(ctx, retryDelay)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := downloadOnce(ctx, u, dest, newHash, want, size, progress)
		if err == nil {
			return nil
		}
		last = err
		log.Printf("[servidor] descarga de %s (intento %d): %v", what, try+1, err)
		if errors.Is(err, errChecksum) {
			_ = os.Remove(dest + ".part") // (empezar de cero)
		}
	}
	if errors.Is(last, errChecksum) {
		return i18n.Errorf("the %s download does not match its official checksum: try again", "la descarga de %s no coincide con su suma oficial: prueba otra vez", what)
	}
	return i18n.Errorf("could not download %s (check your internet connection, then press Install again: it resumes): %v", "no pude descargar %s (revisa internet y pulsa Instalar otra vez: sigue donde se quedó): %v", what, last)
}

func downloadOnce(ctx context.Context, u, dest string, newHash func() hash.Hash, want string, size int64, progress func(done, total int64)) error {
	part := dest + ".part"
	var off int64
	if st, err := os.Stat(part); err == nil {
		off = st.Size()
	}
	if size > 0 && off > size {
		_ = os.Remove(part)
		off = 0
	}
	if size > 0 && off == size { // ya estaba entero: solo falta comprobarlo
		return finish(part, dest, newHash, want)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "cubit-test-server-installer")
	if off > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", off))
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	flag := os.O_WRONLY | os.O_CREATE
	switch {
	case resp.StatusCode == http.StatusPartialContent && off > 0:
		flag |= os.O_APPEND
	case resp.StatusCode == http.StatusOK:
		off = 0 // (no sabe seguir: de cero)
		flag |= os.O_TRUNC
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		_ = os.Remove(part)
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	default:
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	total := size
	if total <= 0 && resp.ContentLength > 0 {
		total = off + resp.ContentLength
	}
	f, err := os.OpenFile(part, flag, 0o644)
	if err != nil {
		return err
	}
	done := off
	progress(done, total)
	buf := make([]byte, 64<<10)
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			done += int64(n)
			if time.Since(last) > 150*time.Millisecond {
				progress(done, total)
				last = time.Now()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	progress(done, total)
	if total > 0 && done != total {
		return fmt.Errorf("incomplete download (%d of %d bytes)", done, total)
	}
	return finish(part, dest, newHash, want)
}

func finish(part, dest string, newHash func() hash.Hash, want string) error {
	f, err := os.Open(part)
	if err != nil {
		return err
	}
	h := newHash()
	_, err = io.Copy(h, f)
	f.Close()
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("%w: %s != %s", errChecksum, got, want)
	}
	return os.Rename(part, dest)
}

// ---- descomprimir ----

// extractArchive: un .tar.gz o .zip a dest, sin su carpeta de arriba (Temurin
// trae jdk-…-jre/). No sale de dest.
func extractArchive(archive, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	if strings.HasSuffix(archive, ".zip") {
		return extractZip(archive, dest)
	}
	return extractTarGz(archive, dest)
}

// safeJoin: dest + el nombre sin su primera carpeta; error si sale de dest.
func safeJoin(dest, name string) (string, bool, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	parts := strings.Split(strings.Trim(name, "/"), "/")
	if len(parts) < 2 {
		return "", true, nil // (la carpeta de arriba o nada)
	}
	rel := path.Clean(strings.Join(parts[1:], "/"))
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || path.IsAbs(rel) || strings.Contains(rel, ":") {
		return "", false, fmt.Errorf("ruta no válida en el archivo: %q", name)
	}
	return filepath.Join(dest, filepath.FromSlash(rel)), false, nil
}

func extractTarGz(archive, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p, skip, err := safeJoin(dest, h.Name)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		if err := noLinkOnPath(dest, p); err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := writeFile(p, tr, os.FileMode(h.Mode)&0o777|0o600); err != nil {
				return err
			}
		case tar.TypeSymlink:
			tgt := filepath.Join(filepath.Dir(p), filepath.FromSlash(h.Linkname))
			if filepath.IsAbs(h.Linkname) || !strings.HasPrefix(tgt, filepath.Clean(dest)+string(filepath.Separator)) {
				return fmt.Errorf("enlace fuera de la carpeta: %q", h.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if runtime.GOOS != "windows" {
				_ = os.Remove(p)
				if err := os.Symlink(filepath.FromSlash(h.Linkname), p); err != nil {
					return err
				}
			}
		}
	}
}

func extractZip(archive, dest string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		p, skip, err := safeJoin(dest, zf.Name)
		if err != nil {
			return err
		}
		if skip {
			continue
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		err = writeFile(p, rc, zf.Mode().Perm()|0o600)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// noLinkOnPath: ninguna carpeta entre dest y p es un enlace. El destino de cada
// enlace se comprueba al crearlo, pero una cadena de enlaces relativos (a -> ..,
// a/b -> ../..) saldría de la carpeta; así no se escribe nunca a través de uno.
func noLinkOnPath(dest, p string) error {
	rel, err := filepath.Rel(dest, filepath.Dir(p))
	if err != nil || rel == "." {
		return err
	}
	cur := dest
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if err != nil {
			return nil // (aún no existe: se crea como carpeta normal)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("ruta a través de un enlace en el archivo: %q", p)
		}
	}
	return nil
}

func writeFile(p string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	_ = os.Remove(p) // (si ya hay un enlace con ese nombre, no se sigue)
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
