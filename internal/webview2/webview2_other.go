//go:build !windows

package webview2

// Has is true where the webview is part of the system.
func Has() (string, bool) { return "", true }
