package desktop

import (
	"strconv"
	"strings"
)

// WebView2 (spec 23.2): the Evergreen runtime records its version as the
// "pv" value of its client key, per machine (both registry views) or per
// user. An empty value or 0.0.0.0 means removed.
const webView2Client = `Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`

// WebView2Keys are the keys to read "pv" from, under HKLM then HKCU.
var WebView2Keys = []struct {
	Machine bool
	Path    string
}{
	{true, `SOFTWARE\WOW6432Node\` + webView2Client},
	{true, `SOFTWARE\` + webView2Client},
	{false, `Software\` + webView2Client},
}

// MinWebView2 is the oldest runtime the shell accepts.
const MinWebView2 = "86.0.616.0"

// WebView2Version picks the first usable version among the values read;
// ok is false when none is installed or all are too old.
func WebView2Version(values []string) (version string, ok bool) {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || v == "0.0.0.0" {
			continue
		}
		if compareVersions(v, MinWebView2) >= 0 {
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

// WebView2Download is where the runtime is offered.
const WebView2Download = "https://developer.microsoft.com/microsoft-edge/webview2/"
