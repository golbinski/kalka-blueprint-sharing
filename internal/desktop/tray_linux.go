package desktop

import (
	"os/exec"
	"strings"
)

// TrayHost reports whether a StatusNotifierWatcher is registered on the
// session bus (spec 22.2): stock GNOME has none without an extension.
func TrayHost() bool {
	out, err := exec.Command("dbus-send", "--session", "--print-reply", "--dest=org.freedesktop.DBus",
		"/org/freedesktop/DBus", "org.freedesktop.DBus.NameHasOwner", "string:org.kde.StatusNotifierWatcher").Output()
	if err != nil {
		return true // cannot tell: assume a tray, as most desktops have one
	}
	return parseNameHasOwner(string(out))
}

func parseNameHasOwner(out string) bool { return strings.Contains(out, "boolean true") }
