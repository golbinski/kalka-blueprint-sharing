package autostart

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/name"
)

// Overridable in tests.
var (
	homeDir   = os.UserHomeDir
	launchctl = func(args ...string) error {
		out, err := exec.Command("/bin/launchctl", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("launchctl %s: %v: %s", args[0], err, strings.TrimSpace(string(out)))
		}
		return nil
	}
)

func plistPath() (string, error) {
	h, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "Library", "LaunchAgents", name.AppID+".plist"), nil
}

func domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// plist is the LaunchAgent. KeepAlive restarts kalka only after a crash:
// a second start exits 0 when another daemon holds the lock (spec 4.1),
// so launchd does not loop.
func plist(t Target) []byte {
	var b bytes.Buffer
	var args strings.Builder
	esc := func(s string) string {
		var e bytes.Buffer
		_ = xml.EscapeText(&e, []byte(s))
		return e.String()
	}
	for _, a := range append([]string{t.Exe}, t.Args...) {
		fmt.Fprintf(&args, "\t\t<string>%s</string>\n", esc(a))
	}
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
%s	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>30</integer>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`, esc(name.AppID), args.String())
	return b.Bytes()
}

func headless(exe string) (Target, error) { return Target{exe, Args}, nil }

func enable(t Target) (Status, error) {
	p, err := plistPath()
	if err != nil {
		return Status{}, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return Status{}, err
	}
	// Unload an older entry first; it may not be loaded.
	_ = launchctl("bootout", domain()+"/"+name.AppID)
	if err := fsutil.WriteAtomic(p, plist(t), nil); err != nil {
		return Status{}, err
	}
	if err := launchctl("bootstrap", domain(), p); err != nil {
		return Status{}, err
	}
	return get()
}

func disable() error {
	p, err := plistPath()
	if err != nil {
		return err
	}
	_ = launchctl("bootout", domain()+"/"+name.AppID)
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func get() (Status, error) {
	p, err := plistPath()
	if err != nil {
		return Status{}, err
	}
	s := Status{Where: p}
	raw, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.Enabled = true
	// The <string>s of ProgramArguments: the program, then its arguments.
	if _, rest, ok := strings.Cut(string(raw), "<array>"); ok {
		rest, _, _ = strings.Cut(rest, "</array>")
		var u struct {
			S []string `xml:"string"`
		}
		if xml.Unmarshal([]byte("<x>"+rest+"</x>"), &u) == nil && len(u.S) > 0 {
			s.Command = strings.Join(u.S, " ")
			_, statErr := os.Stat(u.S[0])
			s.Stale = statErr != nil
		}
	}
	return s, nil
}

const startsOnEnable = true

func start(Target) error {
	return launchctl("kickstart", domain()+"/"+name.AppID)
}
