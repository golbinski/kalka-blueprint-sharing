//go:build !windows

package desktop

// HasWebView2 is true where the webview is part of the system.
func HasWebView2() (string, bool) { return "", true }
