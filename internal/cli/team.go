package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/config"
	"github.com/golbinski/kalka/internal/core"
	"github.com/golbinski/kalka/internal/name"
)

const teamUsage = `usage:
  %[1]s team create <name>
  %[1]s team invite <email> [--expires 72h]
  %[1]s team uninvite <invite id>
  %[1]s team members | log | invites
  %[1]s team sync              replicate with every member now
  %[1]s team remove <email> [--dry-run]
  %[1]s team add-owner <email> | remove-owner <email>
  every subcommand takes --team <name> when you are in several teams, and --json
`

// cmdTeam implements `kalka team` (spec 12, 19).
func cmdTeam(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("team", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	team := fs.String("team", "", "the team, by name or hash, when you are in several")
	expires := fs.Duration("expires", 0, "how long an invite is valid (default 72h)")
	dry := fs.Bool("dry-run", false, "list the documents a removal would reassign to you, and remove no one")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	bad := func() int { fmt.Fprintf(stderr, teamUsage, name.Binary); return exitUsage }
	if len(pos) == 0 {
		return bad()
	}
	need := map[string]int{"create": 2, "invite": 2, "uninvite": 2, "members": 1, "log": 1, "invites": 1, "remove": 2,
		"add-owner": 2, "remove-owner": 2, "sync": 1}
	if n, ok := need[pos[0]]; !ok || len(pos) != n {
		return bad()
	}
	if (pos[0] == "invite" || pos[0] == "remove" || pos[0] == "add-owner" || pos[0] == "remove-owner") && !bp.ValidEmail(pos[1]) {
		fmt.Fprintf(stderr, "%q is not an email address\n", pos[1])
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	fail := func(err error) int { return failWith(*asJSON, stdout, stderr, err) }
	if pos[0] == "members" || pos[0] == "log" || pos[0] == "invites" {
		vs, code := openViews(*asJSON, stdout, stderr)
		if vs == nil {
			return code
		}
		v, err := vs.team(ctx, *team)
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			writeJSON(stdout, struct {
				Daemon bool `json:"daemon"`
				core.TeamView
			}{vs.live(), v})
			return exitOK
		}
		switch pos[0] {
		case "log":
			printLog(stdout, v)
		case "invites":
			printInvites(stdout, v)
		default:
			printMembers(stdout, v)
		}
		if !vs.live() {
			notRunningNote(stdout)
		}
		return exitOK
	}
	c, code := daemonFor(*asJSON, stdout, stderr)
	if c == nil {
		return code
	}

	var v core.TeamView
	switch pos[0] {
	case "uninvite":
		if err := c.CancelInvite(ctx, pos[1]); err != nil {
			return fail(err)
		}
		if *asJSON {
			writeJSON(stdout, map[string]string{"canceled": pos[1]})
			return exitOK
		}
		fmt.Fprintf(stdout, "canceled invite %s; its link and token no longer work\n", pos[1])
		return exitOK
	case "sync":
		st, err := c.Replicate(ctx, *team)
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			writeJSON(stdout, st)
			return exitOK
		}
		if st.Error != "" {
			fmt.Fprintf(stdout, "sync: %s\n", st.Error)
		}
		fmt.Fprintf(stdout, "reached %d of %d members", len(st.Reached), st.Tried)
		if st.Received > 0 {
			fmt.Fprintf(stdout, ", received %d documents", st.Received)
		}
		fmt.Fprintln(stdout)
		for _, r := range st.Refused {
			fmt.Fprintf(stdout, "refused “%s” from %s: %s\n", r.Title, r.From, r.Reason)
		}
		return exitOK
	case "create":
		if v, err = c.CreateTeam(ctx, pos[1]); err != nil {
			return fail(err)
		}
		if *asJSON {
			writeJSON(stdout, v)
			return exitOK
		}
		fmt.Fprintf(stdout, "created team %s; you are its owner\nlog: %s\ninvite people with `%s team invite <email>`\n", v.Name, v.Log, name.Binary)
		return exitOK
	case "invite":
		exp := ""
		if *expires != 0 {
			exp = expires.String()
		}
		r, err := c.Invite(ctx, *team, pos[1], exp)
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			writeJSON(stdout, r)
			return exitOK
		}
		fmt.Fprintf(stderr, "invite for %s to %s, valid until %s. Send them the link, or the token if the link does not open, through any channel; "+
			"it works once, only for %[1]s, and only while your kalka is online:\n", r.Email, r.Name, r.Expires.Local().Format("Mon 2 Jan 15:04"))
		fmt.Fprintln(stdout, r.Link)
		fmt.Fprintln(stdout, r.Token)
		fmt.Fprintf(stderr, "they open the link, paste either line under Join with link or token, or run: %s join <link or token>\n", name.Binary)
		return exitOK
	case "remove":
		docs, perr := c.RemovalPreview(ctx, *team, pos[1])
		if perr != nil {
			return fail(perr)
		}
		if *dry {
			if *asJSON {
				writeJSON(stdout, map[string]any{"email": pos[1], "reassigned": docs})
				return exitOK
			}
			if len(docs) == 0 {
				fmt.Fprintf(stdout, "removing %s reassigns no documents\n", pos[1])
			} else {
				fmt.Fprintf(stdout, "removing %s reassigns these published documents to you:\n", pos[1])
			}
			for _, d := range docs {
				dom := ""
				if d.Domain != "" {
					dom = "domain " + d.Domain + ", "
				}
				fmt.Fprintf(stdout, "  %s  (%s%s, released up to v%d)\n", d.Title, dom, d.Path, d.After)
			}
			return exitOK
		}
		v, err = c.Remove(ctx, *team, pos[1])
	case "add-owner":
		v, err = c.SetOwner(ctx, *team, pos[1], true)
	case "remove-owner":
		v, err = c.SetOwner(ctx, *team, pos[1], false)
	}
	if err != nil {
		return fail(err)
	}
	if *asJSON {
		writeJSON(stdout, v)
		return exitOK
	}
	printMembers(stdout, v)
	if v.Warning != "" {
		fmt.Fprintln(stderr, "warning: "+v.Warning)
	}
	return exitOK
}

