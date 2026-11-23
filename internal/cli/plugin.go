package cli

// kalka plugins, kalka plugin settings|set|clear (spec 10.6, 12.1). The
// views ask the running daemon, whose environment is the one that
// resolves references, and run the plugins here when none answers; set and
// clear run the plugin here, so a secret goes from the terminal to the
// plugin's stdin and through no socket, and then tell a running daemon.
// None of them needs `kalka setup`.

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"

	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/plugin"
)

// pluginHost is the host as `kalka check --with-plugins` builds it.
func pluginHost(dir string) *plugin.Host {
	return &plugin.Host{Dir: filepath.Join(dir, "plugins"), ExeDir: exeDir(), Log: io.Discard}
}

// pluginDaemon is the running daemon, or nil; it never starts one.
func pluginDaemon(dir string) *daemon.Client {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return answering(ctx, dir)
}

type pluginRow struct {
	plugin.Listed
	Status *plugin.Status `json:"status,omitempty"`
	Hint   *plugin.Hint   `json:"hint,omitempty"`
	// Error: its settings could not be read.
	Error string `json:"error,omitempty"`
}

type pluginsView struct {
	Daemon  bool        `json:"daemon"`
	Dirs    plugin.Dirs `json:"dirs"`
	Plugins []pluginRow `json:"plugins"`
}

// cmdPlugins implements `kalka plugins [--check] [--json]`.
func cmdPlugins(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("plugins", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	check := fs.Bool("check", false, "ask each plugin to try its credentials")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) > 0 {
		fmt.Fprintf(stderr, "usage: %s plugins [--check] [--json]\n", name.Binary)
		return exitUsage
	}
	dir, err := config.Dir()
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	ctx := context.Background()
	c := pluginDaemon(dir)
	h := pluginHost(dir)
	v := pluginsView{Daemon: c != nil}
	var list plugin.List
	if c != nil {
		if list, err = c.Plugins(ctx); err != nil {
			return failWith(*asJSON, stdout, stderr, err)
		}
	} else {
		list = h.List(ctx)
	}
	v.Dirs, v.Plugins = list.Dirs, []pluginRow{}
	for _, p := range list.Plugins {
		row := pluginRow{Listed: p}
		if p.Problem == "" && p.Capabilities.CanSettings() {
			var a *plugin.Answer
			if c != nil {
				a, err = c.PluginSettings(ctx, p.Scheme, *check)
			} else {
				a, err = h.Settings(ctx, p.Scheme, *check)
			}
			if err != nil {
				row.Error = err.Error()
			} else {
				row.Status, row.Hint = &a.Status, a.Hint
			}
		}
		v.Plugins = append(v.Plugins, row)
	}
	if *asJSON {
		writeJSON(stdout, v)
		return exitOK
	}
	if len(v.Plugins) == 0 {
		fmt.Fprintf(stdout, "no plugins are installed (looked in %s, beside %s, and on PATH)\n", v.Dirs.Config, name.Binary)
		return exitOK
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 3, ' ', 0)
	for _, p := range v.Plugins {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", p.Scheme, strings.TrimSpace(p.Name+" "+p.Version), sourceWords(p.Listed), capWords(p.Capabilities), rowState(p))
	}
	tw.Flush()
	for _, p := range v.Plugins {
		for _, s := range p.Shadowed {
			fmt.Fprintf(stdout, "%s: used instead of %s\n", p.Scheme, s)
		}
	}
	if c == nil {
		fmt.Fprintf(stdout, "(%s is not running: the plugins were run here)\n", name.Product)
	}
	return exitOK
}

func sourceWords(l plugin.Listed) string {
	switch l.Source {
	case plugin.SourceBundled:
		return "shipped with " + name.Product
	case plugin.SourceConfig:
		return "in " + filepath.Dir(l.File)
	}
	return "on PATH, " + l.File
}

