package mcserver

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cubit/internal/properties"
	"cubit/internal/rcon"
)

// TestMain: con CUBIT_FAKE_JAVA=1 este mismo programa hace de "java": un
// servidor de Minecraft de mentira que solo habla RCON (y lee "stop" de stdin).
func TestMain(m *testing.M) {
	if os.Getenv("CUBIT_FAKE_JAVA") == "1" {
		fakeJava()
		return
	}
	os.Exit(m.Run())
}

func fakeJava() {
	p, err := properties.Read("server.properties")
	if err != nil {
		fmt.Fprintln(os.Stderr, "sin server.properties:", err)
		os.Exit(2)
	}
	if _, err := os.Stat("eula.txt"); err != nil {
		os.Exit(3)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+p["rcon.port"])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(4)
	}
	fmt.Println("[fake] Done! RCON en", ln.Addr())
	go func() {
		b := make([]byte, 64)
		for {
			n, err := os.Stdin.Read(b)
			if err != nil {
				return
			}
			if strings.Contains(string(b[:n]), "stop") {
				os.Exit(0)
			}
		}
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			for {
				var n int32
				if binary.Read(c, binary.LittleEndian, &n) != nil {
					return
				}
				buf := make([]byte, n)
				if _, err := io.ReadFull(c, buf); err != nil {
					return
				}
				id, typ, body := int32(binary.LittleEndian.Uint32(buf)), binary.LittleEndian.Uint32(buf[4:]), strings.TrimRight(string(buf[8:]), "\x00")
				reply := func(id int32, s string) {
					var out bytes.Buffer
					_ = binary.Write(&out, binary.LittleEndian, int32(10+len(s)))
					_ = binary.Write(&out, binary.LittleEndian, id)
					_ = binary.Write(&out, binary.LittleEndian, int32(0))
					out.WriteString(s)
					out.Write([]byte{0, 0})
					_, _ = c.Write(out.Bytes())
				}
				switch {
				case typ == 3 && body != p["rcon.password"]:
					reply(-1, "")
				case typ == 3:
					reply(id, "")
				case body == "stop":
					reply(id, "Stopping the server")
					os.Exit(0)
				case body == "list":
					reply(id, "There are 0 of a max of 20 players online:")
				default:
					reply(id, "")
				}
			}
		}()
	}
}

func freePort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// testNative: el servidor nativo en una carpeta temporal, sin tocar Docker.
func testNative(t *testing.T, java ...string) string {
	data := t.TempDir()
	for _, k := range []string{"MCBOT_SERVER_DIR", "MCBOT_RCON", "MCBOT_RCON_PASSWORD", "MCBOT_NO_NATIVE", "MCBOT_TEST_SERVER"} {
		t.Setenv(k, "")
	}
	t.Setenv("MCBOT_SERVER_RCON_PORT", strconv.Itoa(freePort(t)))
	oldData, oldPoll, oldWarn := DataFile, supPoll, warnDelay
	DataFile = func(n string) string { return filepath.Join(data, n) }
	supPoll, warnDelay = 30*time.Millisecond, 0
	javaOverride = java
	if !EnableNative(data) {
		t.Fatal("EnableNative")
	}
	t.Cleanup(func() {
		Shutdown()
		nat.mu.Lock()
		nat.on, nat.cfg, nat.inst, nat.proc, nat.opped, nat.fresh = false, nativeCfg{}, installState{}, procState{}, false, false
		nat.mu.Unlock()
		rcon.SetOverride("", "")
		javaOverride = nil
		DataFile, supPoll, warnDelay = oldData, oldPoll, oldWarn
	})
	return filepath.Join(data, "server")
}

func waitFor(t *testing.T, what string, d time.Duration, f func() bool) {
	t.Helper()
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("esperando: %s", what)
}

func TestSelectKind(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for _, c := range []struct {
		env    map[string]string
		native bool
		want   string
	}{
		{map[string]string{"MCBOT_SERVER_DIR": "/server", "MCBOT_RCON": "minecraft:25575"}, true, "docker"},
		{map[string]string{"MCBOT_RCON": "x:25575"}, true, "docker"},
		{map[string]string{"MCBOT_SERVER_DIR": "/server"}, false, "docker"},
		{nil, true, "native"},
		{nil, false, ""},
		{map[string]string{"MCBOT_NO_NATIVE": "1"}, true, ""},
	} {
		if got := selectKind(env(c.env), c.native); got != c.want {
			t.Errorf("%v native=%v: %q, quería %q", c.env, c.native, got, c.want)
		}
	}
}

