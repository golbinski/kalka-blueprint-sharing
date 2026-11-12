// Package notify shows a native popup that asks the user to open kalka.
// The daemon runs in the background (started at logon) with no terminal,
// so this is how a person learns that a teammate's sync waits for them.
// The popup only offers to open the UI; the decision itself is made there.
package notify

import (
	"context"
	"errors"
)

// ErrUnavailable means this system has no way to show the popup; the
// caller falls back to opening the browser.
var ErrUnavailable = errors.New("no popup available on this system")

// OpenLabel is the button that opens the UI.
const OpenLabel = "Open kalka"

// Ask shows title and message with the buttons "Open kalka" and "Later".
// It returns true when the user chose to open, false for "Later", and
// closes the popup and returns ctx.Err() when ctx ends first.
func Ask(ctx context.Context, title, message string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return ask(ctx, title, message)
}
