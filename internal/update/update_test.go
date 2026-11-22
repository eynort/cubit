package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		c    int
		ok   bool
	}{
		{"v1.2.3", "v1.2.3", 0, true},
		{"v1.2.4", "v1.2.3", 1, true},
		{"v1.10.0", "v1.9.9", 1, true},
		{"1.0.0", "v1.0.0", 0, true},
		{"v0.0.1", "v0.1.0", -1, true},
		{"v1.0.0", "v1.0.0-rc1", 1, true},
		{"v1.0.0-rc1", "v1.0.0", -1, true},
		{"v1.0.0-rc2", "v1.0.0-rc1", 1, true},
		{"v1.0", "v1.0.0", 0, true},
		{"dev", "v1.0.0", 0, false},
		{"", "v1.0.0", 0, false},
		{"v1.x.0", "v1.0.0", 0, false},
	}
	for _, c := range cases {
		got, ok := Compare(c.a, c.b)
		if got != c.c || ok != c.ok {
			t.Errorf("Compare(%q,%q) = %d,%v; want %d,%v", c.a, c.b, got, ok, c.c, c.ok)
		}
	}
	if Newer("v1.0.1", "dev") || Newer("v1.0.1", "") {
		t.Error("a dev build must never be offered an update")
	}
	if !Newer("v1.0.1", "v1.0.0") || Newer("v1.0.0", "v1.0.0") || Newer("v0.9.0", "v1.0.0") {
		t.Error("Newer is wrong")
	}
}

const releaseJSON = `{"tag_name":"v1.2.0","name":"Cubit 1.2.0","html_url":"https://github.com/eynort/cubit/releases/tag/v1.2.0","draft":false,"prerelease":false,
"assets":[{"name":"cubit-v1.2.0-linux-amd64","browser_download_url":"http://x/a","size":3},{"name":"cubit_1.2.0_amd64.deb","browser_download_url":"http://x/d","size":3},{"name":"cubit-1.2.0.x86_64.rpm","browser_download_url":"http://x/r","size":3}]}`

func TestParseRelease(t *testing.T) {
	r, err := ParseRelease([]byte(releaseJSON))
	if err != nil || r.Tag != "v1.2.0" || len(r.Assets) != 3 || r.Page == "" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, ok := r.Asset("cubit-v1.2.0-linux-amd64"); !ok {
		t.Error("asset not found")
	}
	for _, bad := range []string{`{`, `{"tag_name":""}`, `{"tag_name":"nightly"}`, `{"tag_name":"v1.0.0","draft":true}`} {
		if _, err := ParseRelease([]byte(bad)); err == nil {
			t.Errorf("%s should fail", bad)
		}
	}
	if AssetName("v1.0.0", "windows", "amd64") != "cubit-v1.0.0-windows-amd64.exe" || AssetName("v1.0.0", "linux", "arm64") != "cubit-v1.0.0-linux-arm64" {
		t.Error("AssetName")
	}
}

func TestFetchLatestSendsNothingIdentifying(t *testing.T) {
	var ua, auth, cookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua, auth, cookie = r.Header.Get("User-Agent"), r.Header.Get("Authorization"), r.Header.Get("Cookie")
		if r.Method != http.MethodGet || r.ContentLength > 0 {
			t.Error("must be a plain GET")
		}
		fmt.Fprint(w, releaseJSON)
	}))
	defer srv.Close()
	r, err := FetchLatest(context.Background(), srv.Client(), srv.URL)
	if err != nil || r.Tag != "v1.2.0" {
		t.Fatal(r, err)
	}
	if ua != "cubit-updater" || auth != "" || cookie != "" {
		t.Errorf("headers: ua=%q auth=%q cookie=%q", ua, auth, cookie)
	}
}

