//go:build !windows

package pickfolder

import "os/exec"

func hide(*exec.Cmd) {}
