// Package web: lo que comparten el panel de un bot (--monitor) y el del hub
// (--hub): la página (panel.html, embebida en el binario), la clave de acceso
// y la dirección con la que se anuncia.
package web

import (
	"crypto/subtle"
	_ "embed"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"cubit/internal/i18n"
)

// PanelHTML es la página del panel: la misma para un bot solo y para el hub.
//
//go:embed panel.html
var PanelHTML []byte

// KeyCheck: la comprobación de la clave del panel: la cabecera
// "Authorization: Bearer <clave>" (para la API), ?t=… o la cookie (que se pone
// al entrar desde el navegador). Sin ella, 401 con el mensaje en y es; y
// quien falla muchas veces seguidas (más de maxFails por minuto desde una IP)
// recibe 429 un rato, aunque acierte.
func KeyCheck(token, en, es string) func(http.ResponseWriter, *http.Request) bool {
	lim := &failLimiter{hits: map[string][]time.Time{}}
	return func(w http.ResponseWriter, r *http.Request) bool {
		ip := clientIP(r)
		if lim.blocked(ip) {
			w.Header().Set("Retry-After", "60")
			i18n.HTTPMsg(w, r, http.StatusTooManyRequests, "too many wrong keys, wait a minute", "demasiadas claves erróneas, espera un minuto")
			return false
		}
		t, bearer := "", false
		if h := r.Header.Get("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
			t, bearer = strings.TrimSpace(h[7:]), true
		}
		if t == "" {
			t = r.URL.Query().Get("t")
		}
		if t == "" {
			if c, err := r.Cookie("mcbot"); err == nil {
				t = c.Value
			}
		}
		if subtle.ConstantTimeCompare([]byte(t), []byte(token)) != 1 {
			if t != "" {
				lim.fail(ip)
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="cubit"`)
			i18n.HTTPMsg(w, r, http.StatusUnauthorized, en, es)
			return false
		}
		if !bearer {
			http.SetCookie(w, &http.Cookie{Name: "mcbot", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 30 * 24 * 3600})
		}
		return true
	}
}

// maxFails: claves erróneas por minuto y por IP antes del 429.
const maxFails = 20

type failLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func (l *failLimiter) recent(ip string, now time.Time) []time.Time {
	var keep []time.Time
	for _, t := range l.hits[ip] {
		if now.Sub(t) < time.Minute {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		delete(l.hits, ip)
	} else {
		l.hits[ip] = keep
	}
	return keep
}

func (l *failLimiter) blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(ip, time.Now())) >= maxFails
}

func (l *failLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.recent(ip, now)
	l.hits[ip] = append(l.hits[ip], now)
	if len(l.hits) > 1000 { // (no crecer sin fin con IPs distintas)
		for k := range l.hits {
			if len(l.recent(k, now)) == 0 {
				continue
			}
		}
	}
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// LocalIP: la IP de este equipo en la red (para la URL del panel).
func LocalIP() string {
	c, err := net.Dial("udp", "192.0.2.1:9") // (no manda nada: solo elige la interfaz)
	if err != nil {
		return "127.0.0.1"
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String()
}
