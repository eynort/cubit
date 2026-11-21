package main

import (
	_ "embed"
	"net/http"
)

// Iconos del panel (brand/). Se sirven sin clave: son públicos y el navegador
// (o iOS al guardar el acceso directo) los pide sin la cookie.
var (
	//go:embed brand/favicon.svg
	brandFaviconSVG []byte
	//go:embed brand/png/icon-32.png
	brandIcon32 []byte
	//go:embed brand/png/icon-180.png
	brandIcon180 []byte
)

func brandRoutes(mux *http.ServeMux) {
	serve := func(path, ctype string, body []byte) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ctype)
			w.Header().Set("Cache-Control", "max-age=86400")
			_, _ = w.Write(body)
		})
	}
	serve("/favicon.svg", "image/svg+xml", brandFaviconSVG)
	serve("/favicon.ico", "image/png", brandIcon32)
	serve("/favicon-32.png", "image/png", brandIcon32)
	serve("/apple-touch-icon.png", "image/png", brandIcon180)
}
