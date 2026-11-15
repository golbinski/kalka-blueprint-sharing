# M1 transport spike — findings

Date: 2026-11-12. Spec v0.3 §9, §16 M1.

## Results against the M1 exit criteria

| Criterion | Result |
|---|---|
| Two CLI processes sync a file through stock `peerjs-server` | **Pass.** `peer` 1.0.2 from npm (run with Node; Docker was not installed), `kalka spike listen` / `kalka spike send` (replaced by the daemon in M2). Both directions, comment round trip, files byte-identical; 10 of 10 repeated runs succeeded after the close fix below. Data channel open in 20–90 ms on localhost. |
| … through the public PeerJS cloud | **Pass** after matching the official client's OFFER shape (see below). 5 of 5 runs; data channel open in 80–230 ms. |
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

- **The Go WebSocket handshake is accepted** (the past issue in spec §17.8 does not reproduce).
- **The cloud is stricter than stock `peerjs-server` about OFFER.** It closes the sender's socket (close code 1000), without relaying, when the OFFER payload lacks `label` or `serialization`, or when its SDP is not well-formed. ANSWER and CANDIDATE pass in kalka's shape; `reliable` is optional. Stock `peerjs-server` 1.0.2 and 1.1.0-rc.2 check none of this.
- **How it was found:** the official PeerJS 1.5.5 browser client, driven in two Chrome tabs, relayed fine through the cloud. Its WebSocket frames were captured and compared field by field with kalka's, then each field was toggled from a Node script against the cloud.
- **Fix:** kalka's OFFER now carries `serialization: "raw"` next to `label: "kalka/1"`. `TestOfferShapeForPeerJSCloud` pins the fields; `TestCloudRelay` (`KALKA_CLOUD_TEST=1 go test ./internal/peerjs/ -run Cloud`) checks the live cloud.
- The first version of this note concluded the cloud did not relay at all. That was wrong: my probes imitated the client's message shape rather than copying it.

**Consequence:** `rendezvous: peerjs-cloud` works and stays the zero-setup demo option, with the warnings of spec §9.7. The PeerJS protocol keeps its point: one client works against the cloud, stock `peerjs-server` and `kalka rendezvous`. Since the cloud enforces rules the stock server does not, `kalka rendezvous` should enforce the same OFFER rules in M5, so that a client passing kalka's own tests also passes the cloud.

## Offline detection (scenario 10)

Dialing an absent peer failed with "peer is offline" after **5.6 s** through stock `peerjs-server` (5 s expire timeout + sweep) and after **39 ms** through the public cloud, which reports absent ids at once. With `kalka rendezvous` the timeout is configurable (tests use 1 s). "Within a few seconds" holds everywhere; presence probing (§9.3) has the same latency.

## Bugs found and fixed

- **Ack lost on close.** The initiator closed the peer connection right after sending `sync_ack`; SCTP aborted the association with the ack still queued ("abort chunk"). Fix: the responder answers the ack with `bye`, and the initiator waits up to 5 s for it before closing.

## ICE (spec 9.4)

`transport.ResolveICE`: configured servers, else `GET <origin>/kalka/ice`, else defaults. For `peerjs-cloud` the defaults are those shipped by the PeerJS client library 1.5.5 (Google STUN plus `turn:{eu,us}-0.turn.peerjs.com:3478` with the public `peerjs` credentials), read from the package, not from memory.

## Still to do on a real network

The spike commands were replaced in M2 by the daemon. To repeat the run on the corporate network (UDP blocked), with a TURN server reachable on 443, set in each side's `config.yaml`:

```yaml
rendezvous: wss://<server>/
relay_only: true
ice_servers:
  - urls: ["turns:<server>:443?transport=tcp"]
    username: <u>
    credential: <p>
```

then run `kalka` on both machines, create a team on one and join it from the other (`kalka team create`, `kalka team invite`, `kalka join`), and publish a document (`kalka publish plan.md`). TURN over TLS is wired in `rendezvous.TLSListener` but not yet covered by a test.
