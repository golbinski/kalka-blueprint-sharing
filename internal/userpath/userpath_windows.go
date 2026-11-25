//go:build windows

package userpath

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const envKey = `Environment`

func add(dir string) (bool, error) {
	return edit(func(v string) (string, bool) { return appendEntry(v, dir, expandEnv) })
}

func remove(dir string) (bool, error) {
	return edit(func(v string) (string, bool) { return dropEntry(v, dir, expandEnv) })
}

// edit reads the whole Path value, changes it and writes it back with the
// type it had. A value that cannot be read is never written: a PATH is
// changed only from what it was.
func edit(change func(string) (string, bool)) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, envKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, fmt.Errorf(`open HKCU\%s: %w`, envKey, err)
	}
	defer k.Close()
	value, typ, err := k.GetStringValue("Path")
	switch {
	case errors.Is(err, registry.ErrNotExist):
		value, typ = "", registry.EXPAND_SZ
	case err != nil:
		return false, fmt.Errorf(`read HKCU\%s\Path: %w; it was left as it is`, envKey, err)
	}
	next, changed := change(value)
	if !changed {
		return false, nil
	}
	switch {
	case next == "":
		err = k.DeleteValue("Path")
	case typ == registry.SZ:
		err = k.SetStringValue("Path", next)
	default:
		err = k.SetExpandStringValue("Path", next)
	}
	if err != nil {
		return false, fmt.Errorf(`write HKCU\%s\Path: %w`, envKey, err)
	}
	broadcast()
	return true, nil
}

func expandEnv(s string) string {
	if e, err := registry.ExpandString(s); err == nil {
		return e
	}
	return s
}

var sendMessageTimeout = windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")

// broadcast tells running programs (Explorer, so new terminals) that the
// environment changed. Best effort: the change is saved either way.
func broadcast() {
	const (
		hwndBroadcast   = 0xffff
		wmSettingChange = 0x001a
		smtoAbortIfHung = 0x0002
	)
	env, err := windows.UTF16PtrFromString("Environment")
	if err != nil || sendMessageTimeout.Find() != nil {
		return
	}
	var result uintptr
	sendMessageTimeout.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)),
		smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
}
