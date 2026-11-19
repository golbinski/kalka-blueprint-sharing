//go:build windows

package update

import (
	"errors"
	"os/exec"
	"syscall"
)

// start runs a command detached; tests replace it.
var start = func(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 | 0x00000200} // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// Apply runs the verified installer silently. It waits for this process's
// program files to be free, replaces them and starts kalka again
// (kalka.nsi.tmpl); the caller shuts kalka down right after.
func Apply(t Target, pkg string) error {
	if t.Kind != Installer {
		return errors.New("this installation cannot update itself")
	}
	return start(pkg, "/S")
}
