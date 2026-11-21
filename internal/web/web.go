// Package web: lo que comparten el panel de un bot (--monitor) y el del hub
// (--hub): la página (panel.html, embebida en el binario), la clave de acceso
// y la dirección con la que se anuncia.
package web

import (
	"crypto/subtle"
	_ "embed"
	"net"
	"net/http"

	"cubit/internal/i18n"
)

// PanelHTML es la página del panel: la misma para un bot solo y para el hub.
//
//go:embed panel.html
var PanelHTML []byte

// KeyCheck: la comprobación de la clave del panel (?t=… o la cookie, que se
// pone al entrar). Sin ella, 401 con el mensaje en y es.
func KeyCheck(token, en, es string) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		t := r.URL.Query().Get("t")
		if t == "" {
			if c, err := r.Cookie("mcbot"); err == nil {
				t = c.Value
			}
		}
		if subtle.ConstantTimeCompare([]byte(t), []byte(token)) != 1 {
			i18n.HTTPMsg(w, r, http.StatusUnauthorized, en, es)
			return false
		}
		http.SetCookie(w, &http.Cookie{Name: "mcbot", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 30 * 24 * 3600})
		return true
	}
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
