package desktop

import (
	"fmt"
	"os"
	"os/exec"
)

// detachedEnv marks the shell started by detach.
const detachedEnv = "KALKA_SHELL_DETACHED"

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// detach starts this binary again, with no arguments, outside the
// terminal's session; that start shows the window (or the running shell's
// window) as usual.
func detach() int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), detachedEnv+"=1")
	setDetached(cmd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = cmd.Process.Release()
	return 0
}