func printMembers(w io.Writer, v core.TeamView) {
	fmt.Fprintf(w, "team %s (%s), you are %s\n", v.Name, v.Team[:12], strings.Replace(v.Me, "none", "not a member", 1))
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "EMAIL\tROLE\tFINGERPRINT\t")
	for _, m := range v.Members {
		role := "member"
		if m.Owner {
			role = "owner"
		}
		you := ""
		if m.You {
			you = "you"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", m.Email, role, m.Fingerprint, you)
	}
	tw.Flush()
	if n := len(v.BadLines) + v.Rejected; n > 0 {
		fmt.Fprintf(w, "%d log line(s) do not count: damaged or badly signed; see `%s team log`\n", n, name.Binary)
	}
	for _, d := range v.NotShared {
		fmt.Fprintf(w, "not shared: %s: %s\n", d.Rel, d.Reason)
	}
}

func printLog(w io.Writer, v core.TeamView) {
	for _, h := range v.History {
		mark := ""
		if !h.Effective {
			mark = "  [does not count: " + h.Reason + "]"
		}
		fmt.Fprintf(w, "%s  %s  %s %s%s\n", h.Hash, h.At, h.Signer, h.Text, mark)
	}
	for _, b := range v.BadLines {
		fmt.Fprintf(w, "line %d: %s\n", b.Line, b.Err)
	}
}

func printInvites(w io.Writer, v core.TeamView) {
	if len(v.Invites) == 0 {
		fmt.Fprintln(w, "no invites made on this installation")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "EMAIL\tSTATE\tEXPIRES")
	for _, iv := range v.Invites {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", iv.Email, iv.State, iv.Expires.Local().Format("2006-01-02 15:04"))
	}
	tw.Flush()
	// The links last: they are long, and copied whole.
	for _, iv := range v.Invites {
		if iv.Link != "" {
			fmt.Fprintf(w, "\n%s:\n%s\n", iv.Email, iv.Link)
		}
	}
}

