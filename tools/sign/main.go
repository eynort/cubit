// sign: firma SHA256SUMS de una release con ed25519 (la firma que comprueba el
// actualizador, internal/update/pubkey.go).
//
//	go run ./tools/sign -gen                      # crea un par de claves (no se guarda nada)
//	CUBIT_SIGNING_KEY=<privada> go run ./tools/sign -in dist/SHA256SUMS -out dist/SHA256SUMS.sig
//
// La clave privada es base64 (de la semilla de 32 bytes o de la clave de 64);
// la firma sale en base64. Nunca se guarda ninguna clave en el repositorio.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

// privateKey lee una clave privada en base64 (semilla de 32 bytes o clave completa de 64).
func privateKey(b64 string) (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, errors.New("the private key is not valid base64")
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(raw), nil
	}
	return nil, fmt.Errorf("the private key has %d bytes (expected %d or %d)", len(raw), ed25519.SeedSize, ed25519.PrivateKeySize)
}

// signMsg firma msg y devuelve la firma en base64.
func signMsg(privB64 string, msg []byte) (string, error) {
	k, err := privateKey(privB64)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(k, msg)), nil
}

// generate crea un par: (pública, privada) en base64.
func generate() (pub, priv string, err error) {
	p, k, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(p), base64.StdEncoding.EncodeToString(k.Seed()), nil
}

func main() {
	gen := flag.Bool("gen", false, "crea un par de claves y muestra la pública (para pubkey.go) y la privada (para el secreto CUBIT_SIGNING_KEY)")
	in := flag.String("in", "dist/SHA256SUMS", "el archivo que firmar")
	out := flag.String("out", "", "dónde dejar la firma (por defecto <in>.sig)")
	env := flag.String("env", "CUBIT_SIGNING_KEY", "la variable con la clave privada en base64")
	flag.Parse()
	if *gen {
		pub, priv, err := generate()
		if err != nil {
			fmt.Fprintln(os.Stderr, "sign:", err)
			os.Exit(1)
		}
		fmt.Println("public  (paste into internal/update/pubkey.go, publicKeyB64):")
		fmt.Println(pub)
		fmt.Println()
		fmt.Println("private (GitHub secret CUBIT_SIGNING_KEY; keep it secret, never commit it):")
		fmt.Println(priv)
		return
	}
	key := os.Getenv(*env)
	if key == "" {
		fmt.Fprintf(os.Stderr, "sign: %s is empty\n", *env)
		os.Exit(1)
	}
	msg, err := os.ReadFile(*in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
	sig, err := signMsg(key, msg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
	dst := *out
	if dst == "" {
		dst = *in + ".sig"
	}
	if err := os.WriteFile(dst, []byte(sig+"\n"), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
	fmt.Println("signed", *in, "->", dst)
}
