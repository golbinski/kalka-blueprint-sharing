package cli

// The command table (spec 12). Every command has a class:
//
//   - workflow: changes something; the app has an action for it;
//   - view: shows something; the app has a place for it;
//   - process: starts, shows or stops kalka itself, exempt from parity.
//
// How a command reaches kalka (a running daemon, the files, or neither)
// is its own business; see access.go.

import (
	"io"
	"sort"
	"time"
)

// Command classes.
const (
	ClassWorkflow = "workflow"
	ClassView     = "view"
	ClassProcess  = "process"
)

type runFunc func(cmd string, args []string, stdin io.Reader, stdout, stderr io.Writer, now func() time.Time) int

type command struct {
	name  string
	class string
	run   runFunc
}

// plain adapts the common signature.
func plain(f func(args []string, stdout, stderr io.Writer) int) runFunc {
	return func(_ string, args []string, _ io.Reader, stdout, stderr io.Writer, _ func() time.Time) int {
		return f(args, stdout, stderr)
	}
}

func withNow(f func(args []string, stdout, stderr io.Writer, now func() time.Time) int) runFunc {
	return func(_ string, args []string, _ io.Reader, stdout, stderr io.Writer, now func() time.Time) int {
		return f(args, stdout, stderr, now)
	}
}

var commands = []command{
	{"daemon", ClassProcess, func(_ string, a []string, in io.Reader, o, e io.Writer, _ func() time.Time) int {
		return cmdDaemon(a, in, o, e)
	}},
	{"stop", ClassProcess, plain(cmdStop)},
	{"open", ClassProcess, plain(cmdOpen)},
	{"status", ClassView, plain(cmdStatus)},
	{"pause", ClassWorkflow, plain(func(a []string, o, e io.Writer) int { return cmdPause(true, a, o, e) })},
	{"resume", ClassWorkflow, plain(func(a []string, o, e io.Writer) int { return cmdPause(false, a, o, e) })},
	{"settings", ClassWorkflow, plain(cmdSettings)},
	{"update", ClassWorkflow, plain(cmdUpdate)},
	{"autostart", ClassWorkflow, plain(cmdAutostart)},
	{"path", ClassProcess, plain(cmdPath)},
	{"setup", ClassWorkflow, plain(cmdSetup)},
	{"check", ClassView, plain(cmdCheck)},
	{"invariants", ClassView, plain(cmdInvariants)},
	{"doctor", ClassView, plain(cmdDoctor)},
	{"plugins", ClassView, plain(cmdPlugins)},
	{"plugin", ClassWorkflow, func(_ string, a []string, in io.Reader, o, e io.Writer, _ func() time.Time) int {
		return cmdPlugin(a, in, o, e)
	}},
	{"samples", ClassWorkflow, plain(cmdSamples)},
	{"comment", ClassWorkflow, func(_ string, a []string, in io.Reader, o, e io.Writer, now func() time.Time) int {
		return cmdComment(a, in, o, e, now)
	}},
	{"version", ClassView, plain(cmdVersion)},
	{"reference", ClassWorkflow, plain(cmdReference)},
	{"seed", ClassWorkflow, withNow(cmdSeed)},
	{"seeds", ClassView, withNow(cmdSeeds)},
	{"promote", ClassWorkflow, plain(cmdPromote)},
	{"approve", ClassWorkflow, plain(cmdApprove)},
	{"context", ClassView, plain(cmdContext)},
	{"graph", ClassView, plain(cmdGraph)},
	{"teams", ClassView, plain(cmdTeams)},
	{"team", ClassWorkflow, plain(cmdTeam)},
	{"join", ClassWorkflow, plain(cmdJoin)},
	{"waiting", ClassView, plain(cmdWaiting)},
	{"publish", ClassWorkflow, plain(cmdPublish)},
	{"release", ClassWorkflow, plain(cmdRelease)},
	{"handoff", ClassWorkflow, plain(cmdHandoff)},
	{"reassign", ClassWorkflow, plain(cmdReassign)},
	{"copy", ClassWorkflow, func(c string, a []string, _ io.Reader, o, e io.Writer, _ func() time.Time) int {
		return cmdTransferStart(c, a, o, e)
	}},
	{"move", ClassWorkflow, func(c string, a []string, _ io.Reader, o, e io.Writer, _ func() time.Time) int {
		return cmdTransferStart(c, a, o, e)
	}},
	{"transfers", ClassView, plain(cmdTransfers)},
	{"transfer", ClassWorkflow, plain(cmdTransfer)},
	{"domains", ClassView, plain(cmdDomains)},
	{"delete", ClassWorkflow, plain(cmdDelete)},
	{"export", ClassWorkflow, plain(cmdExport)},
	{"archive", ClassWorkflow, plain(cmdArchive)},
}

func commandByName(n string) (command, bool) {
	for _, c := range commands {
		if c.name == n {
			return c, true
		}
	}
	return command{}, false
}

// Commands maps every command to its class.
func Commands() map[string]string {
	out := make(map[string]string, len(commands))
	for _, c := range commands {
		out[c.name] = c.class
	}
	return out
}

// commandNames is the table's names in order, for tests.
func commandNames() []string {
	var out []string
	for _, c := range commands {
		out = append(out, c.name)
	}
	sort.Strings(out)
	return out
}
