package peerjs

import "testing"

func TestParseRendezvous(t *testing.T) {
	cases := map[string]Server{
		"peerjs-cloud":                    {URL: "wss://0.peerjs.com:443/peerjs", Key: "peerjs"},
		"wss://kalka.corp.example/":       {URL: "wss://kalka.corp.example/peerjs", Key: "peerjs"},
		"https://kalka.corp.example/rdv":  {URL: "wss://kalka.corp.example/rdv/peerjs", Key: "peerjs"},
		"ws://127.0.0.1:9000/?key=team-a": {URL: "ws://127.0.0.1:9000/peerjs", Key: "team-a"},
	}
	for in, want := range cases {
		got, err := ParseRendezvous(in)
		if err != nil || got != want {
			t.Errorf("ParseRendezvous(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	if _, err := ParseRendezvous("ftp://x"); err == nil {
		t.Error("ftp accepted")
	}
	if o := (Server{URL: "wss://kalka.corp.example/peerjs"}).HTTPOrigin(); o != "https://kalka.corp.example" {
		t.Errorf("origin %q", o)
	}
}

func TestValidID(t *testing.T) {
	for _, ok := range []string{"kalka-0123abcd", "a", "a b_c-d"} {
		if !ValidID(ok) {
			t.Errorf("%q rejected", ok)
		}
	}
	for _, bad := range []string{"", "-a", "a-", "a--b", "a.b"} {
		if ValidID(bad) {
			t.Errorf("%q accepted", bad)
		}
	}
}