func capWords(c *plugin.Capabilities) string {
	if c == nil {
		return "-"
	}
	var w []string
	if c.Resolve {
		w = append(w, "references")
	}
	if c.CanMeta() {
		w = append(w, "issue details")
	}
	if c.CanExport() {
		w = append(w, "export")
	}
	if len(w) == 0 {
		return "-"
	}
	return strings.Join(w, ", ")
}

func rowState(p pluginRow) string {
	switch {
	case p.Problem != "":
		return "cannot be used: " + p.Problem
	case p.Error != "":
		return "error: " + p.Error
	case p.Status == nil:
		return "nothing to set up"
	}
	return stateWords(*p.Status)
}

func stateWords(s plugin.Status) string {
	w := map[string]string{plugin.StateReady: "ready", plugin.StateNeedsSetup: "not set up", plugin.StateError: "error"}[s.State]
	if s.Checked {
		w += " (checked)"
	}
	return w + ": " + s.Message
}

// cmdPlugin implements `kalka plugin settings|set|clear`.
func cmdPlugin(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		pluginUsage(stderr)
		return exitUsage
	}
	switch args[0] {
	case "settings":
		return cmdPluginSettings(args[1:], stdout, stderr)
	case "set":
		return cmdPluginSet(args[1:], stdin, stdout, stderr)
	case "clear":
		return cmdPluginClear(args[1:], stdout, stderr)
	}
	pluginUsage(stderr)
	return exitUsage
}

func pluginUsage(w io.Writer) {
	fmt.Fprintf(w, `usage:
  %[1]s plugin settings <scheme> [--check] [--json]
  %[1]s plugin set <scheme> [<field>=<value>...] [--secret-stdin <field>] [--json]
  %[1]s plugin clear <scheme> [--json]
`, name.Binary)
}

type pluginSettingsView struct {
	Daemon bool `json:"daemon"`
	*plugin.Answer
}

// cmdPluginSettings implements `kalka plugin settings <scheme>`.
func cmdPluginSettings(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("plugin settings", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	check := fs.Bool("check", false, "ask the plugin to try its credentials")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) != 1 {
		fmt.Fprintf(stderr, "usage: %s plugin settings <scheme> [--check] [--json]\n", name.Binary)
		return exitUsage
	}
	dir, err := config.Dir()
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	ctx := context.Background()
	c := pluginDaemon(dir)
	var a *plugin.Answer
	if c != nil {
		a, err = c.PluginSettings(ctx, pos[0], *check)
	} else {
		a, err = pluginHost(dir).Settings(ctx, pos[0], *check)
	}
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	if *asJSON {
		writeJSON(stdout, pluginSettingsView{Daemon: c != nil, Answer: a})
		return exitOK
	}
	printAnswer(stdout, a)
	if c == nil {
		fmt.Fprintf(stdout, "(%s is not running: the plugin was run here)\n", name.Product)
	}
	return exitOK
}

