package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/pion/logging"
	"github.com/pion/turn/v5"

	"github.com/golbinski/kalka/internal/rendezvous"
)

func TestRendezvousFlags(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--tls-cert", "x.pem"}, "go together"},
		{[]string{"--turn-ip", "0.0.0.0"}, "not an IP address peers can reach"},
		{[]string{"--turn-ip", "nope"}, "not an IP address"},
		{[]string{"--turn-host", "turn.corp.com"}, "need --turn-ip"},
		{[]string{"extra"}, "unexpected argument"},
		{[]string{"--turn-ip", "127.0.0.1", "--relay-ports", "9-1"}, "want min-max"},
	} {
		var out, errb bytes.Buffer
		if code := run(context.Background(), c.args, &out, &errb, nil); code != exitUsage || !strings.Contains(errb.String(), c.want) {
			t.Errorf("%v: exit %d, %q", c.args, code, errb.String())
		}
	}
	t.Setenv(secretEnv, "short")
	var errb bytes.Buffer
	if code := run(context.Background(), []string{"--turn-ip", "127.0.0.1"}, io.Discard, &errb, nil); code != exitUsage || !strings.Contains(errb.String(), "shorter than 16") {
		t.Errorf("short secret: %d %q", code, errb.String())
	}
}

// HTTPS signaling and TURN over TLS with one certificate: the setup for
// networks that only let TLS on 443 out.
func TestRendezvousTLS(t *testing.T) {
	dir := t.TempDir()
	pool := writeCert(t, dir)
	turnPort, tlsPort := freePort(t), freePort(t)
	t.Setenv(secretEnv, "0123456789abcdef0123")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan *rendezvous.Running, 1)
	var out bytes.Buffer
	exit := make(chan int, 1)
	go func() {
		exit <- run(ctx, []string{
			"--listen", "127.0.0.1:0", "--json",
			"--tls-cert", filepath.Join(dir, "cert.pem"), "--tls-key", filepath.Join(dir, "key.pem"),
			"--turn-ip", "127.0.0.1", "--turn-host", "localhost", "--relay-bind", "127.0.0.1", "--relay-ports", "40000-40100",
			"--turn-port", strconv.Itoa(turnPort), "--turn-tls-port", strconv.Itoa(tlsPort),
		}, &out, io.Discard, func(r *rendezvous.Running) { ready <- r })
	}()
	var r *rendezvous.Running
	select {
	case r = <-ready:
	case code := <-exit:
		t.Fatalf("exited %d", code)
	case <-time.After(5 * time.Second):
		t.Fatal("did not start")
	}
	var sum struct {
		URL string
		ICE []string
	}
	if err := json.Unmarshal(out.Bytes(), &sum); err != nil || !strings.HasPrefix(sum.URL, "https://") || len(sum.ICE) != 3 ||
		sum.ICE[2] != "turns:localhost:"+strconv.Itoa(tlsPort)+"?transport=tcp" {
		t.Fatalf("summary %s %v", out.String(), err)
	}

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	origin := "https://" + r.Addr
	for path, want := range map[string]string{"/healthz": "ok", "/": "PeerJS Server", "/peerjs/id": ""} {
		resp, err := client.Get(origin + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(body), want) {
			t.Fatalf("%s: %d %q", path, resp.StatusCode, body)
		}
	}
	// Signaling over wss.
	ws, _, err := websocket.Dial(ctx, "wss://"+r.Addr+"/peerjs?key=peerjs&id=abc&token=t1", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	_, msg, err := ws.Read(ctx)
	if err != nil || !strings.Contains(string(msg), `"OPEN"`) {
		t.Fatalf("wss: %s %v", msg, err)
	}
	ws.CloseNow()

	// A browser from another site is refused unless allowed.
	hdr := http.Header{"Origin": {"https://evil.example"}}
	if _, _, err := websocket.Dial(ctx, "wss://"+r.Addr+"/peerjs?key=peerjs&id=b&token=t", &websocket.DialOptions{HTTPClient: client, HTTPHeader: hdr}); err == nil {
		t.Fatal("foreign origin accepted")
	}

	// TURN over TLS with credentials from /kalka/ice.
	resp, err := client.Get(origin + "/kalka/ice")
	if err != nil {
		t.Fatal(err)
	}
	var ice rendezvous.ICEResponse
	_ = json.NewDecoder(resp.Body).Decode(&ice)
	resp.Body.Close()
	creds := ice.ICEServers[len(ice.ICEServers)-1]
	if ice.TTL != 12*3600 || creds.Username == "" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("ice %+v", ice)
	}
	conn, err := tls.Dial("tcp", "127.0.0.1:"+strconv.Itoa(tlsPort), &tls.Config{RootCAs: pool, ServerName: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	tc, err := turn.NewClient(&turn.ClientConfig{
		TURNServerAddr: "127.0.0.1:" + strconv.Itoa(tlsPort), Conn: turn.NewSTUNConn(conn),
		Username: creds.Username, Password: creds.Credential, Realm: rendezvous.TURNRealm,
		LoggerFactory: logging.NewDefaultLoggerFactory(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tc.Close()
	if err := tc.Listen(); err != nil {
		t.Fatal(err)
	}
	relay, err := tc.Allocate()
	if err != nil {
		t.Fatalf("allocate over TLS: %v", err)
	}
	if p := relay.LocalAddr().(*net.UDPAddr).Port; p < 40000 || p > 40100 {
		t.Fatalf("relay port %d outside --relay-ports", p)
	}
	relay.Close()
	// A wrong password is refused.
	conn2, err := tls.Dial("tcp", "127.0.0.1:"+strconv.Itoa(tlsPort), &tls.Config{RootCAs: pool, ServerName: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	tc2, err := turn.NewClient(&turn.ClientConfig{TURNServerAddr: "127.0.0.1:" + strconv.Itoa(tlsPort), Conn: turn.NewSTUNConn(conn2), Username: creds.Username, Password: "wrong", Realm: rendezvous.TURNRealm, LoggerFactory: logging.NewDefaultLoggerFactory()})
	if err != nil {
		t.Fatal(err)
	}
	defer tc2.Close()
	_ = tc2.Listen()
	if _, err := tc2.Allocate(); err == nil {
		t.Fatal("allocated with a wrong password")
	}

	cancel()
	select {
	case code := <-exit:
		if code != exitOK {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("did not stop")
	}
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// writeCert writes a self-signed certificate for localhost and returns a
// pool that trusts it.
func writeCert(t *testing.T, dir string) *x509.CertPool {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kder, _ := x509.MarshalECPrivateKey(key)
	_ = os.WriteFile(filepath.Join(dir, "cert.pem"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "key.pem"), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder}), 0o600)
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return pool
}