func TestFetchLatestGraceful(t *testing.T) {
	for code, want := range map[int]error{404: ErrNoRelease, 429: ErrRateLimited, 403: ErrRateLimited} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", code) }))
		_, err := FetchLatest(context.Background(), srv.Client(), srv.URL)
		srv.Close()
		if !errors.Is(err, want) {
			t.Errorf("%d: %v", code, err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", 500) }))
	defer srv.Close()
	if _, err := FetchLatest(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Error("500 should be an error")
	}
}

// ---- firma y sumas ----

func keys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	p, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return p, k
}

func TestSignatureAndChecksum(t *testing.T) {
	pub, priv := keys(t)
	otherPub, _ := keys(t)
	sums := []byte(SumHex([]byte("hello")) + "  cubit-v1-linux-amd64\n")
	sig := ed25519.Sign(priv, sums)
	b64 := []byte(base64.StdEncoding.EncodeToString(sig) + "\n")

	if err := VerifySignature(pub, sums, sig); err != nil {
		t.Errorf("raw signature: %v", err)
	}
	if err := VerifySignature(pub, sums, b64); err != nil {
		t.Errorf("base64 signature: %v", err)
	}
	if VerifySignature(otherPub, sums, sig) == nil {
		t.Error("signature with another key must fail")
	}
	if VerifySignature(pub, append([]byte("x"), sums...), sig) == nil {
		t.Error("tampered SHA256SUMS must fail")
	}
	if VerifySignature(pub, sums, []byte("garbage")) == nil || VerifySignature(nil, sums, sig) == nil {
		t.Error("garbage / no key must fail")
	}

	table := ParseSums(sums)
	if err := VerifyChecksum(table, "cubit-v1-linux-amd64", SumHex([]byte("hello"))); err != nil {
		t.Error(err)
	}
	if VerifyChecksum(table, "cubit-v1-linux-amd64", SumHex([]byte("hellO"))) == nil {
		t.Error("wrong hash must fail")
	}
	if VerifyChecksum(table, "other", SumHex([]byte("hello"))) == nil {
		t.Error("unknown file must fail")
	}
	if _, err := PublicKey(); err != nil {
		t.Errorf("embedded key: %v", err)
	}
	restore := SetPublicKeyForTest("")
	defer restore()
	if _, err := PublicKey(); !errors.Is(err, ErrNoKey) {
		t.Errorf("empty key should report ErrNoKey, got %v", err)
	}
}

// ---- método de instalación ----

func TestPlanFor(t *testing.T) {
	rel, _ := ParseRelease([]byte(releaseJSON))
	cases := []struct {
		method   string
		writable bool
		kind     string
		cmd      string
	}{
		{"script", true, "self", ""},
		{"zip", true, "self", ""},
		{"", true, "self", ""},
		{"script", false, "manual", ""},
		{"scoop", true, "command", "scoop update cubit"},
		{"winget", true, "command", "winget upgrade Cubit"},
		{"docker", true, "command", "./up.sh update"},
		{"weird", true, "manual", ""},
	}
	for _, c := range cases {
		p := PlanFor(c.method, c.writable, rel)
		if p.Kind != c.kind || p.Command != c.cmd {
			t.Errorf("%q writable=%v: %+v", c.method, c.writable, p)
		}
	}
	if p := PlanFor("deb", true, rel); p.Kind != "command" || !strings.HasPrefix(p.Command, "sudo apt install ./") || !strings.HasSuffix(p.Command, ".deb") {
		t.Errorf("deb: %+v", p)
	}
	if p := PlanFor("rpm", true, rel); p.Kind != "command" || !strings.HasPrefix(p.Command, "sudo dnf install ./") || !strings.HasSuffix(p.Command, ".rpm") {
		t.Errorf("rpm: %+v", p)
	}
	if p := PlanFor("deb", true, nil); !strings.Contains(p.Command, ".deb") {
		t.Errorf("deb without release: %+v", p)
	}
}

func TestEffectiveMethodMarker(t *testing.T) {
	dir := t.TempDir()
	old := Method
	defer func() { Method = old }()
	Method = ""
	if m := EffectiveMethod(dir); m != "" {
		t.Errorf("no marker: %q", m)
	}
	os.WriteFile(filepath.Join(dir, MarkerFile), []byte("Script\n"), 0o644)
	if m := EffectiveMethod(dir); m != "script" {
		t.Errorf("marker: %q", m)
	}
	os.WriteFile(filepath.Join(dir, MarkerFile), []byte("nonsense"), 0o644)
	if m := EffectiveMethod(dir); m != "" {
		t.Errorf("unknown marker: %q", m)
	}
	Method = "deb"
	if m := EffectiveMethod(dir); m != "deb" {
		t.Errorf("ldflags wins: %q", m)
	}
}

// ---- el comprobador y su ajuste ----

func TestCheckerRespectsSetting(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, releaseJSON)
	}))
	defer srv.Close()
	on := false
	c := &Checker{Current: "v1.0.0", APIURL: srv.URL, Client: srv.Client(), Enabled: func() bool { return on }, Logf: func(string, ...any) {}}

	if err := c.Check(context.Background(), false); err != nil || hits.Load() != 0 {
		t.Fatalf("disabled: automatic check must not request anything (hits=%d, err=%v)", hits.Load(), err)
	}
	if s := c.Status("script", true); s.Available || s.Enabled {
		t.Errorf("disabled status: %+v", s)
	}
	if err := c.Check(context.Background(), true); err != nil || hits.Load() != 1 {
		t.Fatalf("manual check works even when disabled: hits=%d err=%v", hits.Load(), err)
	}
	on = true
	if err := c.Check(context.Background(), false); err != nil || hits.Load() != 2 {
		t.Fatalf("enabled: hits=%d err=%v", hits.Load(), err)
	}
	s := c.Status("script", true)
	if !s.Available || s.Latest != "v1.2.0" || s.Plan.Kind != "self" || s.Notes == "" || s.Checked == nil {
		t.Errorf("status: %+v", s)
	}
	c.Current = "v1.2.0"
	if c.Status("script", true).Available {
		t.Error("same version is not an update")
	}
	c.Current = "dev"
	if c.Status("script", true).Available {
		t.Error("dev build is never offered an update")
	}
}

func TestCheckerLogsFailuresOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }))
	defer srv.Close()
	var logs int
	c := &Checker{Current: "v1.0.0", APIURL: srv.URL, Client: srv.Client(), Logf: func(string, ...any) { logs++ }}
	for i := 0; i < 3; i++ {
		if err := c.Check(context.Background(), false); !errors.Is(err, ErrNoRelease) {
			t.Fatal(err)
		}
	}
	if logs != 1 {
		t.Errorf("a repeated failure should be logged once, got %d", logs)
	}
	if c.Status("script", true).Available {
		t.Error("no release, no update")
	}
}

// ---- aplicar: descarga, comprobación, sustitución, vuelta atrás ----

type fixture struct {
	srv     *httptest.Server
	pub     ed25519.PublicKey
	rel     *Release
	binary  []byte
	exe     string
	data    string
	applier *Applier
}

// newFixture monta un "GitHub" falso con la release v1.2.0 y un binario falso instalado.
func newFixture(t *testing.T, mutate func(sums *string, sig *[]byte, bin *[]byte)) *fixture {
	t.Helper()
	pub, priv := keys(t)
	f := &fixture{pub: pub, binary: []byte("#!/bin/sh\necho new version\n")}
	name := AssetName("v1.2.0", "linux", "amd64")
	sums := SumHex(f.binary) + "  " + name + "\n" + SumHex([]byte("zzz")) + "  other\n"
	sig := ed25519.Sign(priv, []byte(sums))
	served := f.binary
	if mutate != nil {
		mutate(&sums, &sig, &served)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) { w.Write(served) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, sums) })
	mux.HandleFunc("/sig", func(w http.ResponseWriter, r *http.Request) { w.Write(sig) })
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	f.rel = &Release{Tag: "v1.2.0", Page: "https://example.invalid/notes", Assets: []Asset{
		{Name: name, URL: f.srv.URL + "/bin"},
		{Name: "SHA256SUMS", URL: f.srv.URL + "/sums"},
		{Name: "SHA256SUMS.sig", URL: f.srv.URL + "/sig"},
	}}
	root := t.TempDir()
	f.data = filepath.Join(root, "data")
	os.MkdirAll(filepath.Join(f.data, "bots", "alex"), 0o755)
	os.WriteFile(filepath.Join(f.data, "hub.json"), []byte(`{"token":"t"}`), 0o600)
	os.WriteFile(filepath.Join(f.data, "bots", "alex", "bot.json"), []byte(`{"name":"Alex"}`), 0o600)
	f.exe = filepath.Join(root, "cubit")
	os.WriteFile(f.exe, []byte("OLD BINARY"), 0o755)
	f.applier = &Applier{Exe: f.exe, DataDir: f.data, Current: "v1.0.0", PubKey: pub, Client: f.srv.Client(), GOOS: "linux", GOARCH: "amd64"}
	return f
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestApplyGood(t *testing.T) {
	f := newFixture(t, nil)
	var phases []string
	f.applier.Phase = func(p string) { phases = append(phases, p) }
	if err := f.applier.Apply(context.Background(), f.rel); err != nil {
		t.Fatal(err)
	}
	if read(t, f.exe) != string(f.binary) || read(t, f.exe+".old") != "OLD BINARY" {
		t.Error("binary not replaced atomically / old not kept")
	}
	if _, err := os.Stat(f.exe + ".new"); !os.IsNotExist(err) {
		t.Error(".new must be gone")
	}
	if fi, _ := os.Stat(f.exe); fi.Mode()&0o100 == 0 {
		t.Error("new binary must be executable")
	}
	if read(t, filepath.Join(f.data, "backups", "v1.0.0", "hub.json")) != `{"token":"t"}` ||
		read(t, filepath.Join(f.data, "backups", "v1.0.0", "bots", "alex", "bot.json")) != `{"name":"Alex"}` {
		t.Error("config backup missing")
	}
	if !Pending(f.data, "v1.2.0") || Pending(f.data, "v1.0.0") {
		t.Error("pending marker for the new version expected")
	}
	if strings.Join(phases, ",") != "verifying,downloading,installing" {
		t.Errorf("phases: %v", phases)
	}
}

