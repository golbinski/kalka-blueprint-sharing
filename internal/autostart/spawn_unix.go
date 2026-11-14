//go:build !windows

package autostart

import (
	"os/exec"
	"syscall"
)

func spawn(exe string) error { return detached(exe, Args) }

func detached(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
