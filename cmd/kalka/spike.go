package main

// `kalka spike` is the M1 transport spike: two processes sync one file
// through a rendezvous server with ephemeral identity keys. It is replaced
// by the daemon and `kalka send` in M2.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"

	bp "github.com/golbinski/kalka/internal/blueprint"
	"github.com/golbinski/kalka/internal/fsutil"
	"github.com/golbinski/kalka/internal/peerjs"
	"github.com/golbinski/kalka/internal/syncproto"
	"github.com/golbinski/kalka/internal/transport"
)

func cmdSpike(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "listen" && args[0] != "send") {
		fmt.Fprintln(stderr, "usage: kalka spike listen --email <e> --dir <d> [--rendezvous <r>]\n       kalka spike send --email <e> [--rendezvous <r>] <file> <peer-email>")
		return exitUsage
	}
	mode := args[0]
	fs := flag.NewFlagSet("spike "+mode, flag.ContinueOnError)
	fs.SetOutput(stderr)
	email := fs.String("email", "", "your email")
	rdv := fs.String("rendezvous", "peerjs-cloud", "rendezvous server URL or peerjs-cloud")
	dir := fs.String("dir", ".", "folder for received blueprints (listen)")
	team := fs.String("team", "", "team string mixed into peer ids")
	relay := fs.Bool("relay-only", false, "use TURN relay candidates only")
	turnURL := fs.String("turn", "", "TURN URL to use instead of discovery, e.g. turn:host:443?transport=tcp")
	turnUser := fs.String("turn-user", "", "TURN username")
	turnPass := fs.String("turn-pass", "", "TURN password")
	timeout := fs.Duration("timeout", 2*time.Minute, "give up after this long")
	pos, err := parseInterspersed(fs, args[1:])
	if err != nil {
		return exitUsage
	}
	if !bp.ValidEmail(*email) {
		fmt.Fprintln(stderr, "spike: --email is required")
		return exitUsage
	}
	srv, err := peerjs.ParseRendezvous(*rdv)
	if err != nil {
		fmt.Fprintln(stderr, "spike:", err)
		return exitUsage
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	var configured []webrtc.ICEServer
	if *turnURL != "" {
		configured = []webrtc.ICEServer{{URLs: []string{*turnURL}, Username: *turnUser, Credential: *turnPass}}
	}
	ice, src, _ := transport.ResolveICE(ctx, configured, srv, *rdv == "peerjs-cloud")
	logf := func(format string, a ...any) {
		fmt.Fprintf(stderr, "%s %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, a...))
	}
	logf("ICE servers from %s: %d", src, len(ice))

	start := time.Now()
	sig, err := peerjs.Dial(ctx, srv, transport.PeerID(*team, *email))
	if err != nil {
		fmt.Fprintln(stderr, "spike:", err)
		return exitProblems
	}
	defer sig.Close()
	logf("registered as %s at %s (%v)", sig.ID(), srv.URL, time.Since(start).Round(time.Millisecond))
	ep := transport.NewEndpoint(sig, transport.Options{ICEServers: ice, RelayOnly: *relay})
	defer ep.Close()

	_, priv, _ := ed25519.GenerateKey(nil)
	self := transport.Identity{Email: *email, Key: priv}
	pins := map[string]ed25519.PublicKey{strings.ToLower(*email): priv.Public().(ed25519.PublicKey)}
	verify := func(e string, k ed25519.PublicKey) error {
		pins[strings.ToLower(e)] = k // spike: trust on first use, not persisted
		return nil
	}
	keys := func(e string) (ed25519.PublicKey, bool) { k, ok := pins[strings.ToLower(e)]; return k, ok }

	if mode == "listen" {
		logf("waiting for a sync")
		conn, err := ep.Accept(ctx)
		if err != nil {
			fmt.Fprintln(stderr, "spike:", err)
			return exitProblems
		}
		defer conn.Close()
		peer, err := transport.Handshake(conn, self, *team, verify)
		if err != nil {
			fmt.Fprintln(stderr, "spike: handshake:", err)
			return exitProblems
		}
		logf("authenticated %s; %s", peer.Email, pairInfo(conn))
		res, err := syncproto.Respond(conn, &dirStore{dir: *dir, log: logf}, peer.Email, keys)
		if err != nil {
			fmt.Fprintln(stderr, "spike: sync:", err)
			return exitProblems
		}
		logf("sync done: %v", res.Outcome)
		return exitOK
	}

	if len(pos) != 2 {
		fmt.Fprintln(stderr, "spike send: give <file> and <peer-email>")
		return exitUsage
	}
	path, to := pos[0], pos[1]
	raw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(stderr, "spike:", err)
		return exitProblems
	}
	local, err := bp.Parse(raw)
	if err != nil {
		fmt.Fprintln(stderr, "spike:", err)
		return exitProblems
	}
	if local.KalkaID() == "" {
		local.SetFrontmatter("kalka_id", newUUID())
		if local.Owner() == "" {
			local.SetFrontmatter("owner", *email)
		}
		if err := fsutil.WriteAtomic(path, local.Bytes(), raw); err != nil {
			fmt.Fprintln(stderr, "spike:", err)
			return exitProblems
		}
		raw = local.Bytes()
		local, _ = bp.Parse(raw)
		logf("first send: added kalka_id %s", local.KalkaID())
	}
	t0 := time.Now()
	conn, err := ep.Dial(ctx, transport.PeerID(*team, to))
	if err != nil {
		fmt.Fprintf(stderr, "spike: %s: %v (after %v)\n", to, err, time.Since(t0).Round(time.Millisecond))
		return exitProblems
	}
	defer conn.Close()
	logf("data channel open after %v; %s", time.Since(t0).Round(time.Millisecond), pairInfo(conn))
	peer, err := transport.Handshake(conn, self, *team, verify)
	if err != nil {
		fmt.Fprintln(stderr, "spike: handshake:", err)
		return exitProblems
	}
	res, err := syncproto.Initiate(conn, local, filepath.Base(path), peer.Email, keys, func(d *bp.Document) error {
		return fsutil.WriteAtomic(path, d.Bytes(), raw)
	})
	if err != nil {
		fmt.Fprintln(stderr, "spike: sync:", err)
		return exitProblems
	}
	logf("synced with %s in %v: %v", peer.Email, time.Since(t0).Round(time.Millisecond), res.Outcome)
	fmt.Fprintln(stdout, "ok")
	return exitOK
}

