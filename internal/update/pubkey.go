package update

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
)

// publicKeyB64 es la clave pública ed25519 (base64) con la que se comprueba la
// firma de SHA256SUMS de cada versión.
//
// La privada está en el secreto CUBIT_SIGNING_KEY de GitHub Actions (nunca en
// el repo). Para cambiarla: `go run ./tools/sign -gen`, pegar aquí la pública y
// cambiar el secreto. Sin clave, la actualización con un clic se niega.
var publicKeyB64 = "M/CJN3S3ZnvFaeWMpGdUgSmKT/i1bjl8KzBZKKfUn0A="

// ErrNoKey: el binario no lleva clave pública (todavía).
var ErrNoKey = errors.New("this build has no update signing key yet; update by hand from the Releases page")

// PublicKey devuelve la clave pública embebida.
func PublicKey() (ed25519.PublicKey, error) {
	if publicKeyB64 == "" {
		return nil, ErrNoKey
	}
	b, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("embedded update public key is malformed")
	}
	return ed25519.PublicKey(b), nil
}

// SetPublicKeyForTest cambia la clave pública (solo para tests) y devuelve cómo
// dejarla como estaba.
func SetPublicKeyForTest(b64 string) (restore func()) {
	old := publicKeyB64
	publicKeyB64 = b64
	return func() { publicKeyB64 = old }
}
