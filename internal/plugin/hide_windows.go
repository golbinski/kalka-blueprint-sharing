package plugin

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// hideWindow keeps a console plugin from opening a console window when
// kalka runs as the console-less kalkaw.exe. The plugin still gets a
// (hidden) console, which its own children, such as gh, share.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
