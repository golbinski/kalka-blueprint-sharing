// Command sign signs a release's SHA256SUMS with the release key (spec
// 24.2), as the release workflow's last job does:
//
//	KALKA_RELEASE_KEY=<base64 seed> go run ./tools/sign -tag v0.3.1 dist/SHA256SUMS
//
// writes dist/SHA256SUMS.minisig, checked against the keys kalka has built
// in, so a release is never signed with a key the updater would refuse.
//
//	go run ./tools/sign -keygen <file>
//
// makes a new key: the secret seed goes into <file> (created, never
// overwritten, readable by you only), the public key is printed for
// internal/update.ReleaseKeys and SECURITY.md.
package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/golbinski/kalka/internal/update"
)

func main() {
	if err := run(os.Args[1:], os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string) error {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	keygen := fs.String("keygen", "", "make a new key, its secret written to this file")
	tag := fs.String("tag", "", "the release's tag, v<version>")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keygen != "" {
		seed := make([]byte, 32)
		if _, err := rand.Read(seed); err != nil {
			return err
		}
		_, pub, _ := update.KeyFor(seed)
		f, err := os.OpenFile(*keygen, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(f, base64.StdEncoding.EncodeToString(seed)); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		fmt.Println(pub.String())
		return nil
	}
	if fs.NArg() != 1 || !strings.HasPrefix(*tag, "v") {
		return errors.New("usage: sign -tag v<version> <SHA256SUMS>")
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(getenv("KALKA_RELEASE_KEY")))
	if err != nil || len(seed) == 0 {
		return errors.New("KALKA_RELEASE_KEY holds no key")
	}
	priv, pub, err := update.KeyFor(seed)
	if err != nil {
		return err
	}
	if !slices.Contains(update.ReleaseKeys, pub.String()) {
		return fmt.Errorf("the key in KALKA_RELEASE_KEY (%s) is not among internal/update.ReleaseKeys", pub.String())
	}
	sums := fs.Arg(0)
	msg, err := os.ReadFile(sums)
	if err != nil {
		return err
	}
	if len(update.Sums(msg)) == 0 {
		return fmt.Errorf("%s lists no files", sums)
	}
	sig := update.Sign(priv, pub, msg, update.TrustedComment(*tag))
	keys := []update.PublicKey{pub}
	if _, err := update.Verify(msg, sig, keys); err != nil {
		return err
	}
	if err := os.WriteFile(sums+".minisig", sig, 0o644); err != nil {
		return err
	}
	fmt.Printf("signed %s for %s with %s\n", sums, *tag, pub.String())
	return nil
}
