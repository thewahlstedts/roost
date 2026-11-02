// Command sign signs roost release checksums with an ed25519 key, so
// `roost update` can tell a genuine release from one uploaded by someone who
// only got access to the GitHub repo.
//
//	go run ./tools/sign -gen KEYFILE        # new key: seed to KEYFILE (0600), public key to stdout
//	go run ./tools/sign -key KEYFILE FILE SIGFILE   # sign FILE into SIGFILE
//	go run ./tools/sign FILE SIGFILE        # same, key from $ROOST_SIGNING_KEY
//
// Keys and signatures are base64 (std encoding). The private key is the
// 32-byte ed25519 seed; roost embeds the public key (update.go: releaseKeys).
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 2 && args[0] == "-gen" {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		f, err := os.OpenFile(args[1], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := fmt.Fprintln(f, base64.StdEncoding.EncodeToString(priv.Seed())); err != nil {
			return err
		}
		fmt.Println(base64.StdEncoding.EncodeToString(pub))
		return nil
	}
	key := os.Getenv("ROOST_SIGNING_KEY")
	if len(args) == 4 && args[0] == "-key" {
		b, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		key, args = string(b), args[2:]
	}
	if len(args) != 2 {
		return fmt.Errorf("usage: sign -gen KEYFILE | sign [-key KEYFILE] FILE SIGFILE")
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(key))
	if err != nil || len(seed) != ed25519.SeedSize {
		return fmt.Errorf("signing key must be a base64 ed25519 seed")
	}
	msg, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), msg)
	return os.WriteFile(args[1], []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644)
}
