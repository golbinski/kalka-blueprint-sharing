package identity

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestKeyCreatedOncePrivate(t *testing.T) {
	dir := t.TempDir()
	k1, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := LoadOrCreateKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !k1.Equal(k2) {
		t.Fatal("key changed between loads")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(filepath.Join(dir, KeyFile))
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", info.Mode().Perm())
		}
	}
}