// El entorno de Docker manda aunque EnableNative se haya llamado.
func TestDockerWinsOverNative(t *testing.T) {
	testNative(t)
	t.Setenv("MCBOT_SERVER_DIR", t.TempDir())
	t.Setenv("MCBOT_RCON", "minecraft:25575")
	if b := activeBackend(); b == nil || b.Kind() != "docker" || nativeActive() {
		t.Fatalf("backend: %v", b)
	}
	if EnableNative(t.TempDir()) {
		t.Error("EnableNative con Docker")
	}
}

func TestNotReadyIsOff(t *testing.T) {
	dir := testNative(t)
	if nativeReady() || !Off() || PowerState(false) != "off" || serverDir() != "" {
		t.Fatal("sin instalar: apagado y sin carpeta")
	}
	if TestServerAddr() == "" {
		t.Error("el servidor de pruebas debería verse aunque falte instalarlo (para ofrecer instalarlo)")
	}
	for name, f := range map[string]func() error{"Start": Start, "stop": stopServer} {
		if err := f(); err != errNeedsSetup {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := newWorld("1"); err != errNeedsSetup {
		t.Errorf("newWorld: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "eula.txt")); err == nil {
		t.Error("eula.txt sin aceptar")
	}
}

func TestDefaultProps(t *testing.T) {
	dir := testNative(t)
	_ = os.MkdirAll(dir, 0o755)
	t.Setenv("MCBOT_TEST_PORT", "25570")
	if err := ensureProps("mi semilla"); err != nil {
		t.Fatal(err)
	}
	p, err := properties.Read(filepath.Join(dir, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"online-mode": "false", "enable-rcon": "true", "motd": "Cubit test server", "level-seed": "mi semilla", "server-port": "25570", "rcon.port": os.Getenv("MCBOT_SERVER_RCON_PORT")} {
		if p[k] != v {
			t.Errorf("%s = %q, quería %q", k, p[k], v)
		}
	}
	if len(p["rcon.password"]) < 20 || p["rcon.password"] != nat.cfg.RCONPassword {
		t.Errorf("contraseña de RCON: %q", p["rcon.password"])
	}
	// la contraseña vive en native.json (0600) y no cambia entre arranques
	b, _ := os.ReadFile(nat.cfgPath)
	if !strings.Contains(string(b), p["rcon.password"]) {
		t.Error("native.json sin contraseña")
	}
	_ = properties.Update(filepath.Join(dir, "server.properties"), map[string]string{"motd": "otro"})
	_ = ensureProps("")
	p, _ = properties.Read(filepath.Join(dir, "server.properties"))
	if p["motd"] != "otro" || p["level-seed"] != "mi semilla" {
		t.Errorf("pisó lo que ya había: %v", p)
	}
	if nativeXmx() != "1G" {
		t.Errorf("Xmx por defecto: %s", nativeXmx())
	}
	t.Setenv("MCBOT_SERVER_XMX", "3G")
	if nativeXmx() != "3G" {
		t.Errorf("Xmx del entorno: %s", nativeXmx())
	}
	t.Setenv("MCBOT_SERVER_XMX", "mucho")
	if nativeXmx() != "1G" {
		t.Errorf("Xmx inválido: %s", nativeXmx())
	}
}

// ---- las descargas oficiales, de mentira ----

type fakeOfficial struct {
	*httptest.Server
	jar, jre            []byte
	jarFails, javaFails atomic.Int32
	ranges              atomic.Int32
}

func tarGz(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "jdk-25-jre/", Typeflag: tar.TypeDir, Mode: 0o755})
	for n, c := range files {
		_ = tw.WriteHeader(&tar.Header{Name: "jdk-25-jre/" + n, Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(c))})
		_, _ = tw.Write([]byte(c))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func newFakeOfficial(t *testing.T) *fakeOfficial {
	f := &fakeOfficial{jar: bytes.Repeat([]byte("JAR!"), 50_000)}
	f.jre = tarGz(t, map[string]string{"bin/java": "#!/bin/sh\nexit 0\n", "bin/java.exe": "MZ", "release": "x"})
	mux := http.NewServeMux()
	var base string
	hx := func(b []byte, h func([]byte) []byte) string { return hex.EncodeToString(h(b)) }
	s1 := func(b []byte) []byte { s := sha1.Sum(b); return s[:] }
	s256 := func(b []byte) []byte { s := sha256.Sum256(b); return s[:] }
	vjson := func() []byte {
		b, _ := json.MarshalIndent(map[string]any{
			"id":          "26.2",
			"javaVersion": map[string]any{"majorVersion": 25, "component": "java-runtime-epsilon"},
			"downloads":   map[string]any{"server": map[string]any{"sha1": hx(f.jar, s1), "size": len(f.jar), "url": base + "/jar"}},
		}, "", "  ")
		return append(b, '\n')
	}
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"versions": []map[string]any{
			{"id": "26.1", "url": base + "/otra", "sha1": "00"},
			{"id": "26.2", "url": base + "/v.json", "sha1": hx(vjson(), s1)},
		}})
	})
	mux.HandleFunc("/v.json", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(vjson()) })
	mux.HandleFunc("/jar", func(w http.ResponseWriter, r *http.Request) {
		if f.jarFails.Add(-1) >= 0 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		if r.Header.Get("Range") != "" {
			f.ranges.Add(1)
		}
		http.ServeContent(w, r, "server.jar", time.Time{}, bytes.NewReader(f.jar))
	})
	mux.HandleFunc("/v3/assets/latest/25/hotspot", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("image_type") != "jre" || q.Get("vendor") != "eclipse" || q.Get("os") != "linux" || q.Get("architecture") != "x64" {
			http.Error(w, "query "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		pkg := func(os_, arch, name, sum string) map[string]any {
			return map[string]any{"release_name": "jdk-25+1", "binary": map[string]any{"os": os_, "architecture": arch, "image_type": "jre",
				"package": map[string]any{"link": base + "/jre.tar.gz", "checksum": sum, "name": name, "size": len(f.jre)}}}
		}
		_ = json.NewEncoder(w).Encode([]any{
			pkg("windows", "x64", "OpenJDK25U-jre_x64_windows_hotspot.zip", strings.Repeat("0", 64)),
			pkg("linux", "x64", "OpenJDK25U-jre_x64_linux_hotspot_25.tar.gz", hx(f.jre, s256)),
		})
	})
	mux.HandleFunc("/jre.tar.gz", func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "jre.tar.gz", time.Time{}, bytes.NewReader(f.jre))
	})
	f.Server = httptest.NewServer(mux)
	base = f.URL
	t.Cleanup(f.Close)
	oM, oA, oR, oO := manifestURL, adoptiumAPI, retryDelay, installOSArch
	manifestURL, adoptiumAPI, retryDelay = f.URL+"/manifest", f.URL, time.Millisecond
	installOSArch = func() (string, string) { return "linux", "amd64" }
	t.Cleanup(func() { manifestURL, adoptiumAPI, retryDelay, installOSArch = oM, oA, oR, oO })
	return f
}

