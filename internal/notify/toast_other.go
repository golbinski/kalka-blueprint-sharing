//go:build !windows

package notify

import "context"

// Toast shows a notification-centre notification; only Windows has one
// kalka can use without an app bundle. Elsewhere it returns ErrUnavailable
// and the caller shows a popup.
func Toast(ctx context.Context, title, message, url string) error {
	return ErrUnavailable
}
