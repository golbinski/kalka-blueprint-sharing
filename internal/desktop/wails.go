//go:build desktop

package desktop

// The Wails binding: the tray, the window, native dialogs and
// notifications (spec 22). Built only with the desktop tag; everything it
// decides is in Shell.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"

	"github.com/golbinski/kalka/internal/autostart"
	"github.com/golbinski/kalka/internal/cli"
	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/notify"
	"github.com/golbinski/kalka/internal/urlscheme"
	"github.com/golbinski/kalka/internal/webview2"
)

// Main is kalka-desktop: with arguments the CLI, without them (or with
// only --hidden) the shell, and with a single kalka:// link the shell on
// that link's join screen (StartArgs).
func Main(args []string) int {
	autostart.DesktopBuild = true
	st := StartArgs(args)
	if st.CLI {
		return cli.Run(args, os.Stdin, os.Stdout, os.Stderr)
	}
	hidden, route := st.Hidden, ""
	if st.Link != "" {
		route = daemon.JoinRoute(st.Link)
	}
	// Started bare in a terminal (macOS, where `kalka` on the PATH is this
	// binary): run the shell detached, so it outlives the terminal.
	if !hidden && route == "" && os.Getenv(detachedEnv) == "" && isTerminal(os.Stdin) {
		return detach()
	}
	dir, err := config.Dir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	_ = os.MkdirAll(dir, 0o700)
	logw := io.Writer(io.Discard)
	if f, err := os.OpenFile(filepath.Join(dir, name.Product+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		defer f.Close()
		logw = f
	}
	if _, ok := webview2.Has(); !ok {
		return noWebView(dir, logw)
	}
	plan, err := Prepare(context.Background(), dir, hidden, route)
	if err != nil {
		fmt.Fprintf(logw, "shell: %v\n", err)
		_, _ = notify.Ask(context.Background(), name.Product, err.Error())
		return 1
	}
	if plan.Action == StartShowOther || plan.Action == StartExit {
		return 0
	}
	registerScheme(logw)
	return run(plan, dir, hidden, logw)
}

// registerScheme re-checks the kalka:// registration at start (spec
// 23.1a): Windows rewrites it if it is missing or names another path,
// Linux writes its desktop entry; macOS has it in the bundle. A start
// with KALKA_NO_URL_SCHEME=1 (the smoke test) leaves it alone.
func registerScheme(logw io.Writer) {
	if os.Getenv(urlscheme.SkipEnv) == "1" {
		return
	}
	exe, err := autostart.Executable()
	if err == nil {
		err = urlscheme.Register(urlscheme.System(exe))
	}
	if err != nil {
		fmt.Fprintf(logw, "shell: kalka:// links: %v\n", err)
	}
}

// noWebView: the window cannot be shown (spec 23.2). Offer the browser,
// with the daemon running as usual.
func noWebView(dir string, logw io.Writer) int {
	fmt.Fprintln(logw, "shell: the WebView2 runtime is missing")
	msg := fmt.Sprintf("The Microsoft Edge WebView2 runtime is missing, so the %s window cannot open. Install it from %s.\n\nOpen %s in the browser instead?",
		name.Product, webview2.Download, name.Product)
	ok, _ := notify.Ask(context.Background(), name.Product, msg)
	if !ok {
		return 0
	}
	return cli.Run(nil, nil, logw, logw) // bare kalka: start the daemon, open the browser
}

// wailsUI implements UI.
type wailsUI struct {
	app   *application.App
	win   *application.WebviewWindow
	tray  *application.SystemTray
	menu  *application.Menu
	notes *notifications.NotificationService

	status, waiting, update, pause, login *application.MenuItem

	mu       sync.Mutex
	url      string
	state    TrayState
	noNotes  bool
	quitOnce sync.Once
	// webReady closes once the window's WebView has started. On Windows,
	// Wails ends the process with status 1 when the app quits while
	// WebView2 is still starting, so a quit waits for it (see Quit).
	webReady     chan struct{}
	webReadyOnce sync.Once
}

// webStartWait is how long a quit waits for a starting WebView.
const webStartWait = 10 * time.Second

func run(plan Plan, dir string, hidden bool, logw io.Writer) int {
	ui := &wailsUI{webReady: make(chan struct{})}
	if runtime.GOOS != "windows" {
		close(ui.webReady)
	}
	s := &Shell{UI: ui, Dir: dir, Log: logw, Hidden: hidden, TrayHost: TrayHost()}
	var services []application.Service
	// macOS posts notifications only from an app bundle; elsewhere the
	// service always starts.
	if runtime.GOOS != "darwin" || strings.Contains(os.Args[0], ".app/Contents/MacOS/") {
		ui.notes = notifications.New()
		services = append(services, application.NewService(ui.notes))
	} else {
		ui.noNotes = true
	}
	ui.app = application.New(application.Options{
		Name:        name.Product,
		Description: "Peer-to-peer blueprint review",
		Icon:        AppIcon(256),
		Services:    services,
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyAccessory,
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		Linux:   application.LinuxOptions{DisableQuitOnLastWindowClosed: true},
		// The system quitting (log out): stop the in-process daemon after
		// its running rounds, without asking.
		OnShutdown: func() {
			ui.quitOnce.Do(func() {})
			s.Stop()
		},
	})
	if ui.notes != nil {
		ui.notes.OnNotificationResponse(func(r notifications.NotificationResult) {
			if r.Error != nil {
				return
			}
			hash, _ := r.Response.UserInfo["hash"].(string)
			go s.Clicked(hash)
		})
	}

	ui.win = ui.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "main", Title: name.Product, Width: 1200, Height: 820, Hidden: true, URL: "about:blank",
	})
	if runtime.GOOS == "windows" {
		// The first navigation completes only once WebView2 has started.
		ui.win.OnWindowEvent(events.Windows.WebViewNavigationCompleted, func(*application.WindowEvent) {
			ui.webReadyOnce.Do(func() { close(ui.webReady) })
		})
	}
	ui.win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if s.isQuitting() {
			return
		}
		e.Cancel()
		go s.Close()
	})

	ui.menu = ui.app.NewMenu()
	ui.menu.Add("Open " + name.Product).OnClick(func(*application.Context) { go s.Open() })
	ui.menu.AddSeparator()
	ui.status = ui.menu.Add("Starting…").SetEnabled(false)
	ui.waiting = ui.menu.Add("").SetHidden(true)
	ui.waiting.OnClick(func(*application.Context) { go s.OpenWaiting() })
	ui.update = ui.menu.Add("").SetHidden(true)
	ui.update.OnClick(func(*application.Context) { go s.UpdateClicked() })
	ui.menu.AddSeparator()
	ui.pause = ui.menu.Add("Pause sync")
	ui.pause.OnClick(func(*application.Context) { go s.TogglePause() })
	ui.login = ui.menu.AddCheckbox("Start at login", false)
	ui.login.OnClick(func(*application.Context) { go s.ToggleStartAtLogin() })
	ui.menu.AddSeparator()
	ui.menu.Add("Quit " + name.Product).OnClick(func(*application.Context) { go s.Quit() })

	ui.tray = ui.app.SystemTray.New()
	ui.tray.SetMenu(ui.menu)
	ui.setIcon(TrayStarting)
	ui.tray.SetTooltip(name.Product)
	if runtime.GOOS != "darwin" {
		// A left click opens the window; the menu is on the right button.
		ui.tray.OnClick(func() { go s.Open() })
	}

	// A kalka:// link: macOS sends it as an event, to a running app or a
	// starting one; Windows and Linux also pass it as the argument, which
	// Main has read. Either way it only opens the join screen.
	ui.app.Event.OnApplicationEvent(events.Common.ApplicationLaunchedWithUrl, func(e *application.ApplicationEvent) {
		if u := e.Context().URL(); u != "" {
			go s.Link(u)
		}
	})
	ui.app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		if ui.notes != nil {
			go func() {
				if ok, err := ui.notes.RequestNotificationAuthorization(); err != nil || !ok {
					ui.mu.Lock()
					ui.noNotes = true
					ui.mu.Unlock()
				}
			}()
		}
		s.Start(plan)
	})
	if err := ui.app.Run(); err != nil {
		fmt.Fprintf(logw, "shell: %v\n", err)
		return 1
	}
	return 0
}

