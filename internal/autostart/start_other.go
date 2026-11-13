//go:build !darwin && !windows

package autostart

import (
	"os/exec"
	"syscall"
)

const startsOnEnable = false

func start(exe string) error {
	cmd := exec.Command(exe, BackgroundFlag)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
