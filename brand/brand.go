// Package brand: el logo y los iconos de Cubit. Los del panel van embebidos
// en el binario (Routes); los demás (svg, png, promo/) son para los README y
// las páginas del proyecto.
package brand

import (
	_ "embed"
	"net/http"
)

// Iconos del panel. Se sirven sin clave: son públicos y el navegador (o iOS al
// guardar el acceso directo) los pide sin la cookie.
var (
	//go:embed favicon.svg
	faviconSVG []byte
	//go:embed png/icon-32.png
	icon32 []byte
	//go:embed png/icon-180.png
	icon180 []byte
)

// Routes sirve los iconos del panel en mux.
func Routes(mux *http.ServeMux) {
	serve := func(path, ctype string, body []byte) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ctype)
			w.Header().Set("Cache-Control", "max-age=86400")
			_, _ = w.Write(body)
		})
	}
	serve("/favicon.svg", "image/svg+xml", faviconSVG)
	serve("/favicon.ico", "image/png", icon32)
	serve("/favicon-32.png", "image/png", icon32)
	serve("/apple-touch-icon.png", "image/png", icon180)
}
