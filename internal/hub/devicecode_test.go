package hub

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoginFromFile(t *testing.T) {
	dir := t.TempDir()
	if loginFrom(dir) != nil {
		t.Fatal("sin archivo no hay código")
	}
	os.MkdirAll(filepath.Join(dir, ".microsoft"), 0o700)
	put := func(s string) { os.WriteFile(filepath.Join(dir, ".microsoft", DeviceCodeFile), []byte(s), 0o600) }
	put(`{"url":"https://www.microsoft.com/link","code":"AB12CD34"}`)
	if l := loginFrom(dir); l == nil || l.Code != "AB12CD34" {
		t.Fatalf("código válido: %+v", l)
	}
	for _, bad := range []string{
		`{"url":"javascript:alert(1)//microsoft.com","code":"AB12CD34"}`,
		`{"url":"http://www.microsoft.com/link","code":"AB12CD34"}`,
		`{"url":"https://evil.example/link","code":"AB12CD34"}`,
		`{"url":"https://microsoft.com.evil.example/","code":"AB12CD34"}`,
		`{"url":"https://user@evil.example/","code":"AB12CD34"}`,
		`{"url":"https://www.microsoft.com/link","code":"<b>x</b>"}`,
		`no es json`,
	} {
		put(bad)
		if l := loginFrom(dir); l != nil {
			t.Errorf("aceptado: %s -> %+v", bad, l)
		}
	}
}
