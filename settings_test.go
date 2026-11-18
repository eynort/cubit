package main

import "testing"

// TestRenameWithoutServer: cambiar el nombre no obliga a escribir el servidor
// (vacío = el de siempre), ni por /settings ni por /settings/name.
func TestRenameWithoutServer(t *testing.T) {
	cfgMu.Lock()
	oldCfg, oldPath, oldOwners := cfg, cfgPath, remoteOwners
	cfg, cfgPath = botConfig{Server: "minecraft", Name: "Viejo_Bot", AI: &aiSettings{URL: "", API: "ollama", Model: "m"}}, ""
	cfgMu.Unlock()
	defer func() { cfgMu.Lock(); cfg, cfgPath, remoteOwners = oldCfg, oldPath, oldOwners; cfgMu.Unlock() }()

	restart, err := applySettings(panelSettings{Name: "Nuevo_Bot", AIAPI: "ollama", AIModel: "m"})
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

// TestStorageChestNeedsBot: sin bot en marcha no se puede marcar el cofre
// (el almacén es del bot), y lo dice claro.
func TestStorageChestNeedsBot(t *testing.T) {
	if current.Load() != nil {
		t.Skip("hay un bot")
	}
	if err := setStorageChest("10 64 -20"); err == nil {
		t.Fatal("sin bot tendría que fallar")
	}
}
