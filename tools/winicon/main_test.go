package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/golbinski/kalka/internal/winres"
)

// The committed resources hold the app icon as the generator draws it
// now: a changed icon or resource layout needs `go generate
// ./cmd/kalka-desktop`. Pixels are compared with a small tolerance, as
// floating-point rounding differs between architectures (arm64 fuses
// multiply-adds).
func TestObjectsCommitted(t *testing.T) {
	files, err := Objects()
	if err != nil {
		t.Fatal(err)
	}
	for name, fresh := range files {
		committed, err := os.ReadFile(filepath.Join("..", "..", "cmd", "kalka-desktop", name))
		if err != nil {
			t.Fatalf("%v; run go generate ./cmd/kalka-desktop", err)
		}
		if bytes.Equal(committed, fresh) {
			continue
		}
		for _, id := range winres.GroupIDs {
			got, err := winres.ObjectIcon(committed, id)
			if err != nil {
				t.Fatalf("%s: %v; run go generate ./cmd/kalka-desktop", name, err)
			}
			want, _ := winres.ObjectIcon(fresh, id)
			if len(got) != len(want) {
				t.Fatalf("%s: %d sizes, want %d; run go generate ./cmd/kalka-desktop", name, len(got), len(want))
			}
			for i := range want {
				if got[i].Size != want[i].Size || !near(t, got[i].PNG, want[i].PNG) {
					t.Errorf("%s: the %dpx icon is stale; run go generate ./cmd/kalka-desktop", name, want[i].Size)
				}
			}
		}
	}
}

// near: the same size, and no channel more than 2 apart.
func near(t *testing.T, a, b []byte) bool {
	t.Helper()
	ia, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	ib, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if ia.Bounds() != ib.Bounds() {
		return false
	}
	r := ia.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if !close4(ia.At(x, y), ib.At(x, y)) {
				return false
			}
		}
	}
	return true
}

func close4(a, b interface{ RGBA() (r, g, b, a uint32) }) bool {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	for _, d := range [][2]uint32{{ar, br}, {ag, bg}, {ab, bb}, {aa, ba}} {
		lo, hi := d[0], d[1]
		if lo > hi {
			lo, hi = hi, lo
		}
		if hi-lo > 2*257 {
			return false
		}
	}
	return true
}
