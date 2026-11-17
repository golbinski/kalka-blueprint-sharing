package webview2

import "testing"

// TestVersion checks sample registry values (spec 23.2).
func TestVersion(t *testing.T) {
	cases := []struct {
		values []string
		want   string
		ok     bool
	}{
		{nil, "", false},
		{[]string{""}, "", false},
		{[]string{"0.0.0.0"}, "", false},
		{[]string{"0.0.0.0", "", "131.0.2903.70"}, "131.0.2903.70", true},
		{[]string{"86.0.616.0"}, "86.0.616.0", true},
		{[]string{"85.0.564.68"}, "", false},
		{[]string{"85.0.564.68", " 120.0.2210.91 "}, "120.0.2210.91", true},
	}
	for _, c := range cases {
		v, ok := Version(c.values)
		if v != c.want || ok != c.ok {
			t.Errorf("%q: %q %v", c.values, v, ok)
		}
	}
}
