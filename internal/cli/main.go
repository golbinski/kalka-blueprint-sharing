// Command kalka shares blueprints between teammates (see specification.md).
// Package cli is the kalka command line (spec 12).
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/identity"
	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/reference"
)

// Set at release build time with -ldflags
// "-X github.com/golbinski/kalka/internal/cli.version=...".
var version = "dev"

// Protocol numbers reported by `kalka version`.
const (
	wireProtocol   = 1 // data channel, spec 9.5
	pluginProtocol = 1 // plugin JSON, spec 10.3
)

// Exit codes.
const (
	exitOK       = 0
	exitProblems = 1 // check found errors, or a command failed
	exitUsage    = 2
)

// Run runs one kalka command line and returns the exit code. It is the
// whole `kalka` program; cmd/kalka and the desktop shell call it.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return run(args, stdin, stdout, stderr, time.Now)
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer, now func() time.Time) int {
	// --no-start applies to every daemon command, wherever it is given.
	noStart = false
	kept := args[:0:0]
	for _, a := range args {
		if a == "--no-start" || a == "-no-start" {
			noStart = true
			continue
		}
		kept = append(kept, a)
	}
	args = kept
	if len(args) == 0 || (strings.HasPrefix(args[0], "-") && args[0] != "-h" && args[0] != "--help") {
		return cmdStart(args, stdout, stderr)
	}
	cmd, rest := args[0], args[1:]
	if cmd == "help" || cmd == "-h" || cmd == "--help" {
		usage(stdout)
		return exitOK
	}
	if cmd == "rendezvous" {
		// The server is its own binary now (spec 9.6).
		fmt.Fprintf(stderr, "%s rendezvous moved: run %s, from its own archive in the release\n", name.Binary, name.RendezvousBinary)
		return exitUsage
	}
	if c, ok := commandByName(cmd); ok {
		return c.run(cmd, rest, stdin, stdout, stderr, now)
	}
	fmt.Fprintf(stderr, "%s: unknown command %q\n", name.Binary, cmd)
	usage(stderr)
	return exitUsage
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `usage:
  %[1]s                     make sure the daemon runs, then open its UI
  %[1]s daemon [--background] [--no-ui] [--no-notifications]   run the daemon
  %[1]s stop [--wait 1m] [--json]   stop the daemon after its running rounds finish
  %[1]s status [--json]       online state, members reachable, last sync, what waits, paused
  %[1]s pause | resume [--json]   go offline and back; kept across restarts
  %[1]s autostart on [--headless]|off|status [--json]
                                   start kalka at logon (the desktop app
                                   if installed; --headless: no window)
  %[1]s open [--print] [<link>]  open the running daemon's UI; with a kalka:// link, its join screen
  %[1]s setup --email <e> --rendezvous <url|peerjs-cloud> --folder <dir>
  %[1]s check <files or folders>... [--json] [--verbose] [--strict]
          [--with-plugins] [--domain <path>] | check --explain <rule>
  %[1]s invariants <file> [--json] [--domain <path>]   the invariants matrix
  %[1]s samples               copy the sample blueprints into Personal, to try kalka with
  %[1]s doctor [--json]       check this installation: config, key, daemon, network, clock, folder, plugins
  %[1]s plugins [--check] [--json]   the installed plugins, where each was found, and their state
  %[1]s plugin settings <scheme> [--check] [--json]   a plugin's state and settings
  %[1]s plugin set <scheme> [<field>=<value>...] [--secret-stdin <field>] [--json]
                                   change a plugin's settings (a secret only from stdin)
  %[1]s plugin clear <scheme> [--json]   remove a plugin's stored settings (log out)
  %[1]s comment <file> [--anchor <slug>] [--kind deviation] [--re <id>] [-m <text>] [--json]
  %[1]s seed <parent> --title <t> [--from <entry id>] [-m <problem>] [--why <text>] [--json]   split off a seed
  %[1]s seed backlink <seed> [--json]   add a missing backlink to the seed's parent
  %[1]s promote <file> [--status <s>] [--json]   a seed becomes a blueprint (default draft)
  %[1]s close <file> [--reopen] [--json]   the owner confirms the document delivered
  %[1]s context <file> [--domain <path>] [--json]   the plan and domain path for an agent
  %[1]s graph <file> [--domain <path>] [--json]     the graph canvas's lens: documents, items, open items
  %[1]s seeds [--team <t>|personal] [--json]   list the pre-backlog
  %[1]s teams [--json]        the teams you hold, your role and what waits in each
  %[1]s team create <name> | invite <email> [--expires 72h] | uninvite <id> | members | log | invites | sync
  %[1]s team remove <email> [--dry-run] | add-owner|remove-owner <email>   (any team command: --team <name>)
  %[1]s waiting [--team <t>] [--json] | --seen <file>   what only you can move forward
  %[1]s settings [--notifications on|off] [--keep-running on|off] [--auto-update on|off] [--json]
  %[1]s update [--check] [--json]   look for a new kalka, and install it and restart (--check: only look)
  %[1]s settings --rendezvous <url|peerjs-cloud> [--force]   switch the server (teammates must switch too)
  %[1]s join <link or token> [--wait 20s]   redeem an invite
  %[1]s join --show <link or token>         what the invite is, without joining
  %[1]s join --list [--json]                joins in progress or failed, with attempts and reason
  %[1]s join --forget <id, link or token>   stop a join in progress, or clear a failed one
  %[1]s publish <file> [--team <name>] [-m <note>]   move a Personal document into a team
  %[1]s release <file> [-m <text>] [--incorporates <id>,...]   release the next version
  %[1]s handoff <file> <email> [--json]   make another member the owner
  %[1]s reassign <file> <email> [--json]  team owners: make any member the owner
  %[1]s copy|move <file> --to <team> [--json]   offer a document to another team
  %[1]s transfers [--team <t>] [--json]   the pending transfers you decide or started
  %[1]s transfer accept <id> [--owner <email>] | decline <id> | withdraw <id> [--json]
  %[1]s delete <file>         delete a published document for the team
  %[1]s export <file> [--json]   send the released version to its kalka_issue (attachment and comment)
  %[1]s archive <file> [--undo] [--json]   hide a blueprint from lists, or show it again
  %[1]s domains [--team <t>] [--json]   list the team domain documents held here
  %[1]s reference export [-o <file>] [--force] | reference version   the blueprint reference for agents
  %[1]s version [--json]

Commands that act through kalka (team changes, join, publish, release, handoff,
reassign, delete, copy, move, transfer, promote, export, archive, waiting --seen, settings with an
option (--rendezvous alone writes config.yaml when kalka is not running), pause, resume) start it in the background when it is not running;
--no-start or %[2]s=1 makes them fail with kind "not-running" instead. Views (status,
teams, team members|log|invites, waiting, seeds, transfers, domains, settings, plugins,
plugin settings) never start it: with none running they read the local files (or run
the plugins) and their JSON says "daemon": false. check, context, invariants, comment,
seed, seed backlink, reference, plugin set and plugin clear never need it.
`, name.Binary, NoStartEnv)
}

