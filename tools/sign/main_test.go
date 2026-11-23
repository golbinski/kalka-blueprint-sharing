package main

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// keygen writes a secret only you can read and never overwrites one.
func TestKeygen(t *testing.T) {
	p := filepath.Join(t.TempDir(), "key")
	if err := run([]string{"-keygen", p}, os.Getenv); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(p)
	if seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw))); err != nil || len(seed) != 32 {
		t.Fatalf("seed %v %d", err, len(seed))
	}
	if st, _ := os.Stat(p); runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode())
	}
	if err := run([]string{"-keygen", p}, os.Getenv); err == nil {
		t.Error("overwrote a key")
	}
}

// A key kalka does not have built in never signs a release, nor does a
// missing key or tag.
func TestSignRefuses(t *testing.T) {
	dir := t.TempDir()
	sums := filepath.Join(dir, "SHA256SUMS")
	os.WriteFile(sums, []byte(strings.Repeat("a", 64)+"  kalka.zip\n"), 0o644)
	env := func(k string) string {
		if k == "KALKA_RELEASE_KEY" {
			return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
		}
		return ""
	}
	for _, c := range []struct {
		args []string
		env  func(string) string
		want string
	}{
		{[]string{"-tag", "v1.0.0", sums}, env, "not among"},
		{[]string{"-tag", "v1.0.0", sums}, func(string) string { return "" }, "no key"},
		{[]string{sums}, env, "usage"},
	} {
		if err := run(c.args, c.env); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v", c.args, err)
		}
	}
	if _, err := os.Stat(sums + ".minisig"); err == nil {
		t.Error("wrote a signature")
	}
}
