package notify

import (
	"context"
	"runtime"
	"time"

	"golang.org/x/sys/windows"
)

const (
	mbOKCancel        = 0x00000001
	mbIconInformation = 0x00000040
	mbSetForeground   = 0x00010000
	mbTopmost         = 0x00040000
	idOK              = 1
	wmClose           = 0x0010
)

var (
	user32            = windows.NewLazySystemDLL("user32.dll")
	enumThreadWindows = user32.NewProc("EnumThreadWindows")
	postMessage       = user32.NewProc("PostMessageW")
	// closeWindow is created once: Windows allows only a limited number of
	// callbacks per process.
	closeWindow = windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		postMessage.Call(hwnd, wmClose, 0, 0)
		return 1
	})
)

// ask shows a message box. The daemon runs per user at logon (not as a
// Windows service), so the box appears on the user's desktop. OK opens
// kalka; Cancel, or closing the box, means later.
func ask(ctx context.Context, title, message string) (bool, error) {
	t, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return false, err
	}
	m, err := windows.UTF16PtrFromString(message + "\n\nOK opens kalka. Cancel answers later.")
	if err != nil {
		return false, err
	}
	type result struct {
		id  int32
		err error
	}
	done := make(chan result, 1)
	tid := make(chan uint32, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		tid <- windows.GetCurrentThreadId()
		id, err := windows.MessageBox(0, m, t, mbOKCancel|mbIconInformation|mbSetForeground|mbTopmost)
		done <- result{id, err}
	}()
	thread := <-tid
	select {
	case r := <-done:
		if r.id == 0 {
			return false, r.err
		}
		return r.id == idOK, nil
	case <-ctx.Done():
	}
	// Close the box: WM_CLOSE acts as Cancel. The box may not exist yet,
	// so keep asking until its thread returns.
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		enumThreadWindows.Call(uintptr(thread), closeWindow, 0)
		select {
		case <-done:
			return false, ctx.Err()
		case <-tick.C:
		}
	}
}
