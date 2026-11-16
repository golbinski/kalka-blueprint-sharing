package rendezvous

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/pion/logging"
	"github.com/pion/turn/v5"
)

// TURNRealm is the realm of the embedded TURN server.
const TURNRealm = "kalka"

// TURNConfig configures the embedded relay (spec 9.6).
type TURNConfig struct {
	// Secret signs time-limited credentials (TURN REST API scheme).
	Secret string
	// RelayIP is the address peers reach relayed traffic at.
	RelayIP net.IP
	// Listen address for relay sockets, e.g. "0.0.0.0".
	RelayBind string
	// MinPort..MaxPort bound the relay ports, for firewall rules; zero
	// leaves them to the OS.
	MinPort, MaxPort uint16
	// UDP, TCP and TLS listeners for clients; any may be nil.
	UDP net.PacketConn
	TCP net.Listener
	TLS net.Listener
}

// StartTURN runs a TURN server with time-limited credentials.
func StartTURN(c TURNConfig) (*turn.Server, error) {
	gen := func() turn.RelayAddressGenerator {
		if c.MinPort != 0 {
			return &turn.RelayAddressGeneratorPortRange{RelayAddress: c.RelayIP, Address: c.RelayBind, MinPort: c.MinPort, MaxPort: c.MaxPort}
		}
		return &turn.RelayAddressGeneratorStatic{RelayAddress: c.RelayIP, Address: c.RelayBind}
	}
	logs := logging.NewDefaultLoggerFactory()
	cfg := turn.ServerConfig{
		Realm:         TURNRealm,
		LoggerFactory: logs,
		AuthHandler:   turn.LongTermTURNRESTAuthHandler(c.Secret, logs.NewLogger("turn")),
	}
	if c.UDP != nil {
		cfg.PacketConnConfigs = append(cfg.PacketConnConfigs, turn.PacketConnConfig{PacketConn: c.UDP, RelayAddressGenerator: gen()})
	}
	for _, l := range []net.Listener{c.TCP, c.TLS} {
		if l != nil {
			cfg.ListenerConfigs = append(cfg.ListenerConfigs, turn.ListenerConfig{Listener: l, RelayAddressGenerator: gen()})
		}
	}
	return turn.NewServer(cfg)
}

// TLSListener wraps a TCP listener for TURN over TLS (port 443, for
// networks whose firewalls let only HTTPS out).
func TLSListener(l net.Listener, cert tls.Certificate) net.Listener {
	return tls.NewListener(l, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
}

// ICEServer is one entry of the /kalka/ice response, in the shape of the
// WebRTC RTCIceServer dictionary.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// ICEResponse is served at GET /kalka/ice (spec 9.4).
type ICEResponse struct {
	ICEServers []ICEServer `json:"iceServers"`
	TTL        int         `json:"ttl"`
}

// Credentials returns ICE servers with credentials valid for ttl.
func Credentials(secret string, turnURLs, stunURLs []string, ttl time.Duration) (ICEResponse, error) {
	user, pass, err := turn.GenerateLongTermTURNRESTCredentials(secret, "kalka", ttl)
	if err != nil {
		return ICEResponse{}, err
	}
	r := ICEResponse{TTL: int(ttl.Seconds())}
	if len(stunURLs) > 0 {
		r.ICEServers = append(r.ICEServers, ICEServer{URLs: stunURLs})
	}
	if len(turnURLs) > 0 {
		r.ICEServers = append(r.ICEServers, ICEServer{URLs: turnURLs, Username: user, Credential: pass})
	}
	return r, nil
}

// ICEHandler serves /kalka/ice.
func ICEHandler(secret string, turnURLs, stunURLs []string, ttl time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}
		resp, err := Credentials(secret, turnURLs, stunURLs, ttl)
		if err != nil {
			http.Error(w, fmt.Sprint(err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(resp)
	})
}
