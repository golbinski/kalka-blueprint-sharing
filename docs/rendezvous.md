# Hosting `kalka rendezvous`

`kalka rendezvous` is the server mode of the `kalka` binary (spec §9.6). It introduces peers to each other and, when direct connections fail, relays their traffic. Blueprints are end-to-end encrypted between peers, and each peer checks the other's pinned key, so the server never sees blueprint content and cannot impersonate anyone. It keeps no state on disk and has no user database. Put SSO or network policy in front of it if you need access control.

## What it serves

| Where | What |
|---|---|
| `<path>peerjs` (WebSocket) | PeerJS-compatible signaling. |
| `/kalka/ice` | ICE servers with TURN credentials that expire (12 h by default). Always at the root of the origin (§9.4). |
| `/healthz` | `ok`, for load balancers. |
| `<path>` and `<path>peerjs/id` | PeerJS server info and a random id, as `peerjs-server` serves them. |
| TURN, UDP and TCP on `--turn-port` (3478) | The relay. Only on with `--turn-ip`. |
| TURN over TLS on `--turn-tls-port` (5349) | Only with `--tls-cert`. Use 443 on a separate IP where only HTTPS gets out. |

The server applies the same OFFER rules as the public PeerJS cloud: an OFFER needs `label`, `serialization` and parseable SDP. Otherwise the sender's socket is closed. It also limits clients (10 000) and messages queued for an offline peer (64 per peer).

Each kalka registers two ids: one derived from its email, and one derived from its public key (`kalka-k…`, §9.2). The server accepts a key id only with a fresh signature by that key (the `kproof` query parameter, §9.6), so nobody can occupy a teammate's key id to keep them offline. The check keeps no state and needs no clock beyond a 5-minute window, so keep the host's clock in sync. On the public cloud or stock `peerjs-server` the parameter is ignored and anyone holding a public key could take that key id; a squatter is still caught in the handshake before any key is pinned.

## Smallest setup

On a host your team can reach:

```sh
export KALKA_TURN_SECRET=$(openssl rand -hex 32)   # keep it to survive restarts
kalka rendezvous --listen :9000 --turn-ip 203.0.113.10
```

Each user then sets `rendezvous: http://rdv.corp.example:9000`, in setup or in `config.yaml`. The daemon fetches `/kalka/ice` on its own; nothing else needs configuring.

## With TLS

Directly:

```sh
kalka rendezvous --listen :443 --tls-cert rdv.pem --tls-key rdv.key \
  --turn-ip 203.0.113.10 --turn-host rdv.corp.example --turn-tls-port 5349
```

Users set `rendezvous: https://rdv.corp.example`. `--turn-host` must be the name on the certificate: WebRTC clients verify the TURN server's certificate against the system roots.

Behind a reverse proxy that ends TLS, route `<path>peerjs` (with WebSocket upgrade) and `/kalka/ice` to kalka. TURN cannot go through an HTTP proxy, so its ports must be reachable directly. For TURN over TLS, give kalka the certificate and keep signaling on plain HTTP behind the proxy:

```sh
kalka rendezvous --listen 127.0.0.1:9000 --path /kalka-rdv/ --plain-http \
  --tls-cert rdv.pem --tls-key rdv.key --turn-ip 203.0.113.10 --turn-host rdv.corp.example
```

```nginx
location /kalka-rdv/ { proxy_pass http://127.0.0.1:9000; proxy_http_version 1.1;
                       proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection upgrade;
                       proxy_read_timeout 120s; }
location = /kalka/ice { proxy_pass http://127.0.0.1:9000; }
```

Users set `rendezvous: https://corp.example/kalka-rdv/`. The proxy's read timeout must be longer than the 5 s client heartbeat; the server drops a client that has been silent for 90 s.

## Options

- `--key`: a PeerJS key other than `peerjs`. Clients add `?key=` to their rendezvous URL. It is not a secret: it only keeps unrelated PeerJS clients apart.
- `--allow-origin`: browsers from other sites are refused by default, and the kalka daemon sends no Origin. Add patterns only for your own browser tooling.
- `--relay-bind`: the address relay sockets are opened on. On a host with several addresses, set it to the address `--turn-ip` points to.
- `--json`: prints the URL and the TURN URLs at startup, for scripts.

## Firewall

| Port | Protocol | Purpose |
|---|---|---|
| `--listen` | TCP | Signaling and `/kalka/ice` |
| 3478 | UDP and TCP | TURN |
| 5349 (or 443) | TCP | TURN over TLS |
| 49152–65535 | UDP | Relayed traffic (`--relay-ports`) |
