package main

// Los mensajes que el panel enseña (errores al guardar, apagar, crear un bot…)
// en el idioma del panel: el panel lo dice en cada petición (cabecera X-Lang:
// en o es); sin ella, inglés. Los errores que llegan al panel se crean con bi
// (los dos textos) y se mandan con httpErr.

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// biErr: un error en los dos idiomas (en el log, el español: como el resto).
type biErr struct{ en, es string }

func (e *biErr) Error() string { return e.es }

// bi: un error con su texto en inglés y en español (con los mismos %v).
func bi(en, es string, args ...any) error {
	return &biErr{en: fmt.Sprintf(en, args...), es: fmt.Sprintf(es, args...)}
}

// reqLang: el idioma del panel que hace la petición ("en" si no lo dice).
func reqLang(r *http.Request) string {
	l := strings.ToLower(r.Header.Get("X-Lang"))
	if l == "" {
		l = strings.ToLower(r.URL.Query().Get("lang"))
	}
	if l == "es" {
		return "es"
	}
	return "en"
}

// langPick: el texto en el idioma de la petición.
func langPick(r *http.Request, en, es string) string {
	if reqLang(r) == "es" {
		return es
	}
	return en
}

// errText: el texto de err en el idioma de la petición (si es bilingüe).
func errText(r *http.Request, err error) string {
	var b *biErr
	if errors.As(err, &b) {
		return langPick(r, b.en, b.es)
	}
	return err.Error()
}

// httpErr manda err con code, en el idioma del panel.
func httpErr(w http.ResponseWriter, r *http.Request, code int, err error) {
	http.Error(w, errText(r, err), code)
}

// httpMsg manda un error con sus dos textos.
func httpMsg(w http.ResponseWriter, r *http.Request, code int, en, es string) {
	http.Error(w, langPick(r, en, es), code)
}