// cmdJoin implements `kalka join <link or token>` and `kalka join --show`
// (spec 19.7).
func cmdJoin(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("join", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	show := fs.Bool("show", false, "show what the invite is, without joining")
	wait := fs.Duration("wait", 20*time.Second, "how long to wait for the owner before leaving it to the daemon")
	list := fs.Bool("list", false, "list the joins in progress or failed")
	forget := fs.Bool("forget", false, "stop a join in progress, or clear a failed one")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) == 0 && !*list {
		fmt.Fprintf(stderr, "usage: %s join [--show | --forget] <link or token> [--wait 20s] [--json]\n       %s join --list [--json]\n", name.Binary, name.Binary)
		return exitUsage
	}
	token := strings.Join(pos, "")
	if *show {
		return joinShow(token, *asJSON, stdout, stderr)
	}
	c, code := daemonFor(*asJSON, stdout, stderr)
	if c == nil {
		return code
	}
	if *list || *forget {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if *forget {
			if err := c.ForgetJoin(ctx, token); err != nil {
				return failWith(*asJSON, stdout, stderr, err)
			}
			if *asJSON {
				writeJSON(stdout, map[string]bool{"forgotten": true})
			} else {
				fmt.Fprintln(stdout, "join forgotten")
			}
			return exitOK
		}
		js, err := c.Joins(ctx)
		if err != nil {
			return failWith(*asJSON, stdout, stderr, err)
		}
		if *asJSON {
			writeJSON(stdout, js)
			return exitOK
		}
		if len(js) == 0 {
			fmt.Fprintln(stdout, "no joins")
		}
		for _, j := range js {
			line := fmt.Sprintf("%s  %-8s %s", j.ID, j.State, j.Name)
			if j.Inviter != "" {
				line += ", invited by " + j.Inviter
			}
			if j.State == core.JoinPending {
				line += fmt.Sprintf(", %d tries, valid until %s", j.Tries, j.Expires.Local().Format("2 Jan 15:04"))
			}
			if j.Detail != "" {
				line += ": " + j.Detail
			}
			fmt.Fprintln(stdout, line)
		}
		return exitOK
	}
	ctx, cancel := context.WithTimeout(context.Background(), *wait+10*time.Second)
	defer cancel()
	j, err := c.Join(ctx, token)
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	deadline := time.Now().Add(*wait)
	for j.State == core.JoinPending && time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		js, err := c.Joins(ctx)
		if err != nil {
			break
		}
		for _, x := range js {
			if x.ID == j.ID {
				j = x
			}
		}
	}
	if *asJSON {
		writeJSON(stdout, j)
	}
	switch j.State {
	case core.JoinDone:
		if !*asJSON {
			fmt.Fprintf(stdout, "joined team %s; see `%s team members`\n", j.Name, name.Binary)
		}
		return exitOK
	case core.JoinFailed:
		if !*asJSON {
			fmt.Fprintf(stderr, "joining %s failed: %s\n", j.Name, j.Detail)
		}
		return exitProblems
	}
	if !*asJSON {
		msg := "the team owner was not reached yet"
		if j.Detail != "" {
			msg = j.Detail
		}
		fmt.Fprintf(stdout, "%s.\nkalka keeps trying in the background until %s; `%s team members` shows the team once you are in\n",
			strings.TrimSuffix(msg, "."), j.Expires.Local().Format("Mon 2 Jan 15:04"), name.Binary)
	}
	return exitOK
}

// cmdPublish implements `kalka publish <file> [--team <t>] [-m <note>]`
// (spec 20.1): a Personal document moves into a team with its first
// release.
func cmdPublish(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("publish", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	team := fs.String("team", "", "the team to publish to (default: your only team)")
	note := fs.String("m", "", "a note for the release")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) != 1 {
		fmt.Fprintf(stderr, "usage: %s publish <file> [--team <name>] [-m <note>] [--json]\n", name.Binary)
		return exitUsage
	}
	c, code := daemonFor(*asJSON, stdout, stderr)
	if c == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, err := c.Publish(ctx, absPath(pos[0]), *team, *note)
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	if *asJSON {
		writeJSON(stdout, r)
		return exitOK
	}
	fmt.Fprintf(stdout, "published %s to %s as version %d; it replicates to the team's members\n", r.Path, r.Team, r.Version)
	if r.LocalOnly > 0 {
		fmt.Fprintf(stdout, "%d entries stay in your file only: unsigned, or by someone outside the team\n", r.LocalOnly)
	}
	return exitOK
}

// cmdDelete implements `kalka delete <file>` (spec 20.6).
func cmdDelete(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) != 1 {
		fmt.Fprintf(stderr, "usage: %s delete <file> [--json]\n", name.Binary)
		return exitUsage
	}
	c, code := daemonFor(*asJSON, stdout, stderr)
	if c == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Delete(ctx, absPath(pos[0])); err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	if *asJSON {
		writeJSON(stdout, map[string]string{"deleted": absPath(pos[0])})
		return exitOK
	}
	fmt.Fprintf(stdout, "deleted %s for the team; the last copy is in the team folder's .kalka/trash\n", pos[0])
	return exitOK
}

