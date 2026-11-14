//go:build !windows

package autostart

import (
	"os/exec"
	"syscall"
)

func spawn(exe string) error {
	cmd := exec.Command(exe, Args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
