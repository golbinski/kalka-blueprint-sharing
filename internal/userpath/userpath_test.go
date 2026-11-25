package userpath

import (
	"strings"
	"testing"
)

const dir = `C:\Users\a\AppData\Local\Programs\kalka`

func expand(s string) string {
	return strings.ReplaceAll(s, "%LOCALAPPDATA%", `C:\Users\a\AppData\Local`)
}

func TestAppendEntry(t *testing.T) {
	long := strings.Repeat(`C:\some\tool\bin;`, 400) + `C:\last` // ~6800 characters
	for _, c := range []struct{ in, want string }{
		{"", dir},
		{`C:\a`, `C:\a;` + dir},
		{`C:\a;`, `C:\a;` + dir},
		{long, long + ";" + dir},
	} {
		got, changed := appendEntry(c.in, dir, expand)
		if got != c.want || !changed {
			t.Errorf("appendEntry(%.40q) = %.60q, %v", c.in, got, changed)
		}
	}
	// Already there, in any spelling: nothing changes.
	for _, in := range []string{
		`C:\a;` + dir,
		dir + `;C:\a`,
		`C:\a;` + strings.ToUpper(dir),
		`C:\a;` + dir + `\`,
		`C:\a;C:/Users/a/AppData/Local/Programs/kalka`,
		`C:\a;%LOCALAPPDATA%\Programs\kalka`,
		`C:\a; "` + dir + `" ;C:\b`,
		`C:\a;C:\Users\a\AppData\Local\Programs\.\x\..\kalka`,
		long + ";" + dir,
	} {
		if got, changed := appendEntry(in, dir, expand); changed || got != in {
			t.Errorf("appendEntry(%.60q) changed it to %.60q", in, got)
		}
	}
	// A folder that only starts the same is another folder.
	if _, changed := appendEntry(dir+"-old", dir, expand); !changed {
		t.Error("kalka-old taken for kalka")
	}
}

func TestDropEntry(t *testing.T) {
	long := strings.Repeat(`C:\some\tool\bin;`, 400) + `C:\last`
	for _, c := range []struct{ in, want string }{
		{dir, ""},
		{`C:\a;` + dir, `C:\a`},
		{dir + `;C:\a`, `C:\a`},
		{`C:\a;` + dir + `;C:\b`, `C:\a;C:\b`},
		{`C:\a;` + dir + `;`, `C:\a;`},
		{`C:\a;;` + dir + `;C:\b`, `C:\a;;C:\b`},
		{`C:\a;%LOCALAPPDATA%\Programs\kalka;C:\b;` + dir + `\`, `C:\a;C:\b`},
		{long + ";" + dir, long},
		{dir + ";" + long, long},
	} {
		got, changed := dropEntry(c.in, dir, expand)
		if got != c.want || !changed {
			t.Errorf("dropEntry(%.60q) = %.60q, %v; want %.60q", c.in, got, changed, c.want)
		}
	}
	for _, in := range []string{"", `C:\a;C:\b`, `C:\a;;C:\b;`, dir + "-old", long} {
		if got, changed := dropEntry(in, dir, expand); changed || got != in {
			t.Errorf("dropEntry(%.60q) changed it to %.60q", in, got)
		}
	}
}
