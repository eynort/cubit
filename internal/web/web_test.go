package web

import (
	"net/http"
	"net/http/httptest"
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
	ok := KeyCheck("tok", "en", "es")
	if w := call(ok, func(r *http.Request) { r.Header.Set("Authorization", "Bearer tok") }); w.Code != 200 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("bearer: %d, cookies %v", w.Code, w.Result().Cookies())
	}
	if w := call(ok, func(r *http.Request) { r.URL.RawQuery = "t=tok" }); w.Code != 200 || len(w.Result().Cookies()) != 1 {
		t.Fatalf("query: %d", w.Code)
	}
	if w := call(ok, func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "mcbot", Value: "tok"}) }); w.Code != 200 {
		t.Fatalf("cookie: %d", w.Code)
	}
	if w := call(ok, func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }); w.Code != 401 {
		t.Fatalf("mala: %d", w.Code)
	}
	if w := call(ok, func(r *http.Request) {}); w.Code != 401 {
		t.Fatalf("sin clave: %d", w.Code)
	}
}

func TestKeyCheckRateLimit(t *testing.T) {
	ok := KeyCheck("tok", "en", "es")
	for i := 0; i < maxFails; i++ {
		call(ok, func(r *http.Request) { r.Header.Set("Authorization", "Bearer bad") })
	}
	w := call(ok, func(r *http.Request) { r.Header.Set("Authorization", "Bearer tok") })
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("esperaba 429, hay %d", w.Code)
	}
	// otra IP no se ve afectada
	w = call(ok, func(r *http.Request) { r.RemoteAddr = "10.0.0.2:1"; r.Header.Set("Authorization", "Bearer tok") })
	if w.Code != 200 {
		t.Fatalf("otra IP: %d", w.Code)
	}
}
