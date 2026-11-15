package cli

// `kalka copy`, `kalka move`, `kalka transfers` and `kalka transfer
// accept|decline|withdraw` (spec 20.7).

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/golbinski/kalka/internal/daemon"
	"github.com/golbinski/kalka/internal/name"
)

// cmdTransferStart implements `kalka copy|move <file> --to <team>`.
func cmdTransferStart(mode string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(mode, flag.ContinueOnError)
	fs.SetOutput(stderr)
	to := fs.String("to", "", "the destination team")
	j := fs.Bool("json", false, "print JSON")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) != 1 || *to == "" {
		fmt.Fprintf(stderr, "usage: %s %s <file> --to <team> [--json]\n", name.Binary, mode)
		return exitUsage
	}
	c, code := daemonFor(*j, stdout, stderr)
	if c == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, err := c.Transfer(ctx, daemon.TransferRequest{Path: absPath(pos[0]), Team: *to, Mode: mode})
	if err != nil {
		return failWith(*j, stdout, stderr, err)
	}
	if *j {
		writeJSON(stdout, r)
		return exitOK
	}
	fmt.Fprintf(stdout, "offered %s to %s as a %s (transfer %s); a team owner of %s accepts or declines it\n", r.Source, r.Team, r.Mode, r.ID, r.Team)
	return exitOK
}

// cmdTransfers implements `kalka transfers`: the pending transfers of the
// teams you own, and your own.
func cmdTransfers(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("transfers", flag.ContinueOnError)
	fs.SetOutput(stderr)
	j := fs.Bool("json", false, "print JSON")
	team := fs.String("team", "", "only transfers into or out of this team (name or hash)")
	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) > 0 {
		return exitUsage
	}
	vs, code := openViews(*j, stdout, stderr)
	if vs == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ts, err := vs.transfers(ctx, *team)
	if err != nil {
		return failWith(*j, stdout, stderr, err)
	}
	if *j {
		writeJSON(stdout, map[string]any{"daemon": vs.live(), "transfers": ts})
		return exitOK
	}
	if len(ts) == 0 {
		fmt.Fprintln(stdout, "no pending transfers")
		return exitOK
	}
	for _, t := range ts {
		who := t.Actor
		if t.Mine {
			who = "you"
		}
		fmt.Fprintf(stdout, "%s  %s “%s” from %s into %s, by %s, %d entries attested: %s\n", t.ID, t.Mode, t.Title, t.From, t.Team, who, t.Entries, t.Status)
	}
	return exitOK
}

// cmdTransfer implements `kalka transfer accept <id> [--owner <email>] |
// decline <id> | withdraw <id>`.
func cmdTransfer(args []string, stdout, stderr io.Writer) int {
	use := func() int {
		fmt.Fprintf(stderr, "usage: %s transfer accept <id> [--owner <email>] | decline <id> | withdraw <id> [--json]\n", name.Binary)
		return exitUsage
	}
	if len(args) == 0 {
		return use()
	}
	action := args[0]
	if action != "accept" && action != "decline" && action != "withdraw" {
		return use()
	}
	fs := flag.NewFlagSet("transfer "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	owner := fs.String("owner", "", "accept: the member who owns version 1 (default: you)")
	j := fs.Bool("json", false, "print JSON")
	pos, err := parseInterspersed(fs, args[1:])
	if err != nil || len(pos) != 1 || (*owner != "" && action != "accept") {
		return use()
	}
	c, code := daemonFor(*j, stdout, stderr)
	if c == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	v, err := c.DecideTransfer(ctx, pos[0], action, *owner)
	if err != nil {
		return failWith(*j, stdout, stderr, err)
	}
	if *j {
		writeJSON(stdout, v)
		return exitOK
	}
	switch action {
	case "accept":
		fmt.Fprintf(stdout, "accepted %s into %s; %s owns version 1", pos[0], v.Team, v.Owner)
		if v.Path != "" {
			fmt.Fprintf(stdout, " (%s)", v.Path)
		}
		fmt.Fprintln(stdout)
	case "decline":
		fmt.Fprintf(stdout, "declined %s; every member discards it\n", pos[0])
	default:
		fmt.Fprintf(stdout, "withdrew %s\n", pos[0])
	}
	return exitOK
}
