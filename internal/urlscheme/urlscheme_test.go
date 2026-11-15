package urlscheme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRegistry is HKCU in memory: key -> value -> data.
type fakeRegistry struct {
	keys   map[string]map[string]string
	writes int
}

func newFake() *fakeRegistry { return &fakeRegistry{keys: map[string]map[string]string{}} }

func (f *fakeRegistry) String(key, value string) (string, error) {
	vs, ok := f.keys[strings.ToLower(key)]
	if !ok {
		return "", ErrNotExist
	}
	d, ok := vs[value]
	if !ok {
		return "", ErrNotExist
	}
	return d, nil
}

func (f *fakeRegistry) SetString(key, value, data string) error {
	k := strings.ToLower(key)
	if f.keys[k] == nil {
		f.keys[k] = map[string]string{}
	}
	f.keys[k][value] = data
	f.writes++
	return nil
}

func (f *fakeRegistry) DeleteTree(key string) error {
	k := strings.ToLower(key)
	for name := range f.keys {
		if name == k || strings.HasPrefix(name, k+`\`) {
			delete(f.keys, name)
		}
	}
	return nil
}

func TestWindows(t *testing.T) {
	reg := newFake()
	exe := `C:\Users\olga\AppData\Local\Programs\kalka\kalka-desktop.exe`
	tg := Target{OS: "windows", Exe: exe, Registry: reg}
	if st, err := Status(tg); err != nil || st.Registered {
		t.Fatalf("before: %+v %v", st, err)
	}
	if err := Register(tg); err != nil {
		t.Fatal(err)
	}
	if v, _ := reg.String(`Software\Classes\kalka`, "URL Protocol"); v != "" {
		t.Fatalf("URL Protocol = %q", v)
	}
	cmd, _ := reg.String(`Software\Classes\kalka\shell\open\command`, "")
	if cmd != `"`+exe+`" "%1"` {
		t.Fatalf("command %q", cmd)
	}
	if st, err := Status(tg); err != nil || !st.Registered || st.Command != cmd {
		t.Fatalf("after: %+v %v", st, err)
	}
	// The shell's re-check at start writes nothing when all is in place.
	n := reg.writes
	if err := Register(tg); err != nil || reg.writes != n {
		t.Fatalf("re-check wrote %d values: %v", reg.writes-n, err)
	}
	// A moved install is corrected.
	tg.Exe = `D:\kalka\kalka-desktop.exe`
	if err := Register(tg); err != nil {
		t.Fatal(err)
	}
	if st, _ := Status(tg); st.Command != `"D:\kalka\kalka-desktop.exe" "%1"` {
		t.Fatalf("moved: %+v", st)
	}
	reg.SetString(`Software\Classes\other`, "", "kept")
	if err := Unregister(tg); err != nil {
		t.Fatal(err)
	}
	if st, _ := Status(tg); st.Registered {
		t.Fatal("still registered")
	}
	for k := range reg.keys {
		if strings.HasPrefix(k, `software\classes\kalka`) {
			t.Fatalf("left %s", k)
		}
	}
	if _, err := reg.String(`Software\Classes\other`, ""); err != nil {
		t.Fatal("removed another key")
	}
}

func TestLinux(t *testing.T) {
	data := t.TempDir()
	var ran [][]string
	tg := Target{OS: "linux", Exe: "/opt/kalka/kalka-desktop", DataHome: data, Run: func(p string, args ...string) error {
		ran = append(ran, append([]string{p}, args...))
		return nil
	}}
	if st, _ := Status(tg); st.Registered {
		t.Fatal("registered before")
	}
	if err := Register(tg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(data, "applications", "kalka.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"MimeType=x-scheme-handler/kalka;", "Exec=/opt/kalka/kalka-desktop %u"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing %q:\n%s", want, raw)
		}
	}
	if len(ran) != 1 || strings.Join(ran[0], " ") != "xdg-mime default kalka.desktop x-scheme-handler/kalka" {
		t.Fatalf("ran %v", ran)
	}
	if st, _ := Status(tg); !st.Registered || st.Command != "/opt/kalka/kalka-desktop %u" {
		t.Fatalf("status %+v", st)
	}
	if err := Unregister(tg); err != nil {
		t.Fatal(err)
	}
	if st, _ := Status(tg); st.Registered {
		t.Fatal("registered after")
	}
	if err := Unregister(tg); err != nil {
		t.Fatal("a second unregister:", err)
	}
}

func TestMacOS(t *testing.T) {
	// Nothing at run time: the bundle's Info.plist registers the scheme.
	tg := Target{OS: "darwin", Exe: "/Applications/Kalka.app/Contents/MacOS/kalka"}
	if err := Register(tg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(PlistURLTypes, "<string>kalka</string>") || !strings.Contains(PlistURLTypes, "CFBundleURLSchemes") {
		t.Fatal(PlistURLTypes)
	}
}

func TestDesktopEntryQuoting(t *testing.T) {
	e := DesktopEntry(`/home/olga/my apps/kalka$1/kalka-desktop`)
	if !strings.Contains(e, `Exec="/home/olga/my apps/kalka\\$1/kalka-desktop" %u`) {
		t.Fatal(e)
	}
	if !strings.Contains(DesktopEntry("kalka-desktop"), "Exec=kalka-desktop %u\n") {
		t.Fatal("plain name quoted")
	}
}
