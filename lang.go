package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"

	"cubit/internal/i18n"
)

// Idioma de las explicaciones (ayuda, sugerencias, errores de uso de las
// órdenes): inglés por defecto, español con /lang es. Las órdenes y sus
// parámetros se escriben siempre en inglés. Se guarda en i18n.LangFile.

var curLang atomic.Pointer[string]

// languages son los idiomas que hay.
var languages = map[string]string{"en": "English", "es": "Español"}

func lang() string {
	if l := curLang.Load(); l != nil {
		return *l
	}
	l := "en"
	if b, err := os.ReadFile(i18n.LangFile); err == nil {
		if s := strings.TrimSpace(string(b)); languages[s] != "" {
			l = s
		}
	}
	curLang.Store(&l)
	return l
}

func setLang(l string) {
	curLang.Store(&l)
	_ = os.WriteFile(i18n.LangFile, []byte(l+"\n"), 0o644)
}

// text es una explicación en los dos idiomas.
type text struct{ en, es string }

func (t text) String() string {
	if lang() == "es" && t.es != "" {
		return t.es
	}
	return t.en
}

// tr elige entre la versión inglesa y la española.
func tr(en, es string) string { return text{en, es}.String() }

// errf es un error de uso en el idioma elegido.
func errf(en, es string, args ...any) error { return fmt.Errorf(tr(en, es), args...) }

// cmdLang: /lang [en|es] — el idioma de las explicaciones.
func cmdLang(b *Bot, args []string) error {
	if len(args) == 0 {
		log.Print(tr("language: ", "idioma: ") + languages[lang()] + " (" + lang() + ")")
		return nil
	}
	l := strings.ToLower(args[0])
	if languages[l] == "" {
		return errf("unknown language %q (en, es)", "idioma %q desconocido (en, es)", args[0])
	}
	setLang(l)
	log.Print(tr("language: English (commands are always in English)", "idioma: español (las órdenes se escriben siempre en inglés)"))
	return nil
}
