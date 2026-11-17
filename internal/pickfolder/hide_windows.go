package pickfolder

import (
	"os/exec"
	"syscall"
)

// hide keeps PowerShell's console window from flashing up.
func hide(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
