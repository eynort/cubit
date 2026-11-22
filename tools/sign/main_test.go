package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"

	"cubit/internal/update"
)

// La firma de tools/sign es la que comprueba el actualizador.
func TestSignedByToolVerifiesInUpdater(t *testing.T) {
	pubB64, privB64, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	sums := []byte("abc  cubit-v1-linux-amd64\n")
	sig, err := signMsg(privB64, sums)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := base64.StdEncoding.DecodeString(pubB64)
	if err := update.VerifySignature(ed25519.PublicKey(pub), sums, []byte(sig+"\n")); err != nil {
		t.Fatal(err)
	}
	if update.VerifySignature(ed25519.PublicKey(pub), append(sums, 'x'), []byte(sig)) == nil {
		t.Error("tampered message must fail")
	}
	// también vale la clave de 64 bytes
	seed, _ := base64.StdEncoding.DecodeString(privB64)
	full := base64.StdEncoding.EncodeToString(ed25519.NewKeyFromSeed(seed))
	if s2, err := signMsg(full, sums); err != nil || s2 != sig {
		t.Errorf("64-byte key: %v", err)
	}
	if _, err := signMsg("###", sums); err == nil {
		t.Error("bad key")
	}
}
