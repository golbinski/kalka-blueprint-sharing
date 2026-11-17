package pickfolder

import (
	"runtime"
	"strings"
	"testing"
)

// The dialog itself is never shown in a test; only the command is checked.
func TestCommand(t *testing.T) {
	name, args, err := command(`/Users/ewa/"blue" prints`)
	if runtime.GOOS == "linux" && err == ErrUnavailable {
		t.Skip("no zenity or kdialog")
	}
	if err != nil {
		t.Fatal(err)
	}
	all := name + " " + strings.Join(args, " ")
	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(all, `POSIX file "/Users/ewa/\"blue\" prints"`) {
			t.Error(all)
		}
	case "windows":
		if !strings.Contains(all, "FolderBrowserDialog") {
			t.Error(all)
		}
	}
}

func TestTrailing(t *testing.T) {
	for in, want := range map[string]string{"/\n": "/", `C:\`: `\`, "/Users/ewa/\n": ""} {
		if got := trailing(in); got != want {
			t.Errorf("%q: %q", in, got)
		}
	}
}
