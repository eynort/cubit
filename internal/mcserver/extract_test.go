package mcserver

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func writeTarGz(t *testing.T, path string, entries []tar.Header) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, h := range entries {
		h := h
		if h.Typeflag == tar.TypeReg {
			h.Size = 5
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			tw.Write([]byte("pwned"))
		}
	}
	tw.Close()
	gz.Close()
	f.Close()
}

// una cadena de enlaces relativos (cada uno "dentro" por sí solo) no puede
// servir para escribir fuera de la carpeta
func TestExtractTarGzSymlinkChain(t *testing.T) {
	if os.Getenv("GOOS") == "windows" {
		t.Skip()
	}
	root := t.TempDir()
	dest := filepath.Join(root, "a", "b", "java")
	arc := filepath.Join(root, "jre.tar.gz")
	writeTarGz(t, arc, []tar.Header{
		{Name: "jdk/x/y/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "jdk/x/y/up", Typeflag: tar.TypeSymlink, Linkname: ".."},
		{Name: "jdk/x/y/up/esc", Typeflag: tar.TypeSymlink, Linkname: "../.."},
		{Name: "jdk/x/y/up/esc/PWNED", Typeflag: tar.TypeReg, Mode: 0o644},
	})
	err := extractTarGz(arc, dest)
	if err == nil {
		t.Fatal("se esperaba un error")
	}
	matches, _ := filepath.Glob(filepath.Join(root, "**", "PWNED"))
	m2, _ := filepath.Glob(filepath.Join(root, "*", "PWNED"))
	m3, _ := filepath.Glob(filepath.Join(root, "PWNED"))
	if len(matches)+len(m2)+len(m3) > 0 {
		t.Fatalf("escribió fuera: %v %v %v", matches, m2, m3)
	}
}

// los enlaces normales de un JRE (legal/x -> ../java.base/y) siguen funcionando
func TestExtractTarGzNormalSymlink(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "java")
	arc := filepath.Join(root, "jre.tar.gz")
	writeTarGz(t, arc, []tar.Header{
		{Name: "jdk/legal/java.base/LICENSE", Typeflag: tar.TypeReg, Mode: 0o644},
		{Name: "jdk/legal/java.sql/LICENSE", Typeflag: tar.TypeSymlink, Linkname: "../java.base/LICENSE"},
	})
	if err := extractTarGz(arc, dest); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "legal", "java.sql", "LICENSE"))
	if err != nil || string(b) != "pwned" {
		t.Fatalf("%q %v", b, err)
	}
}
