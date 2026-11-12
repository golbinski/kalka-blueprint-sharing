// Package peerjs speaks the PeerJS server WebSocket protocol (spec 9.1), so
// one client works against the public PeerJS cloud, stock peerjs-server and
// `kalka rendezvous`. Only signaling is PeerJS; the data channel carries
// kalka's own protocol.
//
// Protocol facts below were taken from the peerjs-server 1.0.2 source:
//   - WebSocket at <path>peerjs?key=<key>&id=<id>&token=<token>
//   - the server answers OPEN, or ID-TAKEN when the id is held with another
//     token, or ERROR for bad parameters, and closes;
//   - client messages are {type, dst, payload}; the server sets src;
//   - messages to an absent id are queued; after expire_timeout (default
//     5 s, swept every 1 s) the sender gets EXPIRE with src = the absent id;
//   - clients send HEARTBEAT; the server drops sockets silent for
//     alive_timeout (default 90 s).
package peerjs

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Message types.
const (
	Open      = "OPEN"
	Leave     = "LEAVE"
	Candidate = "CANDIDATE"
	Offer     = "OFFER"
	Answer    = "ANSWER"
	Expire    = "EXPIRE"
	Heartbeat = "HEARTBEAT"
	IDTaken   = "ID-TAKEN"
	Error     = "ERROR"
)

// Message is one signaling message.
type Message struct {
	Type    string          `json:"type"`
	Src     string          `json:"src,omitempty"`
	Dst     string          `json:"dst,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Server describes how to reach a PeerJS server.
type Server struct {
	// URL is the WebSocket endpoint without query, e.g.
	// wss://0.peerjs.com:443/peerjs
	URL string
	Key string
}

// CloudHost is the public PeerJS cloud.
const CloudHost = "0.peerjs.com"

// ParseRendezvous turns the config value (spec 5) into a Server. It accepts
// the literal "peerjs-cloud" or a ws://, wss://, http:// or https:// URL
// whose path is the server's mount path; "/peerjs" is appended.
func ParseRendezvous(s string) (Server, error) {
	if s == "peerjs-cloud" {
		return Server{URL: "wss://" + CloudHost + ":443/peerjs", Key: "peerjs"}, nil
	}
	u, err := url.Parse(s)
	if err != nil {
		return Server{}, fmt.Errorf("rendezvous %q: %w", s, err)
	}
	switch u.Scheme {
	case "ws", "wss":
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return Server{}, fmt.Errorf("rendezvous %q: want ws://, wss:// or peerjs-cloud", s)
	}
	key := u.Query().Get("key")
	if key == "" {
		key = "peerjs"
	}
	u.RawQuery = ""
	u.Path = strings.TrimSuffix(u.Path, "/") + "/peerjs"
	return Server{URL: u.String(), Key: key}, nil
}

// HTTPOrigin returns the http(s) origin of the server, for /kalka/ice.
func (s Server) HTTPOrigin() string {
	u, err := url.Parse(s.URL)
	if err != nil {
		return ""
	}
	scheme := "http"
	if u.Scheme == "wss" {
		scheme = "https"
	}
	return scheme + "://" + u.Host
}

// idRe is the id rule of the PeerJS client library.
var idRe = regexp.MustCompile(`^[A-Za-z0-9]+(?:[ _-][A-Za-z0-9]+)*$`)

// ValidID reports whether id is acceptable to PeerJS servers and clients.
func ValidID(id string) bool { return idRe.MatchString(id) }