func (u *wailsUI) setIcon(st TrayState) {
	if runtime.GOOS == "darwin" {
		u.tray.SetTemplateIcon(TrayIcon(st, true))
	} else {
		u.tray.SetIcon(TrayIcon(st, false))
	}
}

func (u *wailsUI) SetTray(m TrayModel) {
	application.InvokeAsync(func() {
		u.status.SetLabel(m.Status)
		u.waiting.SetLabel(m.Waiting)
		u.waiting.SetHidden(m.Waiting == "")
		u.update.SetLabel(m.Update)
		u.update.SetHidden(m.Update == "")
		u.pause.SetLabel(m.PauseLabel)
		u.pause.SetEnabled(m.State != TrayStarting)
		u.login.SetChecked(m.StartAtLogin)
		u.login.SetEnabled(m.StartAtLoginEnabled)
		u.menu.Update()
		u.tray.SetTooltip(m.Tooltip)
		u.mu.Lock()
		changed := u.state != m.State
		u.state = m.State
		u.mu.Unlock()
		if changed {
			u.setIcon(m.State)
		}
	})
}

func (u *wailsUI) LoadWindow(url string) {
	u.mu.Lock()
	same := u.url == url
	u.url = url
	u.mu.Unlock()
	if !same {
		application.InvokeAsync(func() { u.win.SetURL(url) })
	}
}

