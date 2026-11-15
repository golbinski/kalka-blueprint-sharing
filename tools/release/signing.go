package main

// Signing is a configuration step (spec 23.5). Settings come from the
// environment, or from a KEY=VALUE file outside the repository named by
// KALKA_SIGNING (the environment wins):
//
//	KALKA_WIN_SIGN_CMD        a command run on every .exe; {file} is the
//	                          path (appended when absent), for example
//	                          signtool sign /fd sha256 /tr http://ts.example /td sha256 /a {file}
//	KALKA_MAC_IDENTITY        a Developer ID Application identity for codesign
//	KALKA_MAC_NOTARY_PROFILE  a notarytool keychain profile; notarizes and
//	                          staples the app (needs the identity)
//
// With nothing set every artifact is reported "unsigned" and the build
// succeeds; macOS apps are then ad-hoc signed so they start on Apple
// Silicon. A configured signing step that fails fails the build.

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type signing struct {
	winCmd      string
	macIdentity string
	macNotary   string
}

func loadSigning(getenv func(string) string) (signing, error) {
	vals := map[string]string{}
	if p := getenv("KALKA_SIGNING"); p != "" {
		raw, err := os.ReadFile(p)
		if err != nil {
			return signing{}, fmt.Errorf("KALKA_SIGNING: %w", err)
		}
		sc := bufio.NewScanner(bytes.NewReader(raw))
		for n := 1; sc.Scan(); n++ {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				return signing{}, fmt.Errorf("%s:%d: want KEY=VALUE", p, n)
			}
			vals[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	get := func(k string) string {
		if v := getenv(k); v != "" {
			return v
		}
		return vals[k]
	}
	s := signing{winCmd: get("KALKA_WIN_SIGN_CMD"), macIdentity: get("KALKA_MAC_IDENTITY"), macNotary: get("KALKA_MAC_NOTARY_PROFILE")}
	if s.macNotary != "" && s.macIdentity == "" {
		return s, errors.New("KALKA_MAC_NOTARY_PROFILE needs KALKA_MAC_IDENTITY: only a Developer ID signed app can be notarized")
	}
	if s.winCmd != "" {
		if _, err := splitCommand(s.winCmd); err != nil {
			return s, fmt.Errorf("KALKA_WIN_SIGN_CMD: %w", err)
		}
	}
	return s, nil
}

// windows signs one executable with the configured command.
func (s signing) windows(path, label string, log io.Writer) error {
	if s.winCmd == "" {
		fmt.Fprintf(log, "unsigned: %s\n", label)
		return nil
	}
	args, _ := splitCommand(s.winCmd)
	found := false
	for i, a := range args {
		if strings.Contains(a, "{file}") {
			args[i], found = strings.ReplaceAll(a, "{file}", path), true
		}
	}
	if !found {
		args = append(args, path)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("signing %s: %w", label, err)
	}
	fmt.Fprintf(log, "signed: %s\n", label)
	return nil
}

// mac signs an app bundle: with the identity and the hardened runtime, or
// ad-hoc. inner are the executables in the bundle besides the main one.
func (s signing) mac(app, label string, inner []string, log io.Writer) error {
	if _, err := exec.LookPath("codesign"); err != nil {
		if s.macIdentity != "" {
			return fmt.Errorf("signing %s: codesign is not available on this host", label)
		}
		fmt.Fprintf(log, "unsigned: %s (not even ad-hoc signed: codesign needs macOS; it will not start on Apple Silicon until signed)\n", label)
		return nil
	}
	run := func(args ...string) error {
		cmd := exec.Command("codesign", args...)
		cmd.Stdout, cmd.Stderr = log, log
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("signing %s: codesign: %w", label, err)
		}
		return nil
	}
	if s.macIdentity == "" {
		if err := run(append([]string{"--force", "--sign", "-"}, append(inner, app)...)...); err != nil {
			return err
		}
		fmt.Fprintf(log, "unsigned: %s (ad-hoc signed)\n", label)
		return nil
	}
	flags := []string{"--force", "--options", "runtime", "--timestamp", "--sign", s.macIdentity}
	for _, p := range inner {
		if err := run(append(flags, p)...); err != nil {
			return err
		}
	}
	if err := run(append(flags, app)...); err != nil {
		return err
	}
	fmt.Fprintf(log, "signed: %s\n", label)
	return nil
}

// notarize submits a zip of the app and staples the ticket to the app.
func (s signing) notarize(zipPath, app, label string, log io.Writer) error {
	run := func(args ...string) error {
		cmd := exec.Command("xcrun", args...)
		cmd.Stdout, cmd.Stderr = log, log
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("notarizing %s: xcrun %s: %w", label, args[0], err)
		}
		return nil
	}
	if err := run("notarytool", "submit", zipPath, "--keychain-profile", s.macNotary, "--wait"); err != nil {
		return err
	}
	if err := run("stapler", "staple", app); err != nil {
		return err
	}
	fmt.Fprintf(log, "notarized: %s\n", label)
	return nil
}

// splitCommand splits a command line on spaces, keeping quoted parts
// ("..." or '...') together.
func splitCommand(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	in, quote := false, rune(0)
	for _, c := range s {
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteRune(c)
			}
		case c == '"' || c == '\'':
			quote, in = c, true
		case c == ' ' || c == '\t':
			if in {
				args = append(args, cur.String())
				cur.Reset()
				in = false
			}
		default:
			cur.WriteRune(c)
			in = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unclosed quote")
	}
	if in {
		args = append(args, cur.String())
	}
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	return args, nil
}
