package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	}
	return nil
}