func (u *wailsUI) ShowWindow(hash string) {
	application.InvokeAsync(func() {
		if u.win.IsMinimised() {
			u.win.Restore()
		}
		u.win.Show()
		u.win.Focus()
		if hash != "" {
			h, _ := json.Marshal(hash)
			u.win.ExecJS("location.hash = " + string(h))
		}
	})
}

func (u *wailsUI) HideWindow()     { application.InvokeAsync(func() { u.win.Hide() }) }
func (u *wailsUI) MinimiseWindow() { application.InvokeAsync(func() { u.win.Minimise() }) }

func (u *wailsUI) WindowVisible() bool {
	return application.InvokeSyncWithResult(func() bool { return u.win.IsVisible() && !u.win.IsMinimised() })
}

var errNoNotifications = errors.New("notifications are not available")

func (u *wailsUI) Notify(id, title, body, hash string) error {
	u.mu.Lock()
	off := u.noNotes || u.notes == nil
	u.mu.Unlock()
	if off {
		return errNoNotifications
	}
	return u.notes.SendNotification(notifications.NotificationOptions{ID: id, Title: title, Body: body,
		Data: map[string]interface{}{"hash": hash}})
}

func (u *wailsUI) Ask(title, message, yes, no string) bool {
	answer := make(chan bool, 1)
	application.InvokeAsync(func() {
		d := u.app.Dialog.Question().SetTitle(title).SetMessage(message)
		y := d.AddButton(yes).OnClick(func() { answer <- true })
		n := d.AddButton(no).OnClick(func() { answer <- false })
		d.SetDefaultButton(y)
		d.SetCancelButton(n)
		d.Show()
	})
	return <-answer
}

func (u *wailsUI) Info(title, message string) {
	application.InvokeAsync(func() { u.app.Dialog.Info().SetTitle(title).SetMessage(message).Show() })
}

func (u *wailsUI) Quit() {
	u.quitOnce.Do(func() {
		go func() {
			// The daemon has stopped already; only the window may still be
			// starting. Without WebView2 it never starts, hence the bound.
			select {
			case <-u.webReady:
			case <-time.After(webStartWait):
			}
			application.InvokeAsync(u.app.Quit)
		}()
	})
}
