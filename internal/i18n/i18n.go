// Package i18n: los mensajes que el panel enseña (errores al guardar, apagar,
// crear un bot…) en el idioma del panel: el panel lo dice en cada petición
// (cabecera X-Lang: en o es); sin ella, inglés. Los errores que llegan al
// panel se crean con Errorf (los dos textos) y se mandan con HTTPError.
//
// El idioma de la consola del bot (/lang) se guarda en LangFile.
package i18n

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// LangFile: el idioma del bot (en o es), en su carpeta.
const LangFile = ".lang"

// Error: un error en los dos idiomas (en el log, el español: como el resto).
type Error struct{ EN, ES string }

func (e *Error) Error() string { return e.ES }

// Errorf: un error con su texto en inglés y en español (con los mismos %v).
func Errorf(en, es string, args ...any) error {
	return &Error{EN: fmt.Sprintf(en, args...), ES: fmt.Sprintf(es, args...)}
}

// ReqLang: el idioma del panel que hace la petición ("en" si no lo dice).
func ReqLang(r *http.Request) string {
	l := strings.ToLower(r.Header.Get("X-Lang"))
	if l == "" {
		l = strings.ToLower(r.URL.Query().Get("lang"))
	}
	if l == "es" {
		return "es"
	}
	return "en"
}

// Pick: el texto en el idioma de la petición.
func Pick(r *http.Request, en, es string) string {
	if ReqLang(r) == "es" {
		return es
	}
	return en
}

// ErrText: el texto de err en el idioma de la petición (si es bilingüe).
func ErrText(r *http.Request, err error) string {
	var b *Error
	if errors.As(err, &b) {
		return Pick(r, b.EN, b.ES)
	}
	return err.Error()
}

// HTTPError manda err con code, en el idioma del panel.
func HTTPError(w http.ResponseWriter, r *http.Request, code int, err error) {
	http.Error(w, ErrText(r, err), code)
}

// HTTPMsg manda un error con sus dos textos.
func HTTPMsg(w http.ResponseWriter, r *http.Request, code int, en, es string) {
	http.Error(w, Pick(r, en, es), code)
}
