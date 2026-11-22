package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func call(ok func(http.ResponseWriter, *http.Request) bool, mod func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/x", nil)
	r.RemoteAddr = "10.0.0.1:5555"
	mod(r)
	w := httptest.NewRecorder()
	if ok(w, r) {
		w.WriteHeader(200)
	}
	return w
}

func TestKeyCheckBearerQueryCookie(t *testing.T) {
	ok := KeyCheck("tok", "missing key: open the URL", "falta la clave: abre la URL")
	if w := call(ok, func(r *http.Request) { r.Header.Set("Authorization", "Bearer tok") }); w.Code != 200 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("bearer: %d, cookies %v", w.Code, w.Result().Cookies())
	}
	if w := call(ok, func(r *http.Request) { r.URL.RawQuery = "t=tok" }); w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("query: %d", w.Code)
	}
	if w := call(ok, func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "mcbot", Value: "tok"}) }); w.Code != 200 {
		t.Fatalf("cookie: %d", w.Code)
	}
	if w := call(ok, func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }); w.Code != 401 || !strings.HasPrefix(w.Body.String(), "wrong key: open the URL") {
		t.Fatalf("mala: %d %q", w.Code, w.Body.String())
	}
	if w := call(ok, func(r *http.Request) {}); w.Code != 401 || !strings.HasPrefix(w.Body.String(), "missing key") {
		t.Fatalf("sin clave: %d %q", w.Code, w.Body.String())
	}
}

func TestKeyCheckRateLimit(t *testing.T) {
	ok := KeyCheck("tok", "en", "es")
	for i := 0; i < maxFails; i++ {
		call(ok, func(r *http.Request) { r.Header.Set("Authorization", "Bearer bad") })
	}
	w := call(ok, func(r *http.Request) { r.Header.Set("Authorization", "Bearer bad2") })
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("esperaba 429, hay %d", w.Code)
	}
	// la clave buena entra siempre (si no, otra web podría dejarte fuera)
	w = call(ok, func(r *http.Request) { r.Header.Set("Authorization", "Bearer tok") })
	if w.Code != 200 {
		t.Fatalf("clave buena bloqueada: %d", w.Code)
	}
	// otra IP no se ve afectada
	w = call(ok, func(r *http.Request) { r.RemoteAddr = "10.0.0.2:1"; r.Header.Set("Authorization", "Bearer nope") })
	if w.Code != 401 {
		t.Fatalf("otra IP: %d", w.Code)
	}
}

func TestKeyCheckCrossSite(t *testing.T) {
	ok := KeyCheck("tok", "en", "es")
	post := func(set func(r *http.Request)) int {
		return call(ok, func(r *http.Request) {
			r.Method = http.MethodPost
			r.Host = "localhost:8080"
			r.AddCookie(&http.Cookie{Name: "mcbot", Value: "tok"})
			set(r)
		}).Code
	}
	if c := post(func(r *http.Request) { r.Header.Set("Origin", "http://evil.example") }); c != 403 {
		t.Fatalf("otra web: %d", c)
	}
	if c := post(func(r *http.Request) { r.Header.Set("Origin", "http://localhost:3000") }); c != 403 {
		t.Fatalf("otro puerto: %d", c)
	}
	if c := post(func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") }); c != 403 {
		t.Fatalf("same-site: %d", c)
	}
	if c := post(func(r *http.Request) { r.Header.Set("Origin", "http://localhost:8080"); r.Header.Set("Sec-Fetch-Site", "same-origin") }); c != 200 {
		t.Fatalf("el propio panel: %d", c)
	}
	if c := post(func(r *http.Request) {}); c != 200 {
		t.Fatalf("sin Origin (curl): %d", c)
	}
}