func TestApplyRefusesBadDownloads(t *testing.T) {
	cases := map[string]func(sums *string, sig *[]byte, bin *[]byte){
		"tampered binary": func(s *string, g *[]byte, b *[]byte) { *b = []byte("evil") },
		"bad signature":   func(s *string, g *[]byte, b *[]byte) { (*g)[0] ^= 0xff },
		"sums altered after signing": func(s *string, g *[]byte, b *[]byte) {
			*s = SumHex([]byte("evil")) + "  " + AssetName("v1.2.0", "linux", "amd64") + "\n"
			*b = []byte("evil")
		},
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, mut)
			if err := f.applier.Apply(context.Background(), f.rel); err == nil {
				t.Fatal("must be refused")
			}
			if read(t, f.exe) != "OLD BINARY" {
				t.Error("the running binary must be untouched")
			}
			for _, x := range []string{f.exe + ".new", f.exe + ".old", filepath.Join(f.data, pendingFile)} {
				if _, err := os.Stat(x); !os.IsNotExist(err) {
					t.Errorf("%s must not exist", x)
				}
			}
		})
	}
}

func TestApplyNeedsKeyAndSignedAssets(t *testing.T) {
	f := newFixture(t, nil)
	f.applier.PubKey = nil
	if err := f.applier.Apply(context.Background(), f.rel); err == nil || read(t, f.exe) != "OLD BINARY" {
		t.Errorf("no key: %v", err)
	}
	f = newFixture(t, nil)
	f.rel.Assets = f.rel.Assets[:2] // sin .sig
	if err := f.applier.Apply(context.Background(), f.rel); err == nil || read(t, f.exe) != "OLD BINARY" {
		t.Errorf("unsigned release: %v", err)
	}
	f = newFixture(t, nil)
	f.applier.GOARCH = "riscv64"
	if err := f.applier.Apply(context.Background(), f.rel); err == nil {
		t.Error("no asset for this platform must fail")
	}
}

