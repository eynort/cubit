package update

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// MarkerFile: el archivo de la carpeta de datos con el método de instalación
// (lo escribe install.sh); sirve si el binario no lo lleva (Method vacío).
const MarkerFile = "install-method"

var knownMethods = map[string]bool{"script": true, "deb": true, "rpm": true, "scoop": true, "winget": true, "zip": true, "docker": true}

// EffectiveMethod: Method (ldflags) o, si está vacío, el marcador install-method
// de la carpeta de datos (o junto al binario). "" = desarrollo / desconocido.
func EffectiveMethod(dataDir string) string {
	if m := strings.ToLower(strings.TrimSpace(Method)); m != "" {
		return m
	}
	dirs := []string{dataDir}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	for _, d := range dirs {
		if b, err := os.ReadFile(filepath.Join(d, MarkerFile)); err == nil {
			if m := strings.ToLower(strings.TrimSpace(string(b))); knownMethods[m] {
				return m
			}
		}
	}
	return ""
}

// Plan: qué hace el panel con una versión nueva.
type Plan struct {
	Kind    string `json:"kind"`              // self (un clic), command (hay que ejecutar Command) o manual (ir a la página de la release)
	Command string `json:"command,omitempty"` // para command
	File    string `json:"file,omitempty"`    // el paquete que descargar antes (deb, rpm)
}

// Writable: se puede crear un archivo en dir (para sustituir el binario).
func Writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".cubit-w-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	_ = os.Remove(name)
	return true
}

// PlanFor decide según el método de instalación; writable: el binario se puede
// sustituir (su carpeta admite escritura); rel puede ser nil (sin versión nueva).
func PlanFor(method string, writable bool, rel *Release) Plan {
	switch method {
	case "script", "zip", "":
		if writable {
			return Plan{Kind: "self"}
		}
		return Plan{Kind: "manual"}
	case "deb":
		name, url := pkgAsset(rel, ".deb", map[string]string{"amd64": "amd64", "arm64": "arm64", "arm": "armhf"})
		return Plan{Kind: "command", Command: "sudo apt install ./" + name, File: url}
	case "rpm":
		name, url := pkgAsset(rel, ".rpm", map[string]string{"amd64": "x86_64", "arm64": "aarch64", "arm": "armv7hl"})
		return Plan{Kind: "command", Command: "sudo dnf install ./" + name, File: url}
	case "scoop":
		return Plan{Kind: "command", Command: "scoop update cubit"}
	case "winget":
		return Plan{Kind: "command", Command: "winget upgrade Cubit"}
	case "docker":
		return Plan{Kind: "command", Command: "./up.sh update"}
	}
	return Plan{Kind: "manual"}
}

// pkgAsset: el paquete de esta arquitectura en la release (o un nombre por defecto).
func pkgAsset(rel *Release, ext string, arch map[string]string) (name, url string) {
	want := arch[runtime.GOARCH]
	if want == "" {
		want = runtime.GOARCH
	}
	tag := "X.Y.Z"
	if rel != nil {
		tag = short(rel.Tag)
		for _, a := range rel.Assets {
			if strings.HasSuffix(a.Name, ext) && strings.Contains(a.Name, want) {
				return a.Name, a.URL
			}
		}
	}
	sep := "_"
	return "cubit" + sep + tag + sep + want + ext, ""
}
