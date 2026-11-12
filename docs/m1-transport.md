# M1 transport spike — findings

Date: 2026-11-12. Spec v0.3 §9, §16 M1.

## Results against the M1 exit criteria

| Criterion | Result |
|---|---|
| Two CLI processes sync a file through stock `peerjs-server` | **Pass.** `peer` 1.0.2 from npm (run with Node; Docker was not installed), `kalka spike listen` / `kalka spike send`. Both directions, comment round trip, files byte-identical; 10 of 10 repeated runs succeeded after the close fix below. Data channel open in 20–90 ms on localhost. |
| … through the public PeerJS cloud | **Fail, not caused by kalka.** Registration works; relaying does not. See below. |
| `hello`/`auth` binding; a rendezvous that swaps SDP fingerprints is detected | **Pass.** `TestChannelBindingDetectsMITM`: a malicious rendezvous answers the offerer itself, dials the answerer with its own offer and relays bytes between the two DTLS sessions it terminates. Both data channels open through the relay; both sides refuse with "channel binding failed". Fingerprints are read from the DTLS transport (`GetLocalParameters`, `GetRemoteCertificate`), never from SDP. A plain SDP fingerprint rewrite without a relay is already stopped by DTLS itself. |
| Note on `peerjs-go` | **Reimplemented**, see below. |
| UDP-blocked network with TURN | **Partly done.** `TestTURNOverTCPRelayOnly` forces relay-only ICE over TURN/TCP through the embedded `pion/turn` server, with time-limited credentials fetched from `/kalka/ice`, and asserts the selected pair is `relay`. A run on a real UDP-blocked corporate network, and TURN over TLS on 443, are still to do (commands below). |

## `peerjs-go`: reimplemented

`github.com/muka/peerjs-go` (Apache-2.0) was not adopted:

- last commit 2024-04-01; built on `pion/webrtc` v3 (current is v4.2);
- pulls in logrus, viper, gorilla/mux and an emitter library;
- most of its ~4,000 lines implement the browser PeerJS data protocol, which kalka does not need (spec 9.1).

The signaling client is ~200 lines (`internal/peerjs`) on `coder/websocket`. The PeerJS-compatible server (`internal/rendezvous`, ~250 lines) was written at the same time: tests need it, and it is most of M5.

Protocol facts were taken from the `peer` 1.0.2 source, as the spec asks: endpoint `<path>peerjs?key=&id=&token=`; `OPEN`, or `ID-TAKEN` for an id held with another token; the server sets `src`; messages to an absent id are queued and the sender gets `EXPIRE` after `expire_timeout` (5 s) plus up to one 1 s sweep; `alive_timeout` 90 s. 1.1.0-rc.2 behaves the same.

## Public PeerJS cloud

- **The Go WebSocket handshake is accepted** (the past issue in spec §17.8 does not reproduce): both ids register and receive `OPEN`, heartbeats keep the socket open.
- **But 0.peerjs.com closes the sender's socket (close code 1000) on any relayed message**, and the recipient receives nothing. This was reproduced with the same JSON sent from Node's built-in WebSocket, with and without an `Origin` header, and with UUID, hex and short ids. It is not specific to Go or to kalka's message shape.
- `TestCloudRelay` (`KALKA_CLOUD_TEST=1 go test ./internal/peerjs/ -run Cloud`) re-checks this; it failed on 2026-11-12.

**Consequence:** `rendezvous: peerjs-cloud` does not work today. The spec presents it as the zero-setup demo option (§5, §9.7). Options for the product owner:

1. Keep the option, label it "may not work", and point demos at a self-hosted server.
2. Drop it and make `kalka rendezvous` the only demo path. The server already exists; a demo is `kalka rendezvous` on one laptop plus `rendezvous: ws://<laptop>:<port>/` on both.

Recommendation: **2**, and pull the rest of M5 (flags, TLS, TURN wiring) forward to right after M2.

## Offline detection (scenario 10)

Dialing an absent peer through stock `peerjs-server` failed with "peer is offline" after **5.6 s** (5 s expire timeout + sweep). With `kalka rendezvous` the timeout is configurable (tests use 1 s). "Within a few seconds" holds with defaults; presence probing (§9.3) has the same latency.

## Bugs found and fixed

- **Ack lost on close.** The initiator closed the peer connection right after sending `sync_ack`; SCTP aborted the association with the ack still queued ("abort chunk"). Fix: the responder answers the ack with `bye`, and the initiator waits up to 5 s for it before closing.

## ICE (spec 9.4)

`transport.ResolveICE`: configured servers, else `GET <origin>/kalka/ice`, else defaults. For `peerjs-cloud` the defaults are those shipped by the PeerJS client library 1.5.5 (Google STUN plus `turn:{eu,us}-0.turn.peerjs.com:3478` with the public `peerjs` credentials), read from the package, not from memory.

## Still to do on a real network

Run on the corporate network (UDP blocked), with a TURN server reachable on 443:

```
kalka spike listen --email bob@corp.com --dir ./in --rendezvous wss://<server>/ \
  --relay-only --turn 'turns:<server>:443?transport=tcp' --turn-user <u> --turn-pass <p>
kalka spike send --email alice@corp.com --rendezvous wss://<server>/ \
  --relay-only --turn 'turns:<server>:443?transport=tcp' --turn-user <u> --turn-pass <p> plan.md bob@corp.com
```

The log line `path relay/... -> ...` shows which candidate pair carried the data. TURN over TLS is wired in `rendezvous.TLSListener` but not yet covered by a test.
