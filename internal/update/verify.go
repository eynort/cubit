package update

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// ParseSums lee un SHA256SUMS ("<hash>  <archivo>" por línea) a nombre → hash.
func ParseSums(b []byte) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && len(f[0]) == 64 {
			out[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
		}
	}
	return out
}

// VerifySignature comprueba que sig (64 bytes crudos, o en base64) es la firma
// ed25519 de msg con la clave pública pub.
func VerifySignature(pub ed25519.PublicKey, msg, sig []byte) error {
	if len(pub) != ed25519.PublicKeySize {
		return ErrNoKey
	}
	s := sig
	if len(s) != ed25519.SignatureSize {
		d, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
		if err != nil {
			return errors.New("signature is not valid base64")
		}
		s = d
	}
	if len(s) != ed25519.SignatureSize || !ed25519.Verify(pub, msg, s) {
		return errors.New("bad signature on SHA256SUMS: refusing to install")
	}
	return nil
}

// SumHex es el SHA256 de b en hexadecimal.
func SumHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// VerifyChecksum compara el hash calculado de name con el de sums.
func VerifyChecksum(sums map[string]string, name, got string) error {
	want, ok := sums[name]
	if !ok {
		return fmt.Errorf("%s is not in SHA256SUMS", name)
	}
	if !strings.EqualFold(want, got) {
		return fmt.Errorf("checksum mismatch for %s", name)
	}
	return nil
}
