package main

// The macOS Keychain, through /usr/bin/security. The token is written on
// the tool's standard input (security -i), never on its command line,
// where other processes could read it.

import (
	"bytes"
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

type keychain struct{}

func systemKeyring() keyring {
	if _, err := exec.LookPath("/usr/bin/security"); err != nil {
		return nil
	}
	return keychain{}
}

func (keychain) set(account, secret string) error {
	if !tokenRe.MatchString(secret) {
		return errors.New("the token has characters kalka-plugin-jira does not store")
	}
	cmd := exec.Command("/usr/bin/security", "-i")
	cmd.Stdin = strings.NewReader("add-generic-password -U -s " + strconv.Quote(service) + " -a " + strconv.Quote(account) + " -w " + strconv.Quote(secret) + "\n")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil || strings.Contains(out.String(), "error") {
		return errors.New("the Keychain did not take the token")
	}
	return nil
}

func (keychain) get(account string) (string, error) {
	out, err := exec.Command("/usr/bin/security", "find-generic-password", "-s", service, "-a", account, "-w").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (keychain) del(account string) error {
	return exec.Command("/usr/bin/security", "delete-generic-password", "-s", service, "-a", account).Run()
}
