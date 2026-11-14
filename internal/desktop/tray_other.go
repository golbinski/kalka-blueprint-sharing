//go:build !linux

package desktop

// TrayHost: macOS and Windows always have a menu bar or notification area.
func TrayHost() bool { return true }