func printAnswer(w io.Writer, a *plugin.Answer) {
	fmt.Fprintf(w, "%s: %s\n", a.Scheme, stateWords(a.Status))
	if a.Status.Detail != "" {
		fmt.Fprintf(w, "  %s\n", a.Status.Detail)
	}
	cur := a.Current()
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	for _, f := range a.Fields {
		if !plugin.Visible(f, cur) {
			continue
		}
		val := ""
		switch {
		case f.Kind == plugin.KindSecret && f.Set != nil && *f.Set:
			val = "stored"
		case f.Kind == plugin.KindSecret:
			val = "not stored"
		case f.Value != nil:
			val = fmt.Sprint(f.Value)
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", f.ID, f.Label, val)
	}
	tw.Flush()
	if a.Hint != nil {
		fmt.Fprintf(w, "  %s\n", a.Hint.Text)
		if a.Hint.Command != "" {
			fmt.Fprintf(w, "  run: %s\n", a.Hint.Command)
		}
		if a.Hint.URL != "" {
			fmt.Fprintf(w, "  see: %s\n", a.Hint.URL)
		}
	}
	if a.Incomplete {
		fmt.Fprintf(w, "  This plugin has settings this %s cannot show; update %[2]s to change them.\n", name.Binary, name.Product)
	}
}

// changedView is the JSON of set and clear.
type changedView struct {
	*plugin.Answer
	Daemon daemonRefresh `json:"daemon"`
}

type daemonRefresh struct {
	Refreshed bool           `json:"refreshed"`
	Status    *plugin.Status `json:"status"`
}

// isTerminal reports a terminal on stdin.
var isTerminal = func(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// readSecret reads a secret without echo from a terminal.
var readSecret = func(r io.Reader) (string, error) {
	b, err := term.ReadPassword(int(r.(*os.File).Fd()))
	return string(b), err
}

// cmdPluginSet implements `kalka plugin set <scheme> [<field>=<value>...]`.
func cmdPluginSet(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("plugin set", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	var secretFields []string
	fs.Func("secret-stdin", "read the secret `field` from standard input", func(s string) error {
		secretFields = append(secretFields, s)
		return nil
	})
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) < 1 {
		fmt.Fprintf(stderr, "usage: %s plugin set <scheme> [<field>=<value>...] [--secret-stdin <field>] [--json]\n", name.Binary)
		return exitUsage
	}
	if len(secretFields) > 1 {
		fmt.Fprintln(stderr, "--secret-stdin may be given once: standard input is one stream")
		return exitUsage
	}
	scheme, pairs := pos[0], pos[1:]
	for _, p := range pairs {
		if !strings.Contains(p, "=") {
			// The argument may be a value typed in the wrong place.
			fmt.Fprintln(stderr, "each setting is <field>=<value>")
			return exitUsage
		}
	}
	dir, err := config.Dir()
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	h := pluginHost(dir)
	ctx := context.Background()
	form, err := h.Settings(ctx, scheme, false)
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	if !form.Can.Apply {
		if form.Incomplete {
			return failWith(*asJSON, stdout, stderr, plugin.ErrIncomplete)
		}
		return failWith(*asJSON, stdout, stderr, plugin.ErrNoSettings)
	}
	values := map[string]any{}
	secrets := map[string]plugin.Secret{}
	for _, p := range pairs {
		id, val, _ := strings.Cut(p, "=")
		f, ok := form.Field(id)
		if ok && f.Kind == plugin.KindSecret {
			fmt.Fprintf(stderr, "%s is a secret: give it with --secret-stdin %s, never on the command line\n", id, id)
			return exitUsage
		}
		values[id] = typed(f, val)
	}
	switch {
	case len(secretFields) == 1:
		id := secretFields[0]
		label := id
		if f, ok := form.Field(id); ok {
			label = f.Label
		}
		s, err := readSecretFrom(stdin, stderr, label)
		if err != nil {
			return failWith(*asJSON, stdout, stderr, err)
		}
		secrets[id] = plugin.Secret(s)
	case len(pairs) == 0:
		if !isTerminal(stdin) {
			fmt.Fprintf(stderr, "nothing to set: give <field>=<value> or --secret-stdin <field>, or run it in a terminal to be asked\n")
			return exitUsage
		}
		if code := ask(form, stdin, stderr, values, secrets); code != exitOK {
			return code
		}
	}
	a, err := h.Apply(ctx, scheme, values, secrets)
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	return changed(dir, a, *asJSON, stdout, stderr)
}

// typed turns a command-line value into its field's type: bool takes
// true, false, on and off; everything else is text, checked by the host.
func typed(f plugin.Field, v string) any {
	if f.Kind == plugin.KindBool {
		switch strings.ToLower(v) {
		case "true", "on":
			return true
		case "false", "off":
			return false
		}
	}
	return v
}

// readSecretFrom reads one secret: without echo from a terminal, else up
// to plugin.MaxSecret bytes with one line ending removed.
func readSecretFrom(stdin io.Reader, stderr io.Writer, label string) (string, error) {
	if isTerminal(stdin) {
		fmt.Fprintf(stderr, "%s (not shown): ", label)
		s, err := readSecret(stdin)
		fmt.Fprintln(stderr)
		return s, err
	}
	b, err := io.ReadAll(io.LimitReader(stdin, plugin.MaxSecret+3))
	if err != nil {
		return "", err
	}
	s := string(b)
	s = strings.TrimSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\r")
	return s, nil
}

// ask prompts for each visible field in order, with the stored value as
// the default; Enter keeps it, and a stored secret.
func ask(form *plugin.Answer, stdin io.Reader, stderr io.Writer, values map[string]any, secrets map[string]plugin.Secret) int {
	in := bufio.NewReader(stdin)
	cur := form.Current()
	for _, f := range form.Fields {
		if !plugin.Visible(f, cur) {
			continue
		}
		if f.Kind == plugin.KindSecret {
			prompt := f.Label
			if f.Set != nil && *f.Set {
				prompt += " (stored; Enter keeps it)"
			}
			s, err := readSecretFrom(stdin, stderr, prompt)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return exitProblems
			}
			if strings.TrimSpace(s) != "" {
				secrets[f.ID] = plugin.Secret(s)
			}
			continue
		}
		def := ""
		if f.Value != nil {
			def = fmt.Sprint(f.Value)
		}
		prompt := f.Label
		if f.Kind == plugin.KindChoice {
			var vs []string
			for _, c := range f.Choices {
				vs = append(vs, c.Value)
			}
			prompt += " [" + strings.Join(vs, "/") + "]"
		}
		if f.Kind == plugin.KindBool {
			prompt += " [true/false]"
		}
		if def != "" {
			prompt += " (" + def + ")"
		}
		fmt.Fprint(stderr, prompt+": ")
		line, err := in.ReadString('\n')
		line = strings.TrimSpace(line)
		if err != nil && line == "" && !errors.Is(err, io.EOF) {
			fmt.Fprintln(stderr, err)
			return exitProblems
		}
		if line == "" || line == def {
			continue
		}
		values[f.ID] = typed(f, line)
		cur[f.ID] = values[f.ID]
	}
	return exitOK
}

// cmdPluginClear implements `kalka plugin clear <scheme>`.
func cmdPluginClear(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("plugin clear", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) != 1 {
		fmt.Fprintf(stderr, "usage: %s plugin clear <scheme> [--json]\n", name.Binary)
		return exitUsage
	}
	dir, err := config.Dir()
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	a, err := pluginHost(dir).Clear(context.Background(), pos[0])
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	return changed(dir, a, *asJSON, stdout, stderr)
}

// changed reports a stored change and tells a running daemon, warning
// when it sees the plugin otherwise. A failed refresh is only a warning:
// the change is stored, and the daemon's caches expire soon.
func changed(dir string, a *plugin.Answer, asJSON bool, stdout, stderr io.Writer) int {
	v := changedView{Answer: a}
	if c := pluginDaemon(dir); c != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		d, err := c.PluginRefresh(ctx, a.Scheme)
		if err != nil {
			fmt.Fprintf(stderr, "warning: the running %s was not told (%v); it sees the change within a minute\n", name.Product, err)
		} else {
			v.Daemon = daemonRefresh{Refreshed: true, Status: &d.Status}
			if d.Status.State != a.Status.State || d.Status.Message != a.Status.Message {
				fmt.Fprintf(stderr, "warning: the running %s sees this plugin as: %s, %s\n", name.Product, d.Status.State, d.Status.Message)
			}
		}
	}
	if asJSON {
		writeJSON(stdout, v)
		return exitOK
	}
	printAnswer(stdout, a)
	return exitOK
}

// pluginFields are the field messages of a plugin error, from the daemon
// or from the host here.
func pluginFields(err error) map[string]string {
	var re *daemon.RemoteError
	if errors.As(err, &re) {
		return re.Fields
	}
	return core.FieldsOf(err)
}
