// Command kalka is the CLI and the headless daemon (spec 4, 12).
package main

import (
	"os"

	"github.com/golbinski/kalka/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
