// Package webview2 finds the Microsoft Edge WebView2 runtime the Windows
// window needs (spec 23.2); the shell and kalka doctor both ask it.
package webview2

import (
	"strconv"
	"strings"
)

// WebView2 (spec 23.2): the Evergreen runtime records its version as the
// "pv" value of its client key, per machine (both registry views) or per
// user. An empty value or 0.0.0.0 means removed.
const webView2Client = `Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`

// Keys are the keys to read "pv" from, under HKLM then HKCU.
var Keys = []struct {
	Machine bool
	Path    string
}{
	{true, `SOFTWARE\WOW6432Node\` + webView2Client},
	{true, `SOFTWARE\` + webView2Client},
	{false, `Software\` + webView2Client},
}

// Min is the oldest runtime the shell accepts.
const Min = "86.0.616.0"

// Version picks the first usable version among the values read;
// ok is false when none is installed or all are too old.
func Version(values []string) (version string, ok bool) {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || v == "0.0.0.0" {
			continue
		}
		if compareVersions(v, Min) >= 0 {
			return v, true
		}
	}
	return "", false
}

// compareVersions compares dotted numeric versions; a part that is not a
// number counts as 0.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// Download is where the runtime is offered.
const Download = "https://developer.microsoft.com/microsoft-edge/webview2/"