// parseInterspersed parses flags that may come before or after positional
// arguments, as in `kalka comment plan.md --anchor plan`.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func cmdComment(args []string, stdin io.Reader, stdout, stderr io.Writer, now func() time.Time) int {
	fs := flag.NewFlagSet("comment", flag.ContinueOnError)
	fs.SetOutput(stderr)
	anchor := fs.String("anchor", "", "heading slug (or slug#node) the entry refers to; empty for a general entry")
	kind := fs.String("kind", bp.KindComment, "entry kind: comment, deviation or a custom kind")
	re := fs.String("re", "", "id of the entry this one replies to")
	msg := fs.String("m", "", "entry text; read from stdin when omitted")
	asJSON := fs.Bool("json", false, "machine-readable output")
	files, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(files) != 1 {
		fmt.Fprintln(stderr, "comment: name exactly one file")
		return exitUsage
	}
	*anchor = strings.TrimPrefix(*anchor, "§")

	cfg, err := config.Load()
	if err != nil {
		return failWith(*asJSON, stdout, stderr, fmt.Errorf("comment: %w", err))
	}
	if !bp.ValidEmail(cfg.Email) {
		fmt.Fprintf(stderr, "comment: configured email %q is not an email address\n", cfg.Email)
		return exitProblems
	}

	// Entries written here are signed with this installation's identity
	// key (spec 6.5), as in the UI.
	dir, err := config.Dir()
	if err != nil {
		return failWith(*asJSON, stdout, stderr, fmt.Errorf("comment: %w", err))
	}
	key, err := identity.LoadOrCreateKey(dir)
	if err != nil {
		return failWith(*asJSON, stdout, stderr, fmt.Errorf("comment: %w", err))
	}
	sign := fileSigner(cfg.Email, key)

	text := *msg
	if text == "" {
		b, err := io.ReadAll(io.LimitReader(stdin, bp.MaxEntryBody+1))
		if err != nil {
			return failWith(*asJSON, stdout, stderr, fmt.Errorf("comment: %w", err))
		}
		text = string(b)
	}
	path := files[0]
	// Re-read and redo if the file changes while we write (spec 14).
	for attempt := 0; ; attempt++ {
		raw, err := os.ReadFile(path)
		if err != nil {
			return failWith(*asJSON, stdout, stderr, fmt.Errorf("comment: %w", err))
		}
		d, err := bp.Parse(raw)
		if err != nil {
			return failWith(*asJSON, stdout, stderr, fmt.Errorf("comment: %s: %w", path, err))
		}
		e, err := bp.Append(d, *kind, cfg.Email, now(), *anchor, *re, "", text, sign(path))
		if err != nil {
			return failWith(*asJSON, stdout, stderr, fmt.Errorf("comment: %s: %w", path, err))
		}
		out := d.Bytes()
		err = fsutil.WriteAtomic(path, out, raw)
		if errors.Is(err, fsutil.ErrChanged) && attempt < 3 {
			continue
		}
		if err != nil {
			return failWith(*asJSON, stdout, stderr, fmt.Errorf("comment: %w", err))
		}
		if *asJSON {
			writeJSON(stdout, map[string]string{"file": path, "id": e.ID, "kind": e.Kind, "anchor": e.Anchor, "rev": e.Rev})
		} else {
			fmt.Fprintf(stdout, "added %s %s to %s\n", e.Kind, e.ID, path)
		}
		return exitOK
	}
}

func cmdVersion(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "machine-readable output")
	if _, err := parseInterspersed(fs, args); err != nil {
		return exitUsage
	}
	if *asJSON {
		writeJSON(stdout, map[string]any{"version": version, "wire_protocol": wireProtocol, "plugin_protocol": pluginProtocol, "reference": reference.Version})
	} else {
		fmt.Fprintf(stdout, "%s %s (wire protocol %d, plugin protocol %d, reference %s)\n", name.Binary, version, wireProtocol, pluginProtocol, reference.Version)
	}
	return exitOK
}
