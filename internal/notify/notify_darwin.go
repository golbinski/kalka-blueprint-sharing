package notify

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// ask uses AppleScript's dialog. The texts go in as arguments, never into
// the script, so a blueprint title cannot inject AppleScript.
func ask(ctx context.Context, title, message string) (bool, error) {
	if _, err := exec.LookPath("osascript"); err != nil {
		return false, ErrUnavailable
	}
	cmd := exec.CommandContext(ctx, "osascript",
		"-e", "on run argv",
		"-e", `display dialog (item 2 of argv) with title (item 1 of argv) buttons {"Later", "`+OpenLabel+`"} default button "`+OpenLabel+`" cancel button "Later" with icon note`,
		"-e", "end run",
		title, message)
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return false, nil // "Later" is the cancel button: osascript exits with -128
	}
	if err != nil {
		return false, err
	}
	return strings.Contains(string(out), "button returned:"+OpenLabel), nil
}
