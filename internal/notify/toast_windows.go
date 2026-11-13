package notify

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/golbinski/kalka/internal/name"
)

// The scripts are constants; every value reaches them through the
// environment, so nothing from a blueprint is ever parsed as PowerShell.
const showScript = `$ErrorActionPreference = 'Stop'
[void][Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
[void][Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom, ContentType = WindowsRuntime]
$x = New-Object Windows.Data.Xml.Dom.XmlDocument
$x.LoadXml($env:KALKA_TOAST_XML)
$t = New-Object Windows.UI.Notifications.ToastNotification $x
$t.Tag = $env:KALKA_TOAST_TAG
$t.Group = 'kalka'
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($env:KALKA_TOAST_APP).Show($t)
`

const removeScript = `$ErrorActionPreference = 'Stop'
[void][Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
[Windows.UI.Notifications.ToastNotificationManager]::History.Remove($env:KALKA_TOAST_TAG, 'kalka', $env:KALKA_TOAST_APP)
`

// Toast shows a Windows notification that opens url, waits until ctx
// ends (the prompt was answered or timed out) and then removes it from
// the notification centre. It needs the AppUserModelID registration that
// `kalka autostart on` writes; without it, it returns ErrUnavailable and
// the caller shows the message box instead.
func Toast(ctx context.Context, title, message, url string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\AppUserModelId\`+name.AppID, registry.QUERY_VALUE)
	if err != nil {
		return ErrUnavailable
	}
	k.Close()
	doc, err := toastXML(title, message, url)
	if err != nil {
		return err
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	tag := hex.EncodeToString(b)
	env := []string{"KALKA_TOAST_XML=" + doc, "KALKA_TOAST_TAG=" + tag, "KALKA_TOAST_APP=" + name.AppID}
	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = powershell(sctx, showScript, env)
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	<-ctx.Done()
	rctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = powershell(rctx, removeScript, env)
	return ctx.Err()
}

// powershell runs a constant script with no window, by absolute path.
func powershell(ctx context.Context, script string, env []string) error {
	exe := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, exe, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encode(script))
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("toast: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// encode is -EncodedCommand's format: base64 of UTF-16LE.
func encode(s string) string {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return base64.StdEncoding.EncodeToString(b)
}
