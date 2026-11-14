//go:build desktop

// Command kalka-desktop is the desktop shell (spec 22): the tray and the
// window, with the daemon in-process or attached. With arguments it is the
// CLI, so one executable serves both where the platform allows (spec 23.1).
package main

import (
	"os"

	"github.com/golbinski/kalka/internal/desktop"
)

func main() { os.Exit(desktop.Main(os.Args[1:])) }
