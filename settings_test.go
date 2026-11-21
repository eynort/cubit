package main

import (
	"testing"

	"cubit/internal/config"
)

// TestRenameWithoutServer: cambiar el nombre no obliga a escribir el servidor
// (vacío = el de siempre), ni por /settings ni por /settings/name.
func TestRenameWithoutServer(t *testing.T) {
	cfgMu.Lock()
	oldCfg, oldPath, oldOwners := cfg, cfgPath, remoteOwners
	cfg, cfgPath = config.Bot{Server: "minecraft", Name: "Viejo_Bot"}, ""
	cfgMu.Unlock()
	defer func() { cfgMu.Lock(); cfg, cfgPath, remoteOwners = oldCfg, oldPath, oldOwners; cfgMu.Unlock() }()

	restart, err := applySettings(panelSettings{Name: "Nuevo_Bot"})
	if err != nil || !restart || cfg.Server != "minecraft" || cfg.Name != "Nuevo_Bot" {
		t.Fatalf("sin servidor: restart=%v err=%v cfg=%s/%s", restart, err, cfg.Server, cfg.Name)
	}
	if restart, err := renameBot("Otro_Bot"); err != nil || !restart || cfg.Name != "Otro_Bot" || cfg.Server != "minecraft" {
		t.Fatalf("renameBot: %v %v %s", restart, err, cfg.Name)
	}
	if restart, err := renameBot("Otro_Bot"); err != nil || restart {
		t.Fatalf("el mismo nombre no reinicia: %v %v", restart, err)
	}
	if _, err := renameBot("a b"); err == nil {
		t.Fatal("nombre inválido aceptado")
	}
}
