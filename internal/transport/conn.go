// Package transport carries kalka's protocol over a WebRTC data channel
// (spec 8.3, 9.4, 9.5): PeerJS signaling to set up the connection, framed
// JSON messages on one reliable ordered channel, and channel binding that
// ties the DTLS session to the peers' identity keys.
package transport

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/pion/datachannel"
	"github.com/pion/webrtc/v4"
)

// Label is the data channel label: "length-prefixed JSON that starts
// with hello". It stays the same for every protocol version; versions are
// negotiated in hello (spec 9.5).
const Label = "kalka/1"

// ChunkSize is the largest single data channel message kalka writes.
const ChunkSize = 16 << 10

// MaxFrame bounds one protocol message: a 2 MiB blueprint JSON-encoded,
// with room for escaping.
const MaxFrame = 16 << 20

// Conn is an open data channel to one peer.
type Conn struct {
	pc     *webrtc.PeerConnection
	rw     datachannel.ReadWriteCloserDeadliner
	remote string // peer id

	localFP, remoteFP [sha256.Size]byte

	rmu  sync.Mutex
	rbuf []byte
	wmu  sync.Mutex

	closeOnce sync.Once
	onClose   func()
}

func newConn(pc *webrtc.PeerConnection, rw datachannel.ReadWriteCloserDeadliner, remote string, onClose func()) (*Conn, error) {
	c := &Conn{pc: pc, rw: rw, remote: remote, onClose: onClose}
	dtls := pc.SCTP().Transport()
	params, err := dtls.GetLocalParameters()
	if err != nil {
		return nil, err
	}
	local, err := fingerprintFromParams(params)
	if err != nil {
		return nil, err
	}
	cert := dtls.GetRemoteCertificate()
	if len(cert) == 0 {
		return nil, errors.New("no remote DTLS certificate")
	}
	c.localFP, c.remoteFP = local, sha256.Sum256(cert)
	return c, nil
}

func fingerprintFromParams(p webrtc.DTLSParameters) ([sha256.Size]byte, error) {
	var out [sha256.Size]byte
	for _, f := range p.Fingerprints {
		if f.Algorithm != "sha-256" {
			continue
		}
		var b []byte
		for i := 0; i+1 < len(f.Value); i += 3 {
			var v byte
			if _, err := fmt.Sscanf(f.Value[i:i+2], "%02X", &v); err != nil {
				return out, err
			}
			b = append(b, v)
		}
		if len(b) == sha256.Size {
			copy(out[:], b)
			return out, nil
		}
	}
	return out, errors.New("no sha-256 fingerprint for the local certificate")
}

// RemoteID is the PeerJS id of the other side: the id dialed, or the src of
// the offer that opened this connection.
func (c *Conn) RemoteID() string { return c.remote }

// Fingerprints returns SHA-256 fingerprints of the local and remote DTLS
// certificates, read from the DTLS transport (not from the SDP).
func (c *Conn) Fingerprints() (local, remote [sha256.Size]byte) { return c.localFP, c.remoteFP }

// SelectedPair reports the ICE candidate pair in use (host, srflx, relay).
func (c *Conn) SelectedPair() (*webrtc.ICECandidatePair, error) {
	return c.pc.SCTP().Transport().ICETransport().GetSelectedCandidatePair()
}

// Send writes one message as a 4-byte big-endian length and its JSON, in
// chunks of at most ChunkSize bytes.
func (c *Conn) Send(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(payload) > MaxFrame {
		return fmt.Errorf("message of %d bytes exceeds %d", len(payload), MaxFrame)
	}
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame, uint32(len(payload)))
	copy(frame[4:], payload)
	c.wmu.Lock()
	defer c.wmu.Unlock()
	for len(frame) > 0 {
		n := min(len(frame), ChunkSize)
		if _, err := c.rw.Write(frame[:n]); err != nil {
			return err
		}
		frame = frame[n:]
	}
	return nil
}

// Envelope is a received message: its type and raw JSON.
type Envelope struct {
	Type string
	Raw  json.RawMessage
}

// Decode unmarshals the message into v.
func (e Envelope) Decode(v any) error { return json.Unmarshal(e.Raw, v) }

// Receive reads the next message. A zero timeout waits indefinitely.
func (c *Conn) Receive(timeout time.Duration) (Envelope, error) {
	c.rmu.Lock()
	defer c.rmu.Unlock()
	if timeout > 0 {
		_ = c.rw.SetReadDeadline(time.Now().Add(timeout))
	} else {
		_ = c.rw.SetReadDeadline(time.Time{})
	}
	buf := make([]byte, 64<<10)
	for {
		if len(c.rbuf) >= 4 {
			n := binary.BigEndian.Uint32(c.rbuf)
			if n > MaxFrame {
				return Envelope{}, fmt.Errorf("peer sent a %d-byte message, limit %d", n, MaxFrame)
			}
			if len(c.rbuf) >= 4+int(n) {
				raw := append(json.RawMessage(nil), c.rbuf[4:4+n]...)
				c.rbuf = c.rbuf[4+n:]
				var head struct {
					Type string `json:"type"`
				}
				if err := json.Unmarshal(raw, &head); err != nil {
					return Envelope{}, fmt.Errorf("peer sent invalid JSON: %w", err)
				}
				return Envelope{Type: head.Type, Raw: raw}, nil
			}
		}
		k, err := c.rw.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return Envelope{}, io.EOF
			}
			return Envelope{}, err
		}
		c.rbuf = append(c.rbuf, buf[:k]...)
	}
}

// Close closes the channel and the peer connection.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		_ = c.rw.Close()
		_ = c.pc.Close()
		if c.onClose != nil {
			c.onClose()
		}
	})
	return nil
}
