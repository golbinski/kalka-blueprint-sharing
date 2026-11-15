package desktop

import "os/exec"

// The shell is a GUI program on Windows: it has no terminal to leave.
func setDetached(*exec.Cmd) {}
