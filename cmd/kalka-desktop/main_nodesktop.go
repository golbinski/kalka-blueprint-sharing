//go:build !desktop

package main

import (
	"fmt"
	"os"

	"github.com/golbinski/kalka/internal/cli"
)

// Without the desktop tag this is only the CLI; the shell needs
// `go build -tags desktop`.
func main() {
	if len(os.Args) < 2 || os.Args[1] == "--hidden" {
		fmt.Fprintln(os.Stderr, "kalka-desktop was built without the desktop shell: build it with -tags desktop, or use kalka")
		os.Exit(1)
	}
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
