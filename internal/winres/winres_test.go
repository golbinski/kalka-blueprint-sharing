package winres

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The object, linked by the Go toolchain into a Windows program, gives an
// executable whose icon group holds the images, under every group id: the
// linker relocated each data entry to the right bytes.
func TestLinkedIcon(t *testing.T) {
	if testing.Short() {
		t.Skip("builds Windows executables")
	}
	imgs := []Image{{256, []byte("\x89PNG large")}, {16, []byte("\x89PNG small, of another length")}, {32, []byte("\x89PNG mid")}}
	for arch, m := range map[string]uint16{"amd64": AMD64, "arm64": ARM64} {
		t.Run(arch, func(t *testing.T) {
			obj, err := IconObject(m, imgs)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			files := map[string]string{
				"go.mod":  "module x\n\ngo 1.21\n",
				"main.go": "package main\n\nfunc main() {}\n",
			}
			for n, s := range files {
				os.WriteFile(filepath.Join(dir, n), []byte(s), 0o644)
			}
			os.WriteFile(filepath.Join(dir, "rsrc_windows_"+arch+".syso"), obj, 0o644)
			exe := filepath.Join(dir, "x.exe")
			cmd := exec.Command("go", "build", "-o", exe, ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH="+arch, "CGO_ENABLED=0", "GOFLAGS=", "GOWORK=off")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("go build: %v\n%s", err, out)
			}
			b, err := os.ReadFile(exe)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range GroupIDs {
				got, err := GroupIcon(b, id)
				if err != nil {
					t.Fatalf("group %d: %v", id, err)
				}
				want := []Image{imgs[1], imgs[2], imgs[0]}
				if len(got) != len(want) {
					t.Fatalf("group %d: %d images", id, len(got))
				}
				for i := range want {
					if got[i].Size != want[i].Size || !bytes.Equal(got[i].PNG, want[i].PNG) {
						t.Errorf("group %d image %d: %d %q, want %d %q", id, i, got[i].Size, got[i].PNG, want[i].Size, want[i].PNG)
					}
				}
			}
		})
	}
}

func TestIconObjectRejects(t *testing.T) {
	if _, err := IconObject(0x14c, []Image{{16, nil}}); err == nil {
		t.Error("386 accepted")
	}
	if _, err := IconObject(AMD64, nil); err == nil {
		t.Error("no images accepted")
	}
	if _, err := IconObject(AMD64, []Image{{300, nil}}); err == nil {
		t.Error("size 300 accepted")
	}
}
