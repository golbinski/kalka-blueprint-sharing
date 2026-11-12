//go:build !darwin && !windows

package notify

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

// ask uses zenity where a desktop session has it.
func ask(ctx context.Context, title, message string) (bool, error) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false, ErrUnavailable
	}
	if _, err := exec.LookPath("zenity"); err != nil {
		return false, ErrUnavailable
	}
	cmd := exec.CommandContext(ctx, "zenity", "--question", "--no-markup",
		"--title", title, "--text", message, "--ok-label", OpenLabel, "--cancel-label", "Later")
	err := cmd.Run()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if exit.ExitCode() == 1 {
			return false, nil
		}
		return false, ErrUnavailable
	}
	return err == nil, err
}