func TestReplaceFailureRestores(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "cubit")
	os.WriteFile(exe, []byte("OLD"), 0o755)
	if err := Replace(exe, filepath.Join(dir, "missing.new")); err == nil {
		t.Fatal("replace with a missing file must fail")
	}
	if read(t, exe) != "OLD" {
		t.Error("the old binary must be put back")
	}
}

func TestRollback(t *testing.T) {
	f := newFixture(t, nil)
	if err := f.applier.Apply(context.Background(), f.rel); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(f.exe); err != nil {
		t.Fatal(err)
	}
	if read(t, f.exe) != "OLD BINARY" {
		t.Error("rollback must restore the old binary")
	}
	if Rollback(f.exe) == nil {
		t.Error("a second rollback has nothing to restore")
	}
}

func TestHealthFlow(t *testing.T) {
	// 1) arranque bueno: cuenta uno, se confirma
	f := newFixture(t, nil)
	if err := f.applier.Apply(context.Background(), f.rel); err != nil {
		t.Fatal(err)
	}
	if rb, _ := BeginStart(f.data, "v1.2.0"); rb {
		t.Fatal("first launch must not roll back")
	}
	MarkOK(f.data, "v1.2.0")
	if Pending(f.data, "v1.2.0") {
		t.Error("pending must be cleared by MarkOK")
	}
	if _, err := os.Stat(filepath.Join(f.data, okFile)); err != nil {
		t.Error("ok marker expected")
	}

	// 2) el nuevo se cae y se vuelve a lanzar: vuelve atrás y lo cuenta
	f = newFixture(t, nil)
	f.applier.Apply(context.Background(), f.rel)
	BeginStart(f.data, "v1.2.0") // (primer arranque, se cae sin confirmar)
	rb, exe := BeginStart(f.data, "v1.2.0")
	if !rb || exe != f.exe {
		t.Fatalf("second launch without OK must roll back (rb=%v exe=%q)", rb, exe)
	}
	if read(t, f.exe) != "OLD BINARY" {
		t.Error("old binary must be back")
	}
	r := LastRollback(f.data)
	if r == nil || r.From != "v1.2.0" || r.To != "v1.0.0" || r.Reason == "" {
		t.Errorf("rollback record: %+v", r)
	}
	DismissRollback(f.data)
	if LastRollback(f.data) != nil {
		t.Error("dismiss")
	}

	// 3) parar a mano antes de confirmar no es un fallo
	f = newFixture(t, nil)
	f.applier.Apply(context.Background(), f.rel)
	BeginStart(f.data, "v1.2.0")
	NoteCleanExit(f.data, "v1.2.0")
	if rb, _ := BeginStart(f.data, "v1.2.0"); rb {
		t.Error("a clean stop must not count as a failed start")
	}

	// 4) el vigilante: DoRollback directo
	f = newFixture(t, nil)
	f.applier.Apply(context.Background(), f.rel)
	if err := DoRollback(f.data, "no answer"); err != nil || read(t, f.exe) != "OLD BINARY" {
		t.Errorf("DoRollback: %v", err)
	}

	// 5) un marcador de otra versión se descarta
	f = newFixture(t, nil)
	f.applier.Apply(context.Background(), f.rel)
	if rb, _ := BeginStart(f.data, "v9.9.9"); rb || Pending(f.data, "v1.2.0") {
		t.Error("stale marker must be dropped")
	}
}

func TestStatusJSONShape(t *testing.T) {
	c := &Checker{Current: "v1.0.0"}
	b, _ := json.Marshal(c.Status("deb", true))
	if !strings.Contains(string(b), `"available":false`) || !strings.Contains(string(b), `"method":"deb"`) {
		t.Errorf("%s", b)
	}
}
