package update

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
)

// publicKeyB64 es la clave pública ed25519 (base64) con la que se comprueba la
// firma de SHA256SUMS de cada versión.
//
// TODO(release): aún no hay clave. Genera el par con `go run ./tools/sign -gen`,
// pega aquí la pública y guarda la privada en el secreto CUBIT_SIGNING_KEY de
// GitHub Actions. Mientras esté vacía, la actualización con un clic se
// niega (nunca se instala nada sin firma comprobada).
const publicKeyB64 = ""

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
