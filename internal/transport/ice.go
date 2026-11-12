package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/golbinski/kalka/internal/peerjs"
)

// DefaultSTUN is the built-in STUN server (spec 9.4, step 3).
var DefaultSTUN = webrtc.ICEServer{URLs: []string{"stun:stun.l.google.com:19302"}}

// peerJSCloudICE are the ICE defaults shipped by the PeerJS client library
// (peerjs 1.5.5, util defaultConfig), used with rendezvous: peerjs-cloud.
var peerJSCloudICE = []webrtc.ICEServer{
	DefaultSTUN,
	{URLs: []string{"turn:eu-0.turn.peerjs.com:3478", "turn:us-0.turn.peerjs.com:3478"}, Username: "peerjs", Credential: "peerjsp"},
}

// ICESource says where the ICE configuration came from.
type ICESource string

const (
	ICEFromConfig     ICESource = "config"
	ICEFromRendezvous ICESource = "rendezvous"
	ICEDefault        ICESource = "default"
)

// ResolveICE applies spec 9.4: configured servers, else GET
// <rendezvous-http-origin>/kalka/ice, else built-in defaults. A 404 or any
// other failure of the endpoint falls through to the defaults.
func ResolveICE(ctx context.Context, configured []webrtc.ICEServer, srv peerjs.Server, cloud bool) ([]webrtc.ICEServer, ICESource, error) {
	if len(configured) > 0 {
		return configured, ICEFromConfig, nil
	}
	if !cloud {
		if s, err := fetchICE(ctx, srv.HTTPOrigin()+"/kalka/ice"); err == nil && len(s) > 0 {
			return s, ICEFromRendezvous, nil
		}
	}
	if cloud {
		return peerJSCloudICE, ICEDefault, nil
	}
	return []webrtc.ICEServer{DefaultSTUN}, ICEDefault, nil
}

func fetchICE(ctx context.Context, url string) ([]webrtc.ICEServer, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	var body struct {
		ICEServers []struct {
			URLs       any    `json:"urls"`
			Username   string `json:"username"`
			Credential string `json:"credential"`
		} `json:"iceServers"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body); err != nil {
		return nil, err
	}
	var out []webrtc.ICEServer
	for _, s := range body.ICEServers {
		var urls []string
		switch u := s.URLs.(type) {
		case string:
			urls = []string{u}
		case []any:
			for _, x := range u {
				if str, ok := x.(string); ok {
					urls = append(urls, str)
				}
			}
		}
		if len(urls) > 0 {
			out = append(out, webrtc.ICEServer{URLs: urls, Username: s.Username, Credential: s.Credential})
		}
	}
	return out, nil
}