func TestResolveVersionAndJava(t *testing.T) {
	f := newFakeOfficial(t)
	v, err := resolveVersion(t.Context(), "26.2")
	if err != nil {
		t.Fatal(err)
	}
	if v.Java != 25 || v.ServerURL != f.URL+"/jar" || v.Size != int64(len(f.jar)) || len(v.SHA1) != 40 {
		t.Fatalf("%+v", v)
	}
	if _, err := resolveVersion(t.Context(), "9.9"); err == nil {
		t.Error("versión que no existe")
	}
	p, err := resolveJava(t.Context(), 25, "linux", "amd64")
	if err != nil || !strings.HasSuffix(p.Name, "linux_hotspot_25.tar.gz") || len(p.Checksum) != 64 {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := resolveJava(t.Context(), 25, "freebsd", "amd64"); err == nil {
		t.Error("sistema sin Java")
	}
	for goos, want := range map[string][2]string{"linux": {"linux", "x64"}, "windows": {"windows", "x64"}, "darwin": {"mac", "x64"}} {
		o, a, err := adoptiumNames(goos, "amd64")
		if err != nil || o != want[0] || a != want[1] {
			t.Errorf("%s: %s %s %v", goos, o, a, err)
		}
	}
	if _, a, _ := adoptiumNames("darwin", "arm64"); a != "aarch64" {
		t.Errorf("arm64: %s", a)
	}
}

// El json de la versión que no coincide con el sha1 del manifiesto no se usa.
func TestVersionJSONChecksum(t *testing.T) {
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/m", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"versions":[{"id":"26.2","url":"%s/v","sha1":"%s"}]}`, base, strings.Repeat("a", 40))
	})
	mux.HandleFunc("/v", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"downloads":{}}`)) })
	s := httptest.NewServer(mux)
	defer s.Close()
	base = s.URL
	old, oldR := manifestURL, retryDelay
	defer func() { manifestURL, retryDelay = old, oldR }()
	manifestURL, retryDelay = s.URL+"/m", time.Millisecond
	if _, err := resolveVersion(t.Context(), "26.2"); err == nil || !strings.Contains(err.Error(), "suma") {
		t.Fatalf("debería rechazarlo: %v", err)
	}
}

func TestDownloadResumeRetryChecksum(t *testing.T) {
	f := newFakeOfficial(t)
	sum := sha1.Sum(f.jar)
	want := hex.EncodeToString(sum[:])
	noProg := func(int64, int64) {}

	// reintenta tras un 500
	dest := filepath.Join(t.TempDir(), "server.jar")
	f.jarFails.Store(2)
	if err := download(t.Context(), f.URL+"/jar", dest, sha1.New, want, int64(len(f.jar)), noProg, "server.jar"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dest); !bytes.Equal(b, f.jar) {
		t.Fatal("contenido distinto")
	}
	if _, err := os.Stat(dest + ".part"); err == nil {
		t.Error("queda el .part")
	}

	// sigue donde se quedó (Range)
	dest = filepath.Join(t.TempDir(), "server.jar")
	_ = os.WriteFile(dest+".part", f.jar[:70_000], 0o644)
	f.jarFails.Store(0)
	var lastDone int64
	if err := download(t.Context(), f.URL+"/jar", dest, sha1.New, want, int64(len(f.jar)), func(d, _ int64) { lastDone = d }, "server.jar"); err != nil {
		t.Fatal(err)
	}
	if f.ranges.Load() != 1 || lastDone != int64(len(f.jar)) {
		t.Errorf("Range=%d, progreso=%d", f.ranges.Load(), lastDone)
	}
	if b, _ := os.ReadFile(dest); !bytes.Equal(b, f.jar) {
		t.Fatal("contenido distinto tras seguir")
	}

	// suma que no coincide: error, nada en su sitio y sin .part
	dest = filepath.Join(t.TempDir(), "x.jar")
	err := download(t.Context(), f.URL+"/jar", dest, sha1.New, strings.Repeat("b", 40), int64(len(f.jar)), noProg, "server.jar")
	if err == nil || !strings.Contains(err.Error(), "suma") {
		t.Fatalf("debería fallar por la suma: %v", err)
	}
	if _, e := os.Stat(dest); e == nil {
		t.Error("dejó el archivo malo en su sitio")
	}
	if _, e := os.Stat(dest + ".part"); e == nil {
		t.Error("dejó el .part malo")
	}
}

func TestExtractArchive(t *testing.T) {
	dir := t.TempDir()
	ar := filepath.Join(dir, "j.tar.gz")
	_ = os.WriteFile(ar, tarGz(t, map[string]string{"bin/java": "x", "lib/a": "y"}), 0o644)
	dest := filepath.Join(dir, "out")
	if err := extractArchive(ar, dest); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dest, "bin", "java"))
	if err != nil || st.Mode()&0o100 == 0 {
		t.Fatalf("bin/java: %v %v", st, err)
	}
	// zip, y rutas que se salen
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("jdk/bin/java.exe")
	_, _ = w.Write([]byte("MZ"))
	zw.Close()
	z := filepath.Join(dir, "j.zip")
	_ = os.WriteFile(z, buf.Bytes(), 0o644)
	if err := extractArchive(z, filepath.Join(dir, "outz")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "outz", "bin", "java.exe")); err != nil {
		t.Error(err)
	}
	var bad bytes.Buffer
	gz := gzip.NewWriter(&bad)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "jdk/../../escape", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1})
	_, _ = tw.Write([]byte("x"))
	tw.Close()
	gz.Close()
	b := filepath.Join(dir, "bad.tar.gz")
	_ = os.WriteFile(b, bad.Bytes(), 0o644)
	if err := extractArchive(b, filepath.Join(dir, "outb")); err == nil {
		t.Error("debería rechazar la ruta que se sale")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape")); err == nil {
		t.Error("escribió fuera")
	}
}

// La EULA: sin aceptar no se escribe nada; aceptada, se instala todo y queda listo.
func TestSetupEULAGateAndInstall(t *testing.T) {
	f := newFakeOfficial(t)
	dir := testNative(t)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, powerMarker), nil, 0o644) // (que el supervisor no arranque lo falso)

	if err := Setup(false, ""); err == nil {
		t.Fatal("sin aceptar la EULA debería fallar")
	}
	if _, err := os.Stat(filepath.Join(dir, "eula.txt")); err == nil {
		t.Fatal("eula.txt sin que el jugador la acepte")
	}
	if _, err := os.Stat(filepath.Join(dir, "server.jar")); err == nil {
		t.Fatal("descargó sin aceptar")
	}
	if st := nativeStatusFor(func(e error) string { return e.Error() }); st.State != "none" || st.EULAURL != "https://aka.ms/MinecraftEULA" {
		t.Fatalf("estado: %+v", st)
	}

	f.jarFails.Store(1) // (un fallo de red por el camino)
	if err := Setup(true, "42"); err != nil {
		t.Fatal(err)
	}
	if err := Setup(true, ""); err == nil {
		t.Error("dos instalaciones a la vez")
	}
	waitFor(t, "instalado", 10*time.Second, func() bool { return !nat.instBusy() })
	st := nativeStatusFor(func(e error) string { return e.Error() })
	if st.State != "ready" || !st.Ready || st.Err != "" {
		t.Fatalf("estado: %+v", st)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "eula.txt")); !strings.Contains(string(b), "eula=true") {
		t.Errorf("eula.txt: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "server.jar")); !bytes.Equal(b, f.jar) {
		t.Error("server.jar distinto")
	}
	if javaPath() == "" {
		t.Error("sin Java")
	}
	p, _ := properties.Read(filepath.Join(dir, "server.properties"))
	if p["level-seed"] != "42" || p["online-mode"] != "false" || p["enable-rcon"] != "true" {
		t.Errorf("props: %v", p)
	}
	js, _ := json.Marshal(st)
	if strings.Contains(string(js), p["rcon.password"]) {
		t.Error("la contraseña de RCON sale en el estado del panel")
	}
	if rcon.Addr() == "" {
		t.Error("RCON sin activar tras instalar")
	}
	if _, err := os.Stat(filepath.Join(dir, "java-download")); err == nil {
		t.Error("queda el paquete descargado")
	}
}

func TestSetupError(t *testing.T) {
	f := newFakeOfficial(t)
	f.jarFails.Store(1000)
	dir := testNative(t)
	_ = os.MkdirAll(dir, 0o755)
	if err := Setup(true, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "fin", 10*time.Second, func() bool { return !nat.instBusy() })
	st := nativeStatusFor(func(e error) string { return e.Error() })
	if st.State != "error" || !strings.Contains(st.Err, "descargar") {
		t.Fatalf("estado: %+v", st)
	}
}

// ---- el proceso, con un java de mentira ----

func TestNativeLifecycle(t *testing.T) {
	t.Setenv("CUBIT_FAKE_JAVA", "1")
	exe, _ := os.Executable()
	dir := testNative(t, exe)
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "server.jar"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "eula.txt"), []byte("eula=true\n"), 0o644)
	if !nativeReady() {
		t.Fatal("debería estar listo")
	}
	up := func() bool { _, err := rcon.Cmd("list"); return err == nil }
	waitFor(t, "el servidor contesta por RCON", 15*time.Second, up)
	if Off() || PowerState(true) != "on" {
		t.Fatalf("encendido: off=%v power=%s", Off(), PowerState(true))
	}
	if l := serverLogs(10); len(l) == 0 || !strings.Contains(strings.Join(l, "\n"), "[fake]") {
		t.Errorf("logs: %v", l)
	}

	// apagar: marca + stop por RCON; se queda apagado
	if err := stopServer(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "se apaga", 15*time.Second, func() bool { return !up() })
	time.Sleep(400 * time.Millisecond)
	if up() || !Off() {
		t.Fatal("volvió a arrancar con la marca puesta")
	}
	// encender
	if err := Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "vuelve", 15*time.Second, up)

	// reiniciar (stop sin marca): lo vuelve a levantar
	if _, err := rcon.Cmd("stop"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "cae", 15*time.Second, func() bool { return !up() })
	waitFor(t, "se reinicia solo", 15*time.Second, up)

	// salir el hub: parada limpia
	Shutdown()
	if up() {
		t.Error("sigue en marcha tras Shutdown")
	}
	nat.mu.Lock()
	running := nat.proc.running
	nat.mu.Unlock()
	if running {
		t.Error("el proceso sigue")
	}
}

// Un nuevo mundo sin Docker: el mismo swapWorld con el servidor parado.
func TestNativeNewWorldOff(t *testing.T) {
	dir := testNative(t, "/no/existe")
	_ = os.MkdirAll(filepath.Join(dir, "world", "region"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "server.jar"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "eula.txt"), []byte("eula=true\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, powerMarker), nil, 0o644)
	if err := ensureProps(""); err != nil {
		t.Fatal(err)
	}
	seed, err := newWorld("77")
	if err != nil || seed != "77" {
		t.Fatalf("%q %v", seed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "world.anterior", "region")); err != nil {
		t.Error(err)
	}
	if p, _ := properties.Read(filepath.Join(dir, "server.properties")); p["level-seed"] != "77" {
		t.Errorf("semilla %q", p["level-seed"])
	}
}
