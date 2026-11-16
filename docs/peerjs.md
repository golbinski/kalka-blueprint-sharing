# PeerJS compatibility

kalka signals over the PeerJS server protocol, so one client works against
three servers: the public PeerJS cloud, a stock `peerjs-server`, and
`kalka-rendezvous` (spec §9). This note records how those servers behave
where the specification depends on it. Everything here was measured, not
taken from documentation; the source of each fact is given.

## Signaling

Taken from the `peer` 1.0.2 source (1.1.0-rc.2 behaves the same):

- Endpoint `<path>peerjs?key=&id=&token=`. The server answers `OPEN`, or
  `ID-TAKEN` for an id already held with another token.
- The server sets `src` on relayed messages.
- A message to an absent id is queued; the sender gets `EXPIRE` after
  `expire_timeout` (5 s) plus up to one 1 s sweep.
- A client silent for `alive_timeout` (90 s) is dropped.

kalka's client (`internal/peerjs`) is about 200 lines on
`coder/websocket`. It does not use `peerjs-go`, whose last release builds
on `pion/webrtc` v3 and implements the browser data protocol kalka does
not need.

## The public cloud is stricter about OFFER

The public PeerJS cloud closes the sender's socket (close code 1000),
without relaying, when an OFFER payload lacks `label` or `serialization`,
or when its SDP is not well formed. ANSWER and CANDIDATE pass in kalka's
shape, and `reliable` is optional. Stock `peerjs-server` checks none of
this.

This was found by driving the official PeerJS 1.5.5 browser client in two
tabs, capturing its WebSocket frames, comparing them field by field with
kalka's, and toggling each field against the cloud.

So kalka's OFFER carries `serialization: "raw"` next to `label:
"kalka/1"`, and `kalka-rendezvous` applies the cloud's rules, so that a
client passing kalka's own tests also passes the cloud.
`TestOfferShapeForPeerJSCloud` pins the fields; `TestCloudRelay` checks
the live cloud:

```sh
KALKA_CLOUD_TEST=1 go test ./internal/peerjs/ -run Cloud
```

The Go WebSocket handshake is accepted by the cloud.

## Offline detection

Dialing an absent peer fails with "peer is offline" after about 5.6 s
through stock `peerjs-server` (the expire timeout plus the sweep), and
after about 40 ms through the public cloud, which reports absent ids at
once. With `kalka-rendezvous` the timeout is configurable. Presence
probing (§9.3) has the same latency.

## Closing a connection

Closing a peer connection right after the last send can lose that message:
SCTP aborts the association with it still queued ("abort chunk"). A side
that ends a session therefore waits for the other's `bye` (up to 5 s)
before closing, and checks that need only an earlier message are made
before the exchange goes on (spec §8.3).

## ICE

`transport.ResolveICE` uses the configured servers, else
`GET <origin>/kalka/ice`, else defaults (spec §9.4). For `peerjs-cloud`
the defaults are those the PeerJS client library 1.5.5 ships: Google STUN
plus `turn:{eu,us}-0.turn.peerjs.com:3478` with the public `peerjs`
credentials, read from the package.

## Networks that block UDP

With a TURN server reachable on 443, set in each side's `config.yaml`:

```yaml
rendezvous: wss://<server>/
relay_only: true
ice_servers:
  - urls: ["turns:<server>:443?transport=tcp"]
    username: <u>
    credential: <p>
```

`kalka-rendezvous` serves TURN over TLS with `--tls-cert`
([rendezvous.md](rendezvous.md)).
