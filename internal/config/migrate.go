package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Versión del formato de hub.json y de bot.json ("schema": N). Se sube al
// cambiar el formato de forma que una versión vieja no lo entendería, y se
// añade la migración N-1 → N a hubMigrations / botMigrations.
const (
	HubSchema = 1
	BotSchema = 1
)

// Migration pasa un archivo del esquema i+1 al i+2 (la posición i de la lista
// lleva del esquema i+1 al i+2; el esquema 0 = archivo anterior a "schema",
// que es el 1: no necesita cambios).
type Migration func(m map[string]any) error

var (
	hubMigrations []Migration // (de momento, ninguna: el esquema 1 es el primero)
	botMigrations []Migration
)

// Migrate lleva el JSON raw al esquema current aplicando las migraciones que
// falten. Devuelve el JSON nuevo, el esquema del que partía y si cambió algo.
// Un archivo de un esquema más nuevo que current (se bajó de versión) no se
// toca: devuelve error y el llamador decide.
func Migrate(raw []byte, current int, migs []Migration) (out []byte, from int, changed bool, err error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return raw, 0, false, err
	}
	if m == nil {
		return raw, 0, false, fmt.Errorf("not a JSON object")
	}
	from = 1 // (sin "schema": el primero)
	if v, ok := m["schema"].(float64); ok {
		from = int(v)
	}
	if from > current {
		return raw, from, false, fmt.Errorf("schema %d is newer than this version understands (%d)", from, current)
	}
	if from == current && m["schema"] != nil {
		return raw, from, false, nil
	}
	for s := from; s < current; s++ {
		i := s - 1 // migs[i]: del esquema i+1 al i+2
		if i >= 0 && i < len(migs) && migs[i] != nil {
			if err := migs[i](m); err != nil {
				return raw, from, false, fmt.Errorf("migration %d→%d: %w", s, s+1, err)
			}
		}
	}
	m["schema"] = current
	out, err = json.MarshalIndent(m, "", "  ")
	if err != nil {
		return raw, from, false, err
	}
	return append(out, '\n'), from, true, nil
}

// MigrateHub y MigrateBot: Migrate con las migraciones de cada archivo.
func MigrateHub(raw []byte) ([]byte, int, bool, error) {
	return Migrate(raw, HubSchema, hubMigrations)
}
func MigrateBot(raw []byte) ([]byte, int, bool, error) {
	return Migrate(raw, BotSchema, botMigrations)
}

// MigrateFile migra el archivo en su sitio (con una copia <archivo>.v<N>.bak del
// original si cambia algo). Un archivo que no existe no es un error.
func MigrateFile(path string, fn func([]byte) ([]byte, int, bool, error)) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	out, from, changed, err := fn(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if !changed {
		return nil
	}
	if err := os.WriteFile(fmt.Sprintf("%s.v%d.bak", path, from), raw, 0o600); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
