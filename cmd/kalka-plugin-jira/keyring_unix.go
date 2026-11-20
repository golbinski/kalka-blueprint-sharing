//go:build !darwin && !windows

package main

// The Secret Service (GNOME Keyring, KWallet), through secret-tool from
// libsecret. The token is written on its standard input.

import (
	"os/exec"
	"strings"
)

type secretTool struct{ path string }

func systemKeyring() keyring {
	p, err := exec.LookPath("secret-tool")
	if err != nil {
		return nil
	}
	return secretTool{p}
}

func (s secretTool) set(account, secret string) error {
	cmd := exec.Command(s.path, "store", "--label", service+" "+account, "service", service, "account", account)
	cmd.Stdin = strings.NewReader(secret)
	return cmd.Run()
}

func (s secretTool) get(account string) (string, error) {
	out, err := exec.Command(s.path, "lookup", "service", service, "account", account).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (s secretTool) del(account string) error {
	return exec.Command(s.path, "clear", "service", service, "account", account).Run()
}
