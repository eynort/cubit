package automation

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"cubit/internal/i18n"
)

// TestTaskNamesEnglish: con el bot en inglés, el nombre de cada tarea y su
// estado en /status y el panel no llevan español (en español, como antes).
func TestTaskNamesEnglish(t *testing.T) {
	lang := "en"
	i18n.SetLang(func() string { return lang })
	defer i18n.SetLang(nil)
	for name := range taskLabels {
		lang = "en"
		if w := i18n.SpanishWords(Label(name)); len(w) > 0 {
			t.Errorf("%s: %q lleva español %v", name, Label(name), w)
		}
		lang = "es"
		if Label(name) != name {
			t.Errorf("%s en español: %q", name, Label(name))
		}
	}
	lang = "en"
	r := New(Deps{})
	release := make(chan struct{})
	set := make(chan struct{})
	r.Start(context.Background(), "talar", func(j *Job) error {
		j.StatusT("looking for trees (%d chopped)", "buscando árboles (%d talados)", 3)
		close(set)
		<-release
		return nil
	})
	<-set
	if st := r.Status(); st != "chop: looking for trees (3 chopped)" {
		t.Errorf("estado en inglés: %q", st)
	}
	lang = "es"
	if st := r.StatusEN(); st != "chop: looking for trees (3 chopped)" {
		t.Errorf("StatusEN con el bot en español: %q", st)
	}
	close(release)
	r.Stop()
}

// TestStatusTextsEnglish: los estados de las tareas (lo que sale en /status y
// en el panel) se escriben con StatusT, y su texto inglés no lleva español.
// Un Status("…") con un texto en español a secas saldría así con /lang en.
func TestStatusTextsEnglish(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	n := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, _ := strconv.Unquote(lit.Value)
			switch sel.Sel.Name {
			case "StatusT", "LogT":
				n++
				if w := i18n.SpanishWords(s); len(w) > 0 {
					t.Errorf("%s: %s(%q) lleva español %v", fset.Position(lit.Pos()), sel.Sel.Name, s, w)
				}
			case "Status":
				if w := i18n.SpanishWords(s); len(w) > 0 {
					t.Errorf("%s: Status(%q) en español: con StatusT(inglés, español)", fset.Position(lit.Pos()), s)
				}
			}
			return true
		})
	}
	if n < 30 {
		t.Errorf("solo %d StatusT/LogT", n)
	}
}
