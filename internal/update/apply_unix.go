//go:build !windows

package update

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// waitThenRun waits for the given PID to end (at most a minute), then
// runs the command: the new kalka must not meet the old one's lock.
const waitThenRun = `i=0; while kill -0 "$0" 2>/dev/null && [ $i -lt 300 ]; do sleep 0.2; i=$((i+1)); done; exec "$@"`

// start runs a command detached; tests replace it.
var start = func(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// Apply installs the verified package into the target and arranges for
// kalka to start again once this process has ended. The caller then shuts
// kalka down the usual way; if Apply fails, nothing has changed that
// matters and kalka keeps running.
func Apply(t Target, pkg string) error {
	var relaunch []string
	switch t.Kind {
	case AppBundle:
		if err := installApp(pkg, t.Path); err != nil {
			return err
		}
		relaunch = []string{"open", t.Path}
	case Tarball:
		if err := installTarball(pkg, t.Path); err != nil {
			return err
		}
		relaunch = []string{filepath.Join(t.Path, "kalka-desktop")}
	default:
		return errors.New("this installation cannot update itself")
	}
	return start(append([]string{"/bin/sh", "-c", waitThenRun, strconv.Itoa(os.Getpid())}, relaunch...)...)
}
