package cli

// The commands T6 adds (plan 4.2): teams, waiting, promote, seed
// backlink, settings. `team uninvite` and `team sync` are in team.go.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/store"
)

// cmdTeams is `kalka teams`: the teams held, your role, members and what
// waits in each (a view).
func cmdTeams(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("teams", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) > 0 {
		fmt.Fprintf(stderr, "usage: %s teams [--json]\n", name.Binary)
		return exitUsage
	}
	vs, code := openViews(*asJSON, stdout, stderr)
	if vs == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ts, err := vs.teams(ctx)
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	if *asJSON {
		writeJSON(stdout, map[string]any{"daemon": vs.live(), "teams": ts})
		return exitOK
	}
	if len(ts) == 0 {
		fmt.Fprintf(stdout, "no teams yet: `%s team create <name>`, or `%[1]s join <link or token>`\n", name.Binary)
		return exitOK
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TEAM\tYOU\tMEMBERS\tWAITING\tHASH")
	for _, t := range ts {
		me := t.Me
		if t.Error != "" {
			me = "error: " + t.Error
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%s\n", t.Name, me, len(t.Members), t.Waiting, t.Team)
	}
	tw.Flush()
	if !vs.live() {
		notRunningNote(stdout)
	}
	return exitOK
}

// cmdWaiting is `kalka waiting [--team <t>]`, a view, and `kalka waiting
// --seen <file>`, which marks a handed-to-you item as seen through the
// daemon (spec 11.3).
func cmdWaiting(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("waiting", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	team := fs.String("team", "", "only this team (name or hash), or personal")
	seen := fs.String("seen", "", "mark the handed-to-you item of this file as seen")
	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) > 0 || (*seen != "" && *team != "") {
		fmt.Fprintf(stderr, "usage: %s waiting [--team <team>] [--json] | --seen <file> [--json]\n", name.Binary)
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if *seen != "" {
		c, code := daemonFor(*asJSON, stdout, stderr)
		if c == nil {
			return code
		}
		had, err := c.MarkSeen(ctx, absPath(*seen))
		if err != nil {
			return failWith(*asJSON, stdout, stderr, err)
		}
		if *asJSON {
			writeJSON(stdout, map[string]any{"path": absPath(*seen), "cleared": had})
			return exitOK
		}
		if had {
			fmt.Fprintf(stdout, "marked %s as seen\n", *seen)
		} else {
			fmt.Fprintf(stdout, "nothing handed to you waits in %s\n", *seen)
		}
		return exitOK
	}
	vs, code := openViews(*asJSON, stdout, stderr)
	if vs == nil {
		return code
	}
	items, err := vs.waiting(ctx, *team)
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	if *asJSON {
		writeJSON(stdout, map[string]any{"daemon": vs.live(), "items": items})
		return exitOK
	}
	if len(items) == 0 {
		fmt.Fprintln(stdout, "nothing waits for you")
	}
	for _, it := range items {
		where := it.Path
		if it.Kind == core.WaitTransfer {
			where = "transfer " + it.Transfer
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", it.TeamName, it.Kind, it.Message, where)
	}
	if !vs.live() {
		notRunningNote(stdout)
	}
	return exitOK
}

// cmdPromote is `kalka promote <file> [--status <s>]`: a seed becomes a
// blueprint (spec 6.11).
func cmdPromote(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("promote", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	status := fs.String("status", "", "the blueprint's status (default draft)")
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		fmt.Fprintf(stderr, "usage: %s promote <file> [--status <status>] [--json]\n", name.Binary)
		return exitUsage
	}
	c, code := daemonFor(*asJSON, stdout, stderr)
	if c == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	path := absPath(pos[0])
	if err := c.Promote(ctx, path, *status); err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	st := *status
	if st == "" {
		st = "draft"
	}
	if *asJSON {
		writeJSON(stdout, map[string]string{"promoted": path, "status": st})
		return exitOK
	}
	fmt.Fprintf(stdout, "%s is a blueprint now (status %s)\n", pos[0], st)
	return exitOK
}

// cmdBacklink is `kalka seed backlink <seed>`, a file command: the
// parent gets the seed's missing budded entry (spec 6.11).
func cmdBacklink(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	fs := flag.NewFlagSet("seed backlink", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) != 1 {
		fmt.Fprintf(stderr, "usage: %s seed backlink <seed> [--json]\n", name.Binary)
		return exitUsage
	}
	fail := func(err error) int { return failWith(*asJSON, stdout, stderr, fmt.Errorf("seed backlink: %w", err)) }
	email, sign, err := signerFromConfig()
	if err != nil {
		return fail(err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fail(err)
	}
	raw, err := os.ReadFile(pos[0])
	if err != nil {
		return fail(err)
	}
	seed, err := bp.Parse(raw)
	if err != nil {
		return fail(err)
	}
	if seed.Parent() == "" {
		return fail(errors.New("this document names no parent"))
	}
	st, err := store.Open(cfg.Folder)
	if err != nil {
		return fail(err)
	}
	var entry *bp.Entry
	parentPath := ""
	for attempt := 0; ; attempt++ {
		pf, err := st.Resolve(seed.Parent())
		if err == nil && pf == nil {
			err = errors.New("the parent blueprint is not in the folder")
		}
		if err == nil {
			parentPath = pf.Path
			var p *bp.Document
			if p, err = bp.Parse(pf.Raw); err == nil {
				if entry, err = bp.AddBacklink(p, seed, email, now(), sign); err == nil && entry != nil {
					err = fsutil.WriteAtomic(pf.Path, p.Bytes(), pf.Raw)
				}
			}
		}
		if errors.Is(err, fsutil.ErrChanged) && attempt < 3 {
			st.Forget(parentPath)
			continue
		}
		if err != nil {
			return fail(err)
		}
		break
	}
	id := ""
	if entry != nil {
		id = entry.ID
	}
	if *asJSON {
		writeJSON(stdout, map[string]any{"parent": parentPath, "entry": id, "added": entry != nil})
		return exitOK
	}
	if entry == nil {
		fmt.Fprintf(stdout, "%s has the backlink already\n", parentPath)
	} else {
		fmt.Fprintf(stdout, "added the backlink to %s (entry %s)\n", parentPath, id)
	}
	return exitOK
}

// settingsView is `kalka settings --json`.
type settingsView struct {
	Daemon             bool `json:"daemon"`
	Notifications      bool `json:"notifications"`
	KeepRunningOnClose bool `json:"keep_running_on_close"`
	Paused             bool `json:"paused"`
	StartAtLogin       bool `json:"start_at_login"`
}

// cmdSettings is `kalka settings [--notifications on|off] [--keep-running
// on|off]`: with no change it shows them, from state.yaml when kalka is
// not running.
func cmdSettings(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("settings", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	notes := fs.String("notifications", "", "on or off")
	keep := fs.String("keep-running", "", "keep kalka running when the window closes: on or off")
	use := func() int {
		fmt.Fprintf(stderr, "usage: %s settings [--notifications on|off] [--keep-running on|off] [--json]\n", name.Binary)
		return exitUsage
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil || len(pos) > 0 {
		return use()
	}
	var in daemon.Settings
	for _, f := range []struct {
		v   string
		dst **bool
	}{{*notes, &in.Notifications}, {*keep, &in.KeepRunningOnClose}} {
		switch f.v {
		case "":
		case "on", "off":
			b := f.v == "on"
			*f.dst = &b
		default:
			return use()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	change := in.Notifications != nil || in.KeepRunningOnClose != nil
	var set daemon.Settings
	live := true
	if change {
		c, code := daemonFor(*asJSON, stdout, stderr)
		if c == nil {
			return code
		}
		if set, err = c.ApplySettings(ctx, in); err != nil {
			return failWith(*asJSON, stdout, stderr, err)
		}
	} else {
		dir, err := config.Dir()
		if err != nil {
			return failWith(*asJSON, stdout, stderr, err)
		}
		if _, err := config.Load(); errors.Is(err, config.ErrMissing) {
			return report(*asJSON, stdout, stderr, core.KindNotSetUp, notSetUp)
		}
		if c := answering(ctx, dir); c != nil {
			if set, err = c.Settings(ctx); err != nil {
				return failWith(*asJSON, stdout, stderr, err)
			}
		} else {
			live = false
			st, err := daemon.LoadState(dir)
			if err != nil {
				return failWith(*asJSON, stdout, stderr, err)
			}
			set = st.Settings()
		}
	}
	v := settingsView{Daemon: live, Notifications: isOn(set.Notifications), KeepRunningOnClose: isOn(set.KeepRunningOnClose),
		Paused: isOn(set.Paused), StartAtLogin: isOn(set.StartAtLogin)}
	if *asJSON {
		writeJSON(stdout, v)
		return exitOK
	}
	word := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	fmt.Fprintf(stdout, "notifications  %s\nkeep running   %s\npaused         %s\nstart at login %s\n",
		word(v.Notifications), word(v.KeepRunningOnClose), word(v.Paused), word(v.StartAtLogin))
	if !live {
		notRunningNote(stdout)
	}
	return exitOK
}

func isOn(b *bool) bool { return b != nil && *b }

// isSubcommand reports whether args start with word, as `seed backlink`.
func isSubcommand(args []string, word string) bool {
	return len(args) > 0 && strings.EqualFold(args[0], word)
}