func pairInfo(c *transport.Conn) string {
	p, err := c.SelectedPair()
	if err != nil || p == nil {
		return "candidate pair unknown"
	}
	return fmt.Sprintf("path %s/%s -> %s/%s", p.Local.Typ, p.Local.Protocol, p.Remote.Typ, p.Remote.Protocol)
}

func newUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// dirStore is a minimal folder store for the spike.
type dirStore struct {
	dir   string
	log   func(string, ...any)
	found map[string]string
}

func (s *dirStore) Find(id string) (*bp.Document, error) {
	paths, _ := filepath.Glob(filepath.Join(s.dir, "*.md"))
	s.found = map[string]string{}
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		d, err := bp.Parse(raw)
		if err == nil && d.KalkaID() == id {
			s.found[id] = p
			return d, nil
		}
	}
	return nil, nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (s *dirStore) Write(d *bp.Document, filename string, isNew bool) error {
	path := s.found[d.KalkaID()]
	if isNew || path == "" {
		base := strings.TrimSuffix(unsafeName.ReplaceAllString(filepath.Base(filename), "-"), ".md")
		if base == "" || base == "." {
			base = "blueprint"
		}
		path = filepath.Join(s.dir, base+".md")
		for i := 1; ; i++ {
			if _, err := os.Stat(path); os.IsNotExist(err) {
				break
			}
			path = filepath.Join(s.dir, fmt.Sprintf("%s-%d.md", base, i))
		}
	}
	s.log("writing %s", path)
	return fsutil.WriteAtomic(path, d.Bytes(), nil)
}

func (s *dirStore) AcceptNew(from string, d *bp.Document) bool {
	s.log("accepting new blueprint %q from %s (spike: no prompt)", d.Title(), from)
	return true
}
