package mcserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServerPropsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MCBOT_SERVER_DIR", dir)
	orig := "#Minecraft server properties\nlevel-type=minecraft\\:normal\nmotd=hola\nmax-players=20\ndifficulty=easy\n"
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	restart, err := setServerProps(map[string]string{"motd": "Servidor de Bej: ñandú ✓", "max-players": "8"})
	if err != nil || !restart {
		t.Fatalf("restart=%v err=%v", restart, err)
	}
	p, err := readServerProps()
	if err != nil {
		t.Fatal(err)
	}
	if p["motd"] != "Servidor de Bej: ñandú ✓" || p["max-players"] != "8" || p["level-type"] != "minecraft:normal" || p["difficulty"] != "easy" {
		t.Fatalf("props: %v", p)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "server.properties"))
	if !strings.HasPrefix(string(b), "#Minecraft server properties\nlevel-type=minecraft\\:normal\n") {
		t.Fatalf("lo demás cambió:\n%s", b)
	}
	if _, err := setServerProps(map[string]string{"max-players": "0"}); err == nil {
		t.Fatal("max-players=0 debería fallar")
	}
	if _, err := setServerProps(map[string]string{"rcon.password": "x"}); err == nil {
		t.Fatal("rcon.password no se puede tocar")
	}
}

// TestLabelsInEnglish: el panel enseña las propiedades y gamerules en su idioma.
func TestLabelsInEnglish(t *testing.T) {
	for _, d := range serverProps {
		if d.LabelEn == "" {
			t.Errorf("%s sin etiqueta en inglés", d.Key)
		}
	}
	for _, g := range gamerules {
		if g.LabelEn == "" {
			t.Errorf("%s sin etiqueta en inglés", g.Name)
		}
	}
}
