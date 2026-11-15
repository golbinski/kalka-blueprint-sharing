// Package desktop is the desktop shell (spec 22): a tray icon and a window
// around the daemon. Everything here except the Wails binding (build tag
// desktop) is plain Go and tested without a display: the tray model, the
// close and start tables, the WebView2 check and the shell's controller,
// which drives the window and tray through the UI interface.
//
// The shell is a client of the local HTTP API in every case, the same API
// the browser and the CLI use (decision 7). When it holds the
// single-daemon lock it also runs the daemon in its own process; when
// another daemon holds it, it attaches to that one.
package desktop

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/golbinski/kalka/internal/autostart"
	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/urlscheme"
)

// TrayState picks the tray icon.
type TrayState string

const (
	TrayStarting TrayState = "starting"
	TrayOnline   TrayState = "online"
	TrayOffline  TrayState = "offline"
	TrayPaused   TrayState = "paused"
)

// TrayModel is everything the tray shows (spec 22.1).
type TrayModel struct {
	State  TrayState
	Status string // the status line
	// Waiting is "N waiting for you", empty when nothing waits.
	Waiting string
	// WaitingHash is where "N waiting" opens the window: the Team page,
	// where joins show.
	WaitingHash string
	Paused      bool
	PauseLabel  string
	// StartAtLogin is the check mark; StartAtLoginEnabled whether the item
	// can be used.
	StartAtLogin        bool
	StartAtLoginEnabled bool
	Tooltip             string
}

// Tray builds the tray model from the daemon's summary and settings. loc
// is the zone the last sync time is shown in.
func Tray(sum core.Summary, set daemon.Settings, loc *time.Location) TrayModel {
	m := TrayModel{Paused: sum.Paused, PauseLabel: "Pause sync",
		StartAtLogin: on(set.StartAtLogin), StartAtLoginEnabled: set.StartAtLoginAvailable}
	switch {
	case sum.Paused:
		m.State, m.Status, m.PauseLabel = TrayPaused, "Paused", "Resume sync"
	case sum.Online:
		m.State = TrayOnline
		m.Status = fmt.Sprintf("Online · %d of %d members reachable", sum.PeersOnline, sum.PeersKnown)
		if sum.PeersKnown == 0 {
			m.Status = "Online · no team members yet"
		}
		if !sum.LastSync.IsZero() {
			m.Status += " · last sync " + sum.LastSync.In(loc).Format("15:04")
		}
	default:
		m.State = TrayOffline
		reason := sum.Error
		if reason == "" {
			reason = "rendezvous not reachable"
		}
		m.Status = "Offline: " + reason
	}
	if n := sum.Waiting(); n > 0 {
		m.Waiting = fmt.Sprintf("%d waiting for you", n)
		m.WaitingHash = "#/team"
	}
	m.Tooltip = name.Product + ": " + m.Status
	return m
}

// StartingTray is the tray before the daemon answers.
func StartingTray() TrayModel {
	return TrayModel{State: TrayStarting, Status: "Starting…", PauseLabel: "Pause sync", Tooltip: name.Product + ": starting"}
}

func on(b *bool) bool { return b != nil && *b }

// CloseAction is what closing the window does.
type CloseAction int

const (
	CloseHide     CloseAction = iota // hide; the tray reopens it
	CloseMinimise                    // no tray host: minimise to the task bar
	CloseQuit                        // the setting is off: quit the shell
)

// OnClose is the close policy (spec 22.2): keep running hides the window,
// or minimises it where there is no tray to bring it back; the first time
// one notice says so. With the setting off the shell quits.
func OnClose(keepRunning, trayAvailable, noticeShown bool) (action CloseAction, notice bool) {
	switch {
	case !keepRunning:
		return CloseQuit, false
	case !trayAvailable:
		return CloseMinimise, !noticeShown
	default:
		return CloseHide, !noticeShown
	}
}

// CloseNotice is the one-time notice after the first close.
func CloseNotice(action CloseAction) (title, body string) {
	title = name.Product + " keeps syncing"
	if action == CloseMinimise {
		return title, fmt.Sprintf("The window is minimised; %s keeps syncing with your team. Start %s again to bring the window back.", name.Product, name.Product)
	}
	return title, fmt.Sprintf("%s keeps syncing with your team in the background. Open it again from its icon in the menu bar or the notification area, or start %s again.", name.Product, name.Product)
}

// Start is how kalka-desktop was started (spec 23.1a): with no arguments
// or only --hidden it is the shell; with a single kalka:// argument, as
// Windows and Linux hand over a link, the shell on the join screen; with
// anything else the CLI.
type Start struct {
	CLI    bool
	Hidden bool
	Link   string
}

// StartArgs reads kalka-desktop's arguments.
func StartArgs(args []string) Start {
	switch {
	case len(args) == 0:
		return Start{}
	case len(args) == 1 && args[0] == autostart.HiddenArg:
		return Start{Hidden: true}
	case len(args) == 1 && len(args[0]) > len(urlscheme.Scheme) && strings.EqualFold(args[0][:len(urlscheme.Scheme)+1], urlscheme.Scheme+":"):
		return Start{Link: args[0]}
	}
	return Start{CLI: true}
}

// StartAction is what a start of the shell does (plan decision 3).
type StartAction int

const (
	StartInProcess StartAction = iota // run the daemon here, with tray and window
	StartAttach                       // attach to a headless daemon
	StartShowOther                    // ask the shell that runs to show its window, then exit
	StartExit                         // exit 0, showing nothing
	StartWait                         // the holder does not answer yet: try again
)

// DecideStart is the start table: lockFree, or else whether the daemon
// holding the lock answers and has a shell attached.
func DecideStart(lockFree, holderAnswers, holderHasShell, hidden bool) StartAction {
	switch {
	case lockFree:
		return StartInProcess
	case !holderAnswers:
		return StartWait
	case holderHasShell && hidden:
		return StartExit
	case holderHasShell:
		return StartShowOther
	default:
		return StartAttach
	}
}

// QuitQuestion is the confirmation before Quit (spec 22.1).
func QuitQuestion(attached bool) (title, message string) {
	title = "Quit " + name.Product + "?"
	message = name.Product + " will stop syncing with your team until you start it again."
	if attached {
		message += fmt.Sprintf(" This also stops the %s that was started without a window.", name.Product)
	}
	return title, message
}

// DocHash is the page's route for a document.
func DocHash(path string) string {
	if path == "" {
		return ""
	}
	return "#/doc/" + url.PathEscape(path)
}

// NoticeHash is where a notice's click opens the window.
func NoticeHash(n core.Notice) string {
	switch {
	case n.Path != "":
		return DocHash(n.Path)
	case n.Team != "":
		return "#/team"
	}
	return ""
}