func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// cmdRelease implements `kalka release <file> [-m <text>] [--incorporates
// <id>,...]` (spec 20.4): the next version of any team document.
func cmdRelease(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	note := fs.String("m", "", "a summary of the version")
	inc := fs.String("incorporates", "", "comma-separated ids of the comments this version takes up")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(pos) != 1 {
		fmt.Fprintf(stderr, "usage: %s release <file> [-m <text>] [--incorporates <id>,...] [--json]\n", name.Binary)
		return exitUsage
	}
	var ids []string
	for _, x := range strings.Split(*inc, ",") {
		if x = strings.TrimSpace(x); x != "" {
			ids = append(ids, x)
		}
	}
	c, code := daemonFor(*asJSON, stdout, stderr)
	if c == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r, err := c.Release(ctx, absPath(pos[0]), *note, ids)
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	if *asJSON {
		writeJSON(stdout, r)
		return exitOK
	}
	fmt.Fprintf(stdout, "released version %d of %s; it replicates to the members of %s\n", r.Version, r.Path, r.Team)
	return exitOK
}

// cmdDomains implements `kalka domains [--json]` (spec 6.12).
func cmdDomains(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("domains", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON")
	team := fs.String("team", "", "only this team (name or hash)")
	if pos, err := parseInterspersed(fs, args); err != nil || len(pos) > 0 {
		return exitUsage
	}
	vs, code := openViews(*asJSON, stdout, stderr)
	if vs == nil {
		return code
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rows, err := vs.domains(ctx, *team)
	if err != nil {
		return failWith(*asJSON, stdout, stderr, err)
	}
	if *asJSON {
		writeJSON(stdout, map[string]any{"daemon": vs.live(), "domains": rows})
		return exitOK
	}
	if len(rows) == 0 {
		fmt.Fprintln(stdout, "no domain documents held here")
		return exitOK
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "DOMAIN\tVERSION\tOWNER\tOPEN\tFILE")
	for _, r := range rows {
		ver := fmt.Sprintf("v%d", r.Version)
		if r.Version == 0 {
			ver = "draft"
		} else if r.Unreleased {
			ver += " + edits"
		}
		owner := r.Owner
		switch {
		case owner == "":
			owner = "(not assigned)"
		case r.You:
			owner = "you"
		}
		fmt.Fprintf(tw, "%s/%s\t%s\t%s\t%d\t%s\n", r.Team, r.Domain, ver, owner, r.Open, r.Rel)
	}
	tw.Flush()
	if !vs.live() {
		notRunningNote(stdout)
	}
	return exitOK
}

// joinShow is `kalka join --show`: what the join screen shows. It asks
// the daemon when one runs, for the membership and email checks, and
// otherwise reads the token alone.
func joinShow(token string, asJSON bool, stdout, stderr io.Writer) int {
	var p core.JoinPreview
	var err error
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dir, _ := config.Dir()
	if c := answering(ctx, dir); c != nil {
		p, err = c.JoinPreview(ctx, token)
	} else {
		p, err = core.PreviewInvite(token)
		if err == nil {
			p.Message = fmt.Sprintf("%s is not running, so whether you can join here was not checked", name.Product)
		}
	}
	if err != nil {
		return failWith(asJSON, stdout, stderr, err)
	}
	if asJSON {
		writeJSON(stdout, p)
		return exitOK
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "team\t%s (%s)\n", p.Name, p.Team[:12])
	fmt.Fprintf(tw, "invited by\t%s\n", p.Inviter)
	fmt.Fprintf(tw, "for\t%s\n", p.Email)
	fmt.Fprintf(tw, "valid until\t%s\n", p.Expires.Local().Format("Mon 2 Jan 15:04"))
	fmt.Fprintf(tw, "rendezvous\t%s\n", p.Rendezvous)
	tw.Flush()
	if p.Joining != nil {
		fmt.Fprintf(stdout, "this kalka is already joining: %s\n", p.Joining.State)
	}
	switch {
	case p.Problem != "":
		fmt.Fprintf(stdout, "cannot join: %s\n", p.Message)
	case p.Message != "":
		fmt.Fprintln(stdout, p.Message)
	default:
		fmt.Fprintf(stdout, "join with: %s join %s\n", name.Binary, p.Token)
	}
	return exitOK
}
