package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/golbinski/kalka/internal/winres"
)

// PE subsystems (the optional header's Subsystem field).
const (
	subsystemGUI     = 2
	subsystemConsole = 3
)

// peSubsystem reads the subsystem of a Windows executable: GUI programs
// open no console window, console programs print to the terminal.
func peSubsystem(b []byte) (int, error) {
	if len(b) < 0x40 || b[0] != 'M' || b[1] != 'Z' {
		return 0, errors.New("not a PE file")
	}
	off := int(binary.LittleEndian.Uint32(b[0x3c:]))
	opt := off + 4 + 20 // signature, then the COFF header
	if off < 0 || opt+70 > len(b) || string(b[off:off+4]) != "PE\x00\x00" {
		return 0, errors.New("not a PE file")
	}
	return int(binary.LittleEndian.Uint16(b[opt+68:])), nil
}

// checkSubsystems: kalka.exe must stay a console program (the CLI) and
// kalka-desktop.exe a GUI one (the shell, and the console-less start at
// logon). A mix-up would open a console at every logon, or make the CLI
// print nothing.
func checkSubsystems(dir string) error {
	for bin, want := range map[string]int{"kalka.exe": subsystemConsole, "kalka-desktop.exe": subsystemGUI} {
		data, err := os.ReadFile(filepath.Join(dir, bin))
		if err != nil {
			return err
		}
		got, err := peSubsystem(data)
		if err != nil {
			return fmt.Errorf("%s: %w", bin, err)
		}
		if got != want {
			return fmt.Errorf("%s has PE subsystem %d, want %d", bin, got, want)
		}
		// The shell carries the app icon, for its shortcut, taskbar
		// button and window (cmd/kalka-desktop/rsrc_windows_<arch>.syso).
		if bin == "kalka-desktop.exe" {
			for _, id := range winres.GroupIDs {
				if imgs, err := winres.GroupIcon(data, id); err != nil || len(imgs) == 0 {
					return fmt.Errorf("%s has no icon %d (%v); run go generate ./cmd/kalka-desktop", bin, id, err)
				}
			}
		}
	}
	return nil
}
