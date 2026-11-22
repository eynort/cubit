package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"cubit/internal/update"
)

const fakeRelease = `{"tag_name":"v2.0.0","html_url":"https://example.invalid/notes","assets":[{"name":"SHA256SUMS","browser_download_url":"https://example.invalid/s"},{"name":"SHA256SUMS.sig","browser_download_url":"https://example.invalid/g"}]}`

func TestUpdateRoutes(t *testing.T) {
	t.Chdir(t.TempDir()) // (hub.json se guarda en la carpeta actual)
	var hits int
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; fmt.Fprint(w, fakeRelease) }))
	defer gh.Close()

	h := &hub{bots: map[string]*hubBot{}, opts: Options{Version: "v1.0.0"}}
	h.initUpdates()
	h.up.chk.APIURL, h.up.chk.Client, h.up.chk.Logf = gh.URL, gh.Client(), func(string, ...any) {}
	mux := http.NewServeMux()
	h.updateRoutes(mux, func(http.ResponseWriter, *http.Request) bool { return true }, func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) })

	do := func(path string) (*httptest.ResponseRecorder, update.Status) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"enabled":false}`)))
		var st update.Status
		json.Unmarshal(w.Body.Bytes(), &st)
		return w, st
	}

	// sin comprobar aún: nada que aplicar
	if w, _ := do("/api/update/apply"); w.Code != http.StatusConflict {
		t.Errorf("apply without a release: %d", w.Code)
	}
	// comprobar ahora
	w, st := do("/api/update/check")
	if w.Code != 200 || !st.Available || st.Latest != "v2.0.0" || hits != 1 {
		t.Fatalf("check: %d %+v hits=%d", w.Code, st, hits)
	}
	// un paquete .deb/.rpm/scoop… no se reemplaza solo
	old := update.Method
	defer func() { update.Method = old }()
	update.Method = "deb"
	if w, _ := do("/api/update/apply"); w.Code != http.StatusConflict {
		t.Errorf("apply on a deb install must be refused, got %d", w.Code)
	}
	if v := h.updateView(0); v.Plan.Kind != "command" || !strings.HasPrefix(v.Plan.Command, "sudo apt install") {
		t.Errorf("deb plan: %+v", v.Plan)
	}
	// script con la carpeta escribible, pero sin clave de firma en este binario: se niega
	update.Method = "script"
	defer update.SetPublicKeyForTest("")()
	h.up.wrOnce.Do(func() {})
	h.up.writable = true
	if w, _ := do("/api/update/apply"); w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "signing") && !strings.Contains(w.Body.String(), "clave") {
		t.Errorf("apply without a signing key: %d %s", w.Code, w.Body.String())
	}
	// el ajuste: apagado, se guarda y la comprobación automática ya no pide nada
	if _, st := do("/api/update/setting"); st.Enabled {
		t.Error("setting should be off")
	}
	if b, _ := os.ReadFile(hubFile); !strings.Contains(string(b), `"no_update_check": true`) {
		t.Errorf("hub.json: %s", b)
	}
	_ = h.up.chk.Check(t.Context(), false)
	if hits != 1 {
		t.Errorf("automatic check with the setting off made a request (hits=%d)", hits)
	}
}
