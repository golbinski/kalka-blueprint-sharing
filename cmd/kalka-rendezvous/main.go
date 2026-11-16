// Command kalka-rendezvous is the rendezvous server a team runs on its
// own host (spec 9.6): PeerJS-compatible signaling, the /kalka/ice
// endpoint and an optional TURN relay. It needs no config directory,
// identity or folder, and imports no part of the kalka client.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/golbinski/kalka/internal/name"
	"github.com/golbinski/kalka/internal/rendezvous"
)

// SecretEnv holds the TURN secret, so it is not on the command line.
const secretEnv = "KALKA_TURN_SECRET"

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(s string) error { *l = append(*l, s); return nil }

// Exit codes, as kalka's.
const (
	exitOK       = 0
	exitProblems = 1
	exitUsage    = 2
)

// version is set at release build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, nil)
	stop()
	os.Exit(code)
}

// run is the program with a context and a hook that gets the running
// server (tests).
func run(ctx context.Context, args []string, stdout, stderr io.Writer, started func(*rendezvous.Running)) int {
	if len(args) == 1 && (args[0] == "version" || args[0] == "--version") {
		fmt.Fprintf(stdout, "%s %s\n", name.RendezvousBinary, version)
		return exitOK
	}
	fs := flag.NewFlagSet(name.RendezvousBinary, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o rendezvous.Options
	var origins, stun listFlag
	fs.StringVar(&o.Listen, "listen", ":9000", "HTTP(S) address for signaling and /kalka/ice")
	fs.StringVar(&o.Path, "path", "/", "mount path for signaling, when a proxy serves it under a prefix")
	fs.StringVar(&o.Key, "key", "peerjs", "PeerJS key clients must send")
	fs.Var(&origins, "allow-origin", "extra browser Origin host pattern to accept (repeatable, e.g. *.corp.com)")
	cert := fs.String("tls-cert", "", "TLS certificate (PEM); serves HTTPS and TURN over TLS")
	key := fs.String("tls-key", "", "TLS private key (PEM)")
	fs.BoolVar(&o.PlainHTTP, "plain-http", false, "serve signaling over plain HTTP even with --tls-cert (TLS ends at a proxy; the certificate is for TURN over TLS)")
	turnIP := fs.String("turn-ip", "", "public IP of this host; turns on the TURN relay")
	fs.StringVar(&o.TURNHost, "turn-host", "", "host name in TURN URLs (default --turn-ip; use the certificate's name with TLS)")
	fs.IntVar(&o.TURNPort, "turn-port", 3478, "TURN port, UDP and TCP")
	fs.IntVar(&o.TURNTLSPort, "turn-tls-port", 5349, "TURN over TLS port when --tls-cert is set (443 passes most firewalls); 0 turns it off")
	fs.StringVar(&o.RelayBind, "relay-bind", "0.0.0.0", "address relay sockets are opened on")
	relayPorts := fs.String("relay-ports", "49152-65535", "UDP port range for relayed traffic, for firewall rules; \"os\" leaves it to the OS")
	secretFile := fs.String("turn-secret-file", "", "file with the TURN secret (or set "+secretEnv+"); default: random per run")
	fs.DurationVar(&o.TTL, "ice-ttl", 12*time.Hour, "lifetime of TURN credentials")
	fs.Var(&stun, "stun", "STUN URL to hand out (repeatable; default: this host's TURN port)")
	asJSON := fs.Bool("json", false, "print the startup summary as JSON")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		if err == nil {
			fmt.Fprintf(stderr, "%s: unexpected argument %q\n", name.RendezvousBinary, fs.Arg(0))
		}
		return exitUsage
	}
	o.AllowOrigins = origins
	if len(stun) > 0 {
		o.STUN = stun
	}
	bad := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "%s: "+format+"\n", append([]any{name.RendezvousBinary}, a...)...)
		return exitUsage
	}
	if (*cert == "") != (*key == "") {
		return bad("--tls-cert and --tls-key go together")
	}
	if *cert != "" {
		c, err := tls.LoadX509KeyPair(*cert, *key)
		if err != nil {
			return bad("%v", err)
		}
		o.TLS = &c
	}
	if *turnIP != "" {
		ip := net.ParseIP(*turnIP)
		if ip == nil || ip.IsUnspecified() {
			return bad("--turn-ip %q is not an IP address peers can reach", *turnIP)
		}
		o.TURNIP = ip
		o.Secret = os.Getenv(secretEnv)
		if *secretFile != "" {
			b, err := os.ReadFile(*secretFile)
			if err != nil {
				return bad("%v", err)
			}
			o.Secret = strings.TrimSpace(string(b))
		}
		if *relayPorts != "os" {
			var lo, hi uint16
			if n, _ := fmt.Sscanf(*relayPorts, "%d-%d", &lo, &hi); n != 2 || lo == 0 || hi < lo {
				return bad("--relay-ports %q: want min-max, e.g. 49152-65535", *relayPorts)
			}
			o.RelayMin, o.RelayMax = lo, hi
		}
		if o.Secret != "" && len(o.Secret) < 16 {
			return bad("the TURN secret is shorter than 16 characters")
		}
	} else if o.TURNHost != "" || *secretFile != "" {
		return bad("TURN flags need --turn-ip")
	}

	r, err := rendezvous.Start(o)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name.RendezvousBinary, err)
		return exitProblems
	}
	if *asJSON {
		_ = json.NewEncoder(stdout).Encode(map[string]any{"listen": r.Addr, "url": r.URL, "ice": r.ICE})
	} else {
		fmt.Fprintf(stdout, "%s listening on %s\n", name.RendezvousBinary, r.Addr)
		fmt.Fprintf(stdout, "clients set   rendezvous: %s   (use the public host name)\n", r.URL)
		if len(r.ICE) > 0 {
			fmt.Fprintf(stdout, "TURN relay    %s\n", strings.Join(r.ICE, "  "))
			if o.Secret == "" {
				fmt.Fprintf(stdout, "              random secret: credentials end with this run; set %s to keep them across restarts\n", secretEnv)
			}
		} else {
			fmt.Fprintln(stdout, "TURN relay    off (--turn-ip turns it on); clients use STUN only")
		}
	}
	if started != nil {
		started(r)
	}
	if err := r.Wait(ctx); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name.RendezvousBinary, err)
		return exitProblems
	}
	return exitOK
}
