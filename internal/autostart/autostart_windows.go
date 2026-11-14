package autostart

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/golbinski/kalka/internal/name"
)

const (
	runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
	// AppIDKey registers kalka's AppUserModelID, so toasts show "kalka"
	// as their sender with no Start-menu shortcut or package needed.
	appIDKey = `Software\Classes\AppUserModelId\` + name.AppID
)

// background returns kalkaw.exe next to exe.
func background(exe string) (string, error) {
	w := filepath.Join(filepath.Dir(exe), name.BackgroundBinary+".exe")
	if _, err := os.Stat(w); err != nil {
		return "", ErrNoBackgroundBinary
	}
	return w, nil
}

func enable(exe string) (Status, error) {
	w, err := background(exe)
	if err != nil {
		return Status{}, err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return Status{}, err
	}
	err = k.SetStringValue(name.Product, `"`+w+`" `+ArgString)
	k.Close()
	if err != nil {
		return Status{}, err
	}
	if err := RegisterAppID(); err != nil {
		return Status{}, err
	}
	return get()
}

// RegisterAppID writes the AppUserModelID registration toasts need.
func RegisterAppID() error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, appIDKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue("DisplayName", name.Product)
}

func disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err == nil {
		err = k.DeleteValue(name.Product)
		k.Close()
	}
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	if err := registry.DeleteKey(registry.CURRENT_USER, appIDKey); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

func get() (Status, error) {
	s := Status{Where: `HKCU\` + runKey + `\` + name.Product}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name.Product)
	if errors.Is(err, registry.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.Enabled, s.Command = true, v
	if rest, ok := strings.CutPrefix(v, `"`); ok {
		if prog, _, ok := strings.Cut(rest, `"`); ok {
			_, statErr := os.Stat(prog)
			s.Stale = statErr != nil
		}
	}
	return s, nil
}

const startsOnEnable = false

// start runs kalkaw.exe detached, with no console.
func start(exe string) error {
	w, err := background(exe)
	if err != nil {
		return err
	}
	return detached(w)
}

// spawn runs exe itself detached; DETACHED_PROCESS gives it no console.
func spawn(exe string) error { return detached(exe) }

func detached(exe string) error {
	cmd := exec.Command(exe, Args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
