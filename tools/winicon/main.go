// Command winicon writes the icon resources of kalka-desktop.exe
// (cmd/kalka-desktop/rsrc_windows_<arch>.syso) from the app icon. It runs
// with `go generate ./cmd/kalka-desktop`; a test keeps the committed files
// in step with desktop.AppIcon.
//
//	go run ./tools/winicon [-dir cmd/kalka-desktop]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/golbinski/kalka/internal/desktop"
	"github.com/golbinski/kalka/internal/winres"
)

// Sizes are the sizes Windows asks for at the usual display scales: small
// icons (16 to 32), the taskbar and Explorer (32 to 64), and large views.
var Sizes = []int{16, 20, 24, 32, 40, 48, 64, 256}

// Arches are the Windows architectures kalka is released for.
var Arches = map[string]uint16{"amd64": winres.AMD64, "arm64": winres.ARM64}

func main() {
	dir := flag.String("dir", ".", "the cmd/kalka-desktop folder")
	flag.Parse()
	files, err := Objects()
	if err == nil {
		for name, b := range files {
			if err = os.WriteFile(filepath.Join(*dir, name), b, 0o644); err != nil {
				break
			}
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "winicon:", err)
		os.Exit(1)
	}
}

// Objects are the .syso files by name.
func Objects() (map[string][]byte, error) {
	var imgs []winres.Image
	for _, s := range Sizes {
		imgs = append(imgs, winres.Image{Size: s, PNG: desktop.AppIcon(s)})
	}
	out := map[string][]byte{}
	for arch, m := range Arches {
		b, err := winres.IconObject(m, imgs)
		if err != nil {
			return nil, err
		}
		out["rsrc_windows_"+arch+".syso"] = b
	}
	return out, nil
}
