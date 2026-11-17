package desktop

import (
	"bytes"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/daemon"
)

func TestTrayModel(t *testing.T) {
	yes, no := true, false
	last := time.Date(2026, 11, 14, 14, 2, 0, 0, time.UTC)
	set := daemon.Settings{StartAtLogin: &yes, StartAtLoginAvailable: true}
	cases := []struct {
		sum                  core.Summary
		state                TrayState
		status, waiting, pse string
		hash                 string
	}{
		{core.Summary{Online: true, PeersOnline: 4, PeersKnown: 6, LastSync: last}, TrayOnline,
			"Online · 4 of 6 members reachable · last sync 14:02", "", "Pause sync", ""},
		{core.Summary{Online: true}, TrayOnline, "Online · no team members yet", "", "Pause sync", ""},
		{core.Summary{Paused: true, Joins: 2, Waiting: core.WaitingSummary{Total: 2, Oldest: &core.WaitingAt{Team: "lending", At: "2026-11-02T09:00:00Z", Target: "#/t/lending/waiting"}}}, TrayPaused, "Paused", "2 waiting for you", "Resume sync", "#/t/lending/waiting"},
		{core.Summary{Error: "rendezvous refused the key"}, TrayOffline, "Offline: rendezvous refused the key", "", "Pause sync", ""},
		{core.Summary{Joins: 1, Pushes: 3}, TrayOffline, "Offline: rendezvous not reachable", "", "Pause sync", ""},
		{core.Summary{Waiting: core.WaitingSummary{Total: 5, Oldest: &core.WaitingAt{Team: "returns", At: "2026-11-01T08:00:00Z", Target: "#/t/returns/waiting"}}}, TrayOffline, "Offline: rendezvous not reachable", "5 waiting for you", "Pause sync", "#/t/returns/waiting"},
	}
	for i, c := range cases {
		m := Tray(c.sum, set, time.UTC)
		if m.State != c.state || m.Status != c.status || m.Waiting != c.waiting || m.PauseLabel != c.pse || m.WaitingHash != c.hash {
			t.Errorf("%d: %+v", i, m)
		}
		if !m.StartAtLogin || !m.StartAtLoginEnabled || !strings.HasSuffix(m.Tooltip, c.status) {
			t.Errorf("%d: start at login or tooltip: %+v", i, m)
		}
	}
	if m := Tray(core.Summary{}, daemon.Settings{StartAtLogin: &no}, time.UTC); m.StartAtLogin || m.StartAtLoginEnabled {
		t.Errorf("start at login: %+v", m)
	}
}

func TestClosePolicy(t *testing.T) {
	cases := []struct {
		keep, tray, shown bool
		action            CloseAction
		notice            bool
	}{
		{true, true, false, CloseHide, true},
		{true, true, true, CloseHide, false},
		{true, false, false, CloseMinimise, true},
		{true, false, true, CloseMinimise, false},
		{false, true, false, CloseQuit, false},
		{false, false, true, CloseQuit, false},
	}
	for _, c := range cases {
		a, n := OnClose(c.keep, c.tray, c.shown)
		if a != c.action || n != c.notice {
			t.Errorf("OnClose(%v, %v, %v) = %v, %v", c.keep, c.tray, c.shown, a, n)
		}
	}
	if _, b := CloseNotice(CloseMinimise); !strings.Contains(b, "minimised") {
		t.Error(b)
	}
}

func TestStartTable(t *testing.T) {
	cases := []struct {
		free, answers, shell, hidden bool
		want                         StartAction
	}{
		{true, false, false, false, StartInProcess},
		{true, false, false, true, StartInProcess},
		{false, false, false, false, StartWait},
		{false, true, true, false, StartShowOther},
		{false, true, true, true, StartExit},
		{false, true, false, false, StartAttach},
		{false, true, false, true, StartAttach},
	}
	for _, c := range cases {
		if got := DecideStart(c.free, c.answers, c.shell, c.hidden); got != c.want {
			t.Errorf("DecideStart(%v, %v, %v, %v) = %v, want %v", c.free, c.answers, c.shell, c.hidden, got, c.want)
		}
	}
	if _, m := QuitQuestion(true); !strings.Contains(m, "without a window") {
		t.Error(m)
	}
	if _, m := QuitQuestion(false); strings.Contains(m, "without a window") || !strings.Contains(m, "until you start it again") {
		t.Error(m)
	}
}

func TestHashes(t *testing.T) {
	if h := DocHash("plans/Parcel routing.md"); h != "#/doc/plans%2FParcel%20routing.md" {
		t.Error(h)
	}
	for n, want := range map[core.Notice]string{
		{Team: "lending desk"}:                       "#/t/lending%20desk/team",
		{Team: "lending", Kind: core.NoticeJoinDone}: "#/t/lending/",
		{Team: "lending", Kind: core.NoticeJoinFail}: "#/join",
		{Team: "lending", Kind: core.NoticeTransfer}: "#/needs",
		{Team: "lending", Path: "lending/loans.md"}:  "#/doc/lending%2Floans.md",
	} {
		if h := NoticeHash(n); h != want {
			t.Errorf("%+v: %s, want %s", n, h, want)
		}
	}
	if h := NoticeHash(core.Notice{}); h != "" {
		t.Error(h)
	}
}

func TestTrayIcons(t *testing.T) {
	seen := map[string]TrayState{}
	for _, st := range []TrayState{TrayStarting, TrayOnline, TrayOffline, TrayPaused} {
		for _, tmpl := range []bool{true, false} {
			b := TrayIcon(st, tmpl)
			img, err := png.Decode(bytes.NewReader(b))
			if err != nil || img.Bounds().Dx() != 44 {
				t.Fatalf("%s: %v", st, err)
			}
			if tmpl {
				if other, dup := seen[string(b)]; dup {
					t.Errorf("%s draws like %s", st, other)
				}
				seen[string(b)] = st
			}
		}
	}
}

func TestAppIcon(t *testing.T) {
	for _, n := range []int{16, 128, 1024} {
		img, err := png.Decode(bytes.NewReader(AppIcon(n)))
		if err != nil || img.Bounds().Dx() != n {
			t.Fatalf("%d: %v", n, err)
		}
		// Transparent corner, opaque middle.
		if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
			t.Errorf("%d: corner alpha %d", n, a)
		}
		if _, _, _, a := img.At(n/2, n/2).RGBA(); a != 0xffff {
			t.Errorf("%d: centre alpha %d", n, a)
		}
	}
}
