# kalka — implementation plan

Plan for building `specification.md` v0.2 (2026-11-12). Section numbers like §7.2 point into the spec. Mockups: the design canvas "bp — trace view mockups" (two boards: *Trace view — divergence summary* and *Node selected — code and comments*).

---

## 1. Architectural decisions

These fill in what the spec leaves to the implementer. Each one can be overturned, but should be settled before M0 code lands.

| # | Decision | Reason |
|---|---|---|
| A1 | **The Go format library is the only parser.** The daemon sends the UI a parsed document model as JSON: sections with slugs and byte ranges, entries with derived states, references, trace graphs with divergences. The UI renders Markdown *per section* and never computes slugs, revs or anchors itself. | Anchors, `rev`, outdated and orphaned must agree between `kalka check`, the CLI, the merge and the UI. Two slug implementations would drift. |
| A2 | **Frontmatter is edited line by line, never re-serialized.** YAML is parsed read-only to find `kalka_id`/`owner`. Writes replace or insert exactly those lines. | §6.2: preserve byte for byte, including key order and comments. |
| A3 | **A blueprint is a list of byte spans**: frontmatter, body before log, log header, log items (entry or unparsed chunk), tail after log. Serializing an unmodified document returns the input bytes exactly. | Gives the golden-file round trips in M0 for free, and makes "change only what you must" checkable. |
| A4 | **Unparsed log content is attached to the entry before it**, or to the log header if it comes first. It moves with that entry when the log is sorted. | §6.4 says preserve "in place", §7.2 says rewrite the log in canonical order. Both cannot hold literally. *Decided 2026-11-12; written into spec v0.3 §6.4.* |
| A5 | **Divergences are computed in Go; trace layout is computed in TypeScript.** | `kalka check --json` can report divergences to agents. Layout is presentation only. |
| A6 | **Sync is a state machine with an injected transport**, tested over an in-memory pipe first and WebRTC second. | Scenarios §16.1 run in milliseconds in CI without a network. WebRTC gets its own smaller test set. |
| A7 | **One lock per `kalka_id`** in the daemon. Syncs, CLI writes and watcher reloads of one blueprint are serialized. | §14 re-read-before-write is simpler when no two operations race inside the process. |
| A8 | **Prompts are daemon objects** (`pending prompt`, with id, kind, timeout) pushed to the UI over SSE, answered by POST. If no UI tab is connected when a prompt arrives, the daemon shows a native popup offering to open the UI (see Q3). | Responder-side prompts (accept, verify, handoff) can arrive while the UI is closed. |
| A9 | **Frontend: TypeScript + React + Vite**, `markdown-it` with `html: false`, `@xyflow/react` (MIT) for the canvas view only, hand-written SVG for the trace view, `mermaid` bundled. Fonts are embedded (no Google Fonts; the mockups' Inter and JetBrains Mono must ship as files or be replaced by system stacks). | xyflow is React-first. Trace layout is specific (lanes, level alignment, gaps) and simpler in plain SVG than bent into a node library. CSP forbids remote origins (§11.2). |
| A10 | **YAML library: the maintained `go.yaml.in/yaml/v3` fork** (or `goccy/go-yaml`), not the archived `gopkg.in/yaml.v3`. Pin at implementation time. | §15. |

## 2. Repository layout

```
cmd/kalka/                 thin main around internal/cli
internal/cli/              CLI subcommands, daemon start, rendezvous mode (§12)
internal/core/             the node: sync, prompts, teams, replication, event bus; no HTTP (§4)
internal/daemon/           the process around the core: local API, web UI, popups (§13)
cmd/kalka-plugin-gh/       reference plugin (§10.4)
internal/name/             the one "kalka" constant set (binary, prefixes, protocol strings)
internal/blueprint/        normalize, spans, frontmatter, slugs, entries, ids, revs, limits
internal/merge/            merge(L, I, S) (§7.2), handoff chain, convergence check
internal/trace/            trace YAML parse, validation, divergences (§6.10)
internal/refs/             reference extraction (§10.1)
internal/config/           config.yaml, paths per OS, setup validation
internal/identity/         ed25519 key, peers.yaml, pinning, SAS code (§8)
internal/peerjs/           PeerJS signaling client (§9.1)
internal/transport/        pion WebRTC, framing, chunking, channel binding (§8.4, §9.5)
internal/syncproto/        initiator/responder state machines over a Conn interface
internal/store/            folder index, watcher, atomic write, own-write suppression (§14)
internal/plugins/          discovery, process runner, batching, cache (§10.2–10.3)
internal/rendezvous/       PeerJS-compatible server, pion/turn, /kalka/ice (§9.6)
web/                       frontend source; build output embedded via embed.FS
testdata/golden/           golden blueprints and expected outputs
test/e2e/                  scenario harness (§16.1)
```

## 3. Work breakdown by milestone

Order follows §16. M0 and M1 run in parallel: M0 is pure library work, M1 is the network risk.

### M0 — Format and merge library

1. `normalize`: BOM, CRLF/CR → LF, trailing whitespace, outer blank lines (§6.6). Line-ending style detection for writing.
2. Span parser (A3): frontmatter fence detection, H1/H2 scan that ignores fenced code blocks, review-log section detection (case-insensitive, first match), sections after the log.
3. Slugger: GitHub algorithm with duplicate suffixes. Test against a fixture list taken from real GitHub rendering, including non-ASCII letters.
4. Entry header parser: strict ` · ` separator, kind regex, RFC 3339 `Z` second precision, `§anchor` with optional `#node`, `rev:` 8 hex, `id:` 12 hex. Body up to the next header; `re:` line; leading blockquote; heading demotion on input.
5. `rev` and `id` hashing exactly as §6.5. Invalid id → entry dropped with a warning on merge, but still preserved on disk when the file is only read (the file is not rewritten by reading).
6. Frontmatter line editor (A2): insert `kalka_id`, insert `owner` if absent, set `owner` on handoff; create the block if missing.
7. Limits: 2 MiB file, 64 KiB entry body.
8. `merge(L, I, S)`: rules 1–6, the handoff chain walk, the convergence check used by §7.5.
9. Serializer: canonical log order `(timestamp, id)`, unparsed chunks per A4, line-ending style preserved, UTF-8 without BOM.
10. CLI: `kalka check [--json]` (exit codes: 0 ok, 1 problems, 2 usage), `kalka comment`.
11. Tests: property tests (idempotent, commutative on the entry set, never loses a valid entry, normalization equivalences, untouched frontmatter bytes); golden files from §16 M0.

**Handoff chain rule, made precise:** walk handoff entries in log order starting from `L.owner`; a handoff entry is a link only if its author equals the current owner in the walk; its target becomes the next owner. Accept if the walk reaches `S`. Write this down in the code as the definition; the spec does not say whether the author must match.

### M1 — Transport spike (time-boxed: 2 weeks)

1. Evaluate `muka/peerjs-go`: licence, code size, whether its signaling part works standalone. Decision note in `docs/m1-transport.md`.
2. PeerJS signaling client: `OPEN`, `OFFER`/`ANSWER`/`CANDIDATE`, `HEARTBEAT`, `LEAVE`, `EXPIRE`, `ERROR`, `ID-TAKEN`. Read URL shape, query params, heartbeat interval and ID rules from the current `peerjs-server` source.
3. Pion data channel `kalka/1`, 4-byte length framing, 16 KiB chunking.
4. `hello` / `auth` channel binding with fingerprints read from pion's DTLS transport (local certificate and remote certificate), not from SDP.
5. A test rendezvous that rewrites SDP fingerprints; the handshake must fail.
6. Runs: stock `peerjs-server` in Docker; the public PeerJS cloud (record whether the Go WebSocket handshake is accepted); a UDP-blocked network with TURN over TLS/443.
7. Measure the offline-probe time against `peerjs-server`. It answers an offer to an absent ID with `EXPIRE` after its expire timeout, so scenario 10 ("fails within a few seconds") depends on that server setting.

Exit: two CLI processes sync a file over each rendezvous option; written go/no-go on the public cloud.

### M2 — Daemon and end-to-end sync

1. Config load/save per OS (§5), setup validation.
2. Identity: key generation, 0600 / Windows ACL (`golang.org/x/sys/windows`), `peers.yaml`, pinning, SAS code, loud key-mismatch state.
3. Store: index of the folder by path and `kalka_id`, duplicate-id flagging, atomic write with Windows sharing-violation retry, own-write suppression by content hash, debounce 300 ms. Note: `fsnotify` is not recursive on Linux/macOS; add subdirectories as they appear.
4. `syncproto` state machines (initiator/responder) incl. `intent: handoff`, write-after-ack on the handoff sender, rejection with convergence content, 30 s silence and 5 min prompt timeouts with keep-alives.
5. Presence prober: peers in `peers.yaml` plus addresses typed this session, at most once a minute each, and on demand before a sync.
6. Local HTTP API skeleton (needed by CLI network commands): token file, Host check, Origin check.
7. CLI: `send`, `handoff`, `peers`, `verify [--confirm]`, all with `--json`.
8. Scenario harness: N daemons in one test process, temp folders, fake clock, in-memory signaling. All ten §16.1 scenarios as tests; a subset also over real pion on loopback.

### M3 — Web UI, linear review

1. API: document model (A1), list, entries POST, sync/handoff POST, prompts, SSE stream.
2. Views: Setup, Blueprints list, Review (linear), Peers. App shell with the header from the mockups: product name, blueprint title, owner and role ("you are reviewing"), view tabs, *Sync with…*.
3. Entry composer: kind (comment/deviation), anchor, reply, quote from selection, heading demotion.
4. Badges: outdated, orphaned, unparsed; key-mismatch banner; permanent public-cloud notice (§9.7); manual `owner:` edit warning (§7.4).
5. Hardening tests: `<script>` in a received blueprint does not run; `javascript:` links are dropped; wrong Host or missing token → 403; state-changing request with wrong Origin → 403. Strict CSP header served with every page.
6. Build: `web/` built in CI, output committed to the release only (not to git), embedded with `embed.FS`.

### M4 — Plugins

1. Discovery in `<config>/plugins` then `PATH`, with `.exe`/`.cmd` on Windows.
2. Runner: JSON on stdin/stdout, 10 s timeout, stderr to log, process killed on timeout, never arguments from the blueprint on the command line.
3. Batching per scheme per blueprint; 5 minute cache keyed by ref and frontmatter hash.
4. Generic rendering of `title`, `url` (http/https only), `status`, `snippet` (text, never HTML), `warnings`.
5. `kalka-plugin-gh`: `gh api` for contents and compare, payload validation, unknown `base_sha` warning, GitHub Enterprise `host`.
6. Tests: missing plugin → label as text; crash, timeout, garbage stdout → per-ref error, UI unaffected.

### M5 — `kalka rendezvous`

PeerJS-compatible signaling (in memory), embedded `pion/turn` with time-limited credentials, `GET /kalka/ice`, `--allow-origin`, TLS flags. Passes the M1 test set against itself.

### M6 — Canvas review

The spec gives no acceptance criteria. Proposed: every section and entry from the linear view appears; layout is deterministic for the same input; no state is saved; selecting a note scrolls the linear view to it. See Q4 about moving this after M7.

### M7 — Trace blocks

1. Library: parse `trace` fences, validation per §6.10 with block index and node id in errors, unknown keys ignored.
2. Divergences, one unit test per class, plus a three-lane block and a block with a labelled branch. *Unverified* and *Stale* are computed after plugin results arrive, in the daemon, not in the library.
3. Trace view, following the mockups:
   - Lanes as columns, most abstract on the left, flow top to bottom, lane names from the block in upper case.
   - A lower-lane node sits level with the first node it realizes. A node realizing several nodes (C3) stretches to span them, as drawn in the mockup.
   - Flow arrows solid grey; realization links dashed blue; divergent nodes orange with a class badge ("implementation-only", "merged · B2 + B3"); a labelled gap ("not in the business view") opposite an implementation-only node.
   - Header: block title, "trace *n* of *m*", counts line (steps, realization links, divergences, stale references).
   - Diagram / Source toggle; zoom in, zoom out, fit; legend.
   - Side panel, default: "Where the two views differ" with one card per divergence and a summary table. Lane names in the summary are taken from the block ("Business steps with no code" is built from lane names, not hard-coded).
   - Side panel, node selected: id and lane, divergence badge, title, `why`, realizes, resolved reference (title, snippet with line numbers, status, warnings, link), entries anchored to the node, comment box showing the anchor `§<slug>#<node-id>`.
4. Inline rendering of the same block, reduced, in the linear view.
5. An invalid block renders as a code block with the error beside it.
6. Mermaid renders offline from the bundled library.
7. Node-anchored comment survives a sync and becomes orphaned when the node id is removed.

## 4. Mockups versus spec — points to settle

| Mockup shows | Spec says | Proposal |
|---|---|---|
| Lane header "CODE @ 3f9c2a1"; panel text "3 commits on main after 3f9c2a1"; "Open on GitHub" | Core must not know `base_sha` or branch on plugin (§2.4, §10.3) | Lane header shows the lane name only. The snippet header's "@ 3f9c2a1" comes from the plugin's `title`/`status`. The link label is generic ("Open source") or a new optional `url_label` result field. |
| Header tabs: Document · Trace · Review log | Views: linear review, canvas review, trace (§11.1); no separate review-log view, no canvas tab | Tabs: Document · Canvas · Trace. A Review-log tab is cheap (entries sorted by time, filterable) — add it to §11.1 if wanted. |
| C3 spans B2 and B3 | "Placed level with the first node it realizes" | Follow the mockup; record it in §11.1. |
| Summary row "Code references resolved 4 of 4" | Unverified class (§6.10) | Same number, phrased per lane: "*<least abstract lane>* references resolved". |
| Inter, JetBrains Mono from Google Fonts | No remote origins (§11.2) | Embed font files (both OFL) or use system stacks. |
| Comment time "11 Nov, 17:40" | UTC in the file | Show local time; UTC in a tooltip. |

## 5. Gaps and risks found in the spec

1. **A forged handoff entry takes over a plan.** Entries are unsigned (§7.6), and rule 4 accepts a new body from anyone whose log carries a handoff chain to them. A teammate can therefore write `handoff · alice@ … → mallory@`, set `owner: mallory@`, and every peer accepts the new body. §7.6 accepts entry impersonation as a v1 limit, but this turns it into plan takeover. Options: (a) accept and document; (b) sign only `handoff` entries with the author's identity key (a header field, as §7.6 already foresees) and verify against pinned keys. **Recommendation: (b), it is small and fits M2.**
2. **Unparsed log content versus canonical reordering** — see A4.
3. **Frontmatter is part of the "plan".** Any tool or agent on a reviewer's machine that adds a frontmatter key makes that reviewer's next sync fail with `not-owner`. Consistent with the spec, but users will hit it; the convergence message should name frontmatter as the cause when only frontmatter differs.
4. **Responder prompts with the UI closed** — see A8.
5. **Presence probing cost.** Each probe is a WebRTC offer through the rendezvous. With 20 known peers that is 20 offers a minute per user. Acceptable for a team, but probe only peers seen in the last *n* days, and stop when the UI has been closed for a while.
6. **Recursive watching** is not native in `fsnotify` on Linux and macOS (see M2).
7. **Public cloud from Go** may be rejected at the WebSocket handshake (§17.8). If it is, the "three settings" story needs a hosted `kalka rendezvous` even for demos.
8. **M6 has no acceptance criteria** (see M6).

## 6. Open questions (in addition to spec §18)

- ~~**Q1** Is A4 acceptable?~~ Yes (spec v0.3 §6.4).
- ~~**Q2** Sign handoff entries in v1?~~ Yes: optional seventh header field `sig:`, verified against pinned keys (spec v0.3 §6.5, §7.2, §7.6).
- ~~**Q3** Responder side with no UI open?~~ Decided 2026-11-12: kalka runs per user, started at logon (not a Windows service, which runs in an isolated session and cannot reach the user's desktop or profile). A prompt with no kalka page connected shows a native popup, "Open kalka" or "Later". The answer is given in the UI, within the 5-minute prompt window; otherwise the sender gets "no answer" and retries. A background kalka tab raises a browser notification instead. Native notification-centre toasts need an app identity: on Windows, a Start-menu shortcut with an AppUserModelID; on macOS, a signed `.app` bundle. Both come with packaging (`kalka autostart`, installer), where the popup becomes the fallback.
- ~~**Q4** Move Canvas review (M6) after Trace (M7)?~~ Yes (decided 2026-11-12): Trace comes next, then Canvas. The trace view has mockups and a clearer purpose; the canvas view repeats the linear view's data.
- **Q5** Do the mockups' "Review log" tab and its view belong in v1?

## 6a. M0 implementation notes

Status 2026-11-12: library, merge, `kalka check`, `kalka comment`, `kalka version` done; unit, property, golden and fuzz tests pass; cross-compiles for `windows/amd64`.

Choices made while implementing that the spec does not spell out:

- **Slugs follow github-slugger**: underscores and combining marks are kept. The spec's one-line rule drops them; GitHub keeps them, and matching GitHub makes anchors work as links on GitHub too.
- **A strict entry header always starts an entry**, even inside a code fence. Other level-3 headings start unparsed content only outside fences. An entry that leaves a fence open can therefore not swallow later entries; `check` reports it as `entry-open-fence`.
- **A body that ends inside an unclosed fence or HTML block** (`body-open-block`) cannot get a review log: a heading after it would not be a heading. `comment` refuses, and such a file received from a peer is `malformed`. Detected with goldmark itself, not a hand-written rule list.
- **Invalid entries in the local file are kept** in it as they are, attached to the entry before them, with a note; only invalid *incoming* entries are dropped. The spec says "dropped on merge"; this reads it as "not merged" so nothing typed locally is lost.
- **`id` hash input** uses the anchor without `§` and the author exactly as written.
- **Merge needs the local user's own key** in its key lookup, since a handoff chain can pass through the local user.
- **Frontmatter keys are found line by line** (top-level `key:` at column 0), not with a YAML parser, so files with invalid YAML still work and nested keys named `owner` are never mistaken for the owner.

## 6b. M1 status

Done 2026-11-12, findings in `docs/m1-transport.md`. Sync passes through stock `peerjs-server` and the public PeerJS cloud (the cloud requires `label` and `serialization` in OFFER payloads and well-formed SDP); MITM detection passes; TURN/TCP relay-only passes in-process; `peerjs-go` not adopted. Still open: a run on a real UDP-blocked network, TURN over TLS. M5 note: `kalka rendezvous` should enforce the cloud's OFFER rules.

## 6c. M2 status

Done 2026-11-12. All ten §16.1 scenarios pass as tests (`internal/daemon/scenario_test.go`): several daemons in one process, each with its own config dir and folder, over the in-process PeerJS rendezvous and real WebRTC on loopback, with scripted prompts, a shared stepping clock and a fault hook that drops the connection after the responder writes (scenario 6). The suite passes with `-race`. Run by hand with two `kalka` processes over the public PeerJS cloud: first send (both sides showed the same code), comment round trip, signed handoff, `peers`, `verify`, and an offline report in 0.2 s.

Packages: `identity` (key, `peers.yaml`, pinning, code), `store` (index, watcher, safe writes), `syncproto` (state machines), `daemon` (process, local API, client); `fsutil.WritePrivate` (0600, owner-only DACL on Windows). `kalka spike` is removed; `kalka` + `kalka send` replace it.

Choices made while implementing that the spec does not spell out:

- **Code verification is a protocol step**: after `auth`, each side sends `verify {status}`, `verified` or `pending`. A pending side asks its user, then sends `verified` or `declined`. The sync goes on only when both sides end `verified`. Confirming sets `verified: true` locally at once, even if the other side then declines.
- **Keep-alives**: a side waiting on its user sends `keepalive` every 10 s. The other side's 30 s silence timer restarts on each one, and the prompt itself times out after 5 min.
- **`bye` carries `reason` and `detail`**, so the other side can show why a sync ended: a changed key, a declined convergence, or diverged plans.
- **Diverged is detected by both sides.** The responder computes the initiator's merge of the responder's file and replies `diverged` if that merge would fail. The initiator checks the `not-owner` content with its own keys and ends with `bye {reason: diverged}`.
- **Convergence happens on the same channel**: after a `not-owner` reply and the user's yes, the initiator saves the conflict copy, writes the owner's plan with its own entries, and offers again. The responder answers up to three offers per connection.
- **Handoff candidates are kept in memory** until the merged reply arrives. They are keyed by `kalka_id` and recipient and tied to a hash of the local file. A retry after a lost reply resends the identical candidate, so the recipient sees rule 3 and gets no prompt (scenario 6). A daemon restart forgets the candidate. A retry after a restart adds a second handoff entry. It is harmless, but the UI will mark it "unverified handoff" because Alice is no longer the owner at that point of the chain.
- **A key change is recorded** in `peers.yaml` (`key_changed: {pubkey, seen}`). From then on syncs with that email are refused before dialing, even if the old key comes back, until `kalka peers forget <email>`. The refusing side tells the other side why in its `bye`.
- **Presence probes** are a `CANDIDATE` with connection id `probe_<random>` and an end-of-candidates payload. A kalka peer answers `pong_<same>`, and an absent id gets `EXPIRE` from the server. The public cloud relays these probes (`TestCloudProbe`, opt-in). The result is `online`, `offline` or `unknown` (no answer within 8 s).
- **Locking per `kalka_id`**: a responder waits up to 10 s for a blueprint that is busy in another sync, then replies `declined` with "busy, try again".
- **Duplicate `kalka_id`** on the responder side is replied as `declined` with the explanation. The spec's reason list has no better fit.
- **Files to send must be inside the blueprint folder.** Replies are matched by `kalka_id` in the folder, so a file outside it could never receive them.
- **Local API**: `daemon.json` `{addr, token, pid}`, written 0600 and removed on exit. The CLI sends `Authorization: Bearer`. Browsers use the `kalka_token` cookie, and state-changing requests also need a matching `Origin`. Host must be `127.0.0.1:<port>` or `localhost:<port>`. `POST /api/sync` streams NDJSON events (`progress`, `prompt`, `prompt-closed`, `result`, `error` with a `kind` for agents). Prompts are answered with `POST /api/prompts/{id}`.
- **Prompts for incoming syncs** are asked in the daemon's terminal and listed at `GET /api/prompts`; the first answer wins. This is M2's answer to Q3 until the UI exists.
- **`setup` takes `--ui-port`**, needed to run two daemons on one machine. **`relay_only`** is a hidden config key for testing on restricted networks; it replaces the spike's `--relay-only`.

Still open: the real UDP-blocked network run and TURN over TLS (from M1). `kalka setup` is a CLI command for now; the setup form comes with the UI in M3.

## 6d. M3 status

Done 2026-11-12. `kalka` with no arguments starts the daemon and opens the UI. With no config, it first serves a setup form on port 7717. The web UI (`web/`: React 19, Vite, TypeScript, markdown-it) has these views: Setup, Blueprints, Review (linear), Peers, sync and handoff dialogs, live prompts, and toasts. Tested by hand in Chrome across two daemons over the public PeerJS cloud: setup, first send with the same code on both sides, accepting a new blueprint, commenting on a selection with a quote, sync back, Peers, the hostile `import.md`, and the page after a daemon restart.

Choices made while implementing that the spec does not spell out:

- **Token handling**: each daemon start makes a new token. `/?token=` sets an HttpOnly, SameSite=Strict cookie and redirects to `/`, so the token leaves the URL and the history. After a restart the old page shows "Open kalka from its link". `kalka open [--print]` reopens the UI from `daemon.json`.
- **Static files need no token. The API always does.** Pages accept only GET or HEAD. Every response carries the CSP (`default-src 'none'; script-src 'self'; …; frame-ancestors 'none'`), so there is no inline script and no inline style.
- **Document model** (`GET /api/doc`): the body is split into sections at top-level headings. Headings inside blockquotes or lists do not start a section. Each section carries its Markdown. Entries carry their state: outdated, orphaned, damaged, and verified or unverified handoff. A handoff chain counts as verified when each link has a valid signature and its author was the owner at that point; the first author is taken as the original owner.
- **`POST /api/entries`** and `kalka comment` share `blueprint.Append`. It validates the anchor and the reply target, puts a quote in a leading `> ` block, and refuses `handoff`; handoffs go through sync.
- **`owner:` edited by hand** is detected by the store: the owner changed in an external edit with no new handoff entry. It shows as a banner until kalka next writes the file.
- **Live updates** use SSE (`/api/events`), with these events: `folder`, `status`, `peers`, `prompt`, `prompt-closed`, `sync`. When a prompt arrives and no UI is connected, the daemon opens the browser (A8), at most once a minute.
- **Rendering untrusted Markdown**: raw HTML is escaped. Only `http`, `https` and `#` links are kept, and external links open in a new tab with `noopener noreferrer`. Images become links, since the CSP blocks remote images and they could track readers.
- **Fonts**: system font stacks, no embedded fonts.
- **Canvas and Trace tabs** were shown disabled until M6 and M7 (both done; see 6e and 6n). The mockups' Review log tab is left out (Q5).
- **Build**: `make` runs `npm ci && npm run build` into `internal/ui/dist`, which is embedded. The build output is not committed. A binary built without it serves a "UI not built" page.

**Prompts with no terminal (Q3, after M3):** `internal/notify` shows a native dialog: AppleScript `display dialog` on macOS (the texts are passed as arguments, never put into the script), `MessageBoxW` on Windows (closed with `WM_CLOSE` when the prompt ends), and `zenity` on Linux. With no dialog available, the browser opens instead, at most once a minute. One popup shows at a time. It closes when its prompt is answered or times out, and "Later" is not asked again for that prompt. Terminal prompts remain only when stdin is a terminal. The page asks once for notification permission and notifies about prompts while hidden. Packaging to-do: `kalka autostart on|off` (Task Scheduler at logon or HKCU Run, with no console window; LaunchAgent on macOS), toasts with an AUMID shortcut on Windows, and an `.app` bundle on macOS. Not tested on a real Windows desktop yet.

Still open: Q5; the M1 network items; the Windows popup run.

## 6e. M7 status

Done 2026-11-12, ahead of M4–M6 (Q4). `docs/example-trace.md` is a sample with two traces and a mermaid sketch.

- **Library** (`blueprint/trace.go`): finds ```` ```trace ```` fences in the body, including those nested in lists or quotes, but not those inside log entries. It validates per §6.10 with the block number, node id and file line in every error. Unknown keys are ignored. Errors are reported as `trace-invalid` *warnings*, so a broken block never blocks a sync. The graph divergences are computed here, with a unit test per class, a three-lane block and a labelled branch. `kalka check --verbose` prints divergences; `--json` lists `traces[]` with them.
- **Choices the spec leaves open:**
  - *Scattered*: a node's realizers in one lane do not form a connected piece of that lane's flow, with edges taken both ways.
  - *Reordered*: checked per pair A→B and per lower lane. A shared realizer does not count. The badge goes on A.
  - A block's *title* is the text of the heading it sits under, since the spec has no title key.
  - Node anchors are `§<slug>#<id>`, where the slug is the top-level heading the block sits under.
  - The same id twice under one heading gives the warning `trace-ambiguous-id`.
  - `Append` (CLI and UI) refuses unknown nodes. An entry becomes orphaned when its node is gone.
- **Unverified and Stale** are added by the daemon from reference results (`daemon.Options.Resolver`, the seam for M4). Until plugins exist, every least-abstract step shows *unverified* with its reason ("no plugin installed for gh:", "not a plugin reference", "no reference"). These count separately from the graph divergences, and nodes with them are not orange.
- **Layout** (`web/src/traceLayout.ts`, tested):
  - Rows on a grid.
  - A lower-lane node is level with the first node it realizes and stretches to the last (C3 spans B2+B3, as in the mockup).
  - A node never moves above its predecessor.
  - An implementation-only node inserts a row in the lanes to its left, labelled "not in the *lane* view".
  - A flow arrow goes around the lane side when it skips nodes. Branch labels are drawn.
- **Mockup points settled:**
  - Lane headers show the lane name only; no `@sha` from the core.
  - The link label is "Open source".
  - The summary rows are built from the block's lane names.
  - The Review log tab is still left out (Q5).
- **Views:**
  - Trace view: header with prev/next, counts, Diagram/Source, zoom ±/Fit, legend.
  - Side panel: divergence cards with a summary, or node detail. Node detail has why, realizes / realized by, the reference box (the snippet is text only, the link only if http/https), comments on the node, and a composer locked to the node's anchor.
  - The same block appears inline in the linear view, scaled to fit, and clicking a node opens the trace view on it.
  - An invalid block shows as code with its errors beside it.
- **Mermaid** 12.2.0 (MIT): its prebuilt browser bundle (5.5 MB) is served at `/mermaid/` and loaded only when a page has a diagram.
  - It runs in a frame sandboxed by both the iframe attribute and a CSP `sandbox allow-scripts`. The frame has an opaque origin, no `connect-src`, and no cookie or API access, and talks to the page by `postMessage` only.
  - Mermaid's SVG uses `<style>` elements, so only this frame allows `'unsafe-inline'` styles.
  - The main page's CSP gained `frame-src 'self'`.
- Checked in Chrome: inline and full views of both blocks, a node comment saved and shown on the node, Source, the invalid block, the mermaid sketch, and no CSP violations. `FuzzParse` was seeded with trace blocks and run for 60 s.

Still open: Q5.

## 6f. M4 status

Done 2026-11-13.

- **Library:** `blueprint.PluginRef` recognizes `scheme:payload` (never http, https, mailto, file, javascript, data, blob or about). `Document.References()` lists the plugin refs in the body and in trace nodes, in order. `Frontmatter.Map()` is the context sent to plugins.
- **Host** (`internal/plugin`):
  - It looks for `kalka-plugin-<scheme>` in `<config>/plugins` first, then on PATH.
  - Each call is one process with no arguments, the JSON request on stdin, a 10 s timeout, and `WaitDelay` 1 s for children such as `gh`.
  - Stdout is capped at 8 MB, stderr at 64 KB (stderr is logged). Text is capped at 500 chars, a snippet at 64 KB, warnings at 20. A URL that is not http(s) is dropped.
  - Any failure of the plugin as a whole becomes an error on each of its refs.
  - The output buffer must not embed `bytes.Buffer`: the promoted `ReadFrom` let `io.Copy` skip the cap (found by a test).
- **Daemon:**
  - Resolution runs in the background, one batch per scheme. The page gets "resolving…" at once, and a `refs` SSE event when results arrive, which makes it reload.
  - Results are cached by frontmatter and ref: 5 min, or 1 min for failures.
  - A missing plugin is reported at once and not cached, so installing one takes effect on the next look.
  - `Options.Resolver` became `Options.Plugins`.
- **`kalka-plugin-gh`** (built and shipped alongside `kalka`):
  - It calls `gh api` with no shell and prompts disabled. Frontmatter needs `repo`; `base_sha` and `host` (GitHub Enterprise) are optional, and all three are validated before use.
  - Payload: `path[#La[-Lb]]`. `.`/`..` segments, absolute paths and odd characters are refused.
  - Round 1, in parallel: the repo (default branch) and the `base_sha` commit. An unknown `base_sha` (an unpushed commit?) or no `base_sha` gives a warning and shows the default branch.
  - Round 2, in parallel: one compare call for the whole batch, and contents 4 at a time, once per path.
  - Staleness is per file: "changed on *branch* since *sha* (*branch* is N commits ahead)". A per-file commit count would cost one call per file.
  - The snippet holds at most 200 lines, or the first 30 when no lines are named. The link is a blob permalink at the commit.
  - The opt-in live test runs against `cli/cli`.
- **UI:**
  - Inline plugin refs render as spans, not links. Their states are ok, stale, pending and error; a ref with no plugin is plain text.
  - Click or Enter opens a reference card (snippet as text, warnings, "Open source", "Resolved on your machine by …").
  - Trace nodes use the same box. The trace summary names the plugins that resolved references.
- **Limits:** a path with spaces cannot be referenced in the body, since CommonMark link destinations and autolinks cannot contain spaces. A trace node's `ref:` has no such limit.
- Checked in Chrome against `cli/cli`: pending → resolved without reload, README ok, go.mod stale, a missing file, a scheme with no plugin, node detail in the trace view, and no console errors. The check found two UI bugs, now fixed: a CSS class clash that blew up snippet text, and stale refs shown as errors.

## 6g. M5 status

Done 2026-11-13. The operator guide is `docs/rendezvous.md`.

- **Signaling** (`rendezvous/signal.go`, mostly written in M1) now enforces what the public cloud enforces: an OFFER without `label` or `serialization`, or with SDP that does not parse or has no media section, closes the sender's socket and is not relayed. A client that passes kalka's tests passes the cloud.
  - Ids are checked with the PeerJS id rule.
  - Clients are capped (10 000), and so are messages queued for an absent id (64 each, 1024 in all).
  - A reconnect with the same token replaces the old socket.
  - Messages with no `dst`, or addressed to the sender, are dropped.
- **`rendezvous.Start`** binds every listener before it returns, so a port conflict shows at once. It serves:
  - signaling at `<path>peerjs`;
  - `/kalka/ice`, always at the origin root (spec 9.4), so a proxy must route it as well;
  - `/healthz`;
  - the PeerJS info and `/<key>/id` endpoints.
- **`kalka rendezvous`** flags:
  - `--listen`, `--path`, `--key`, `--allow-origin`.
  - `--tls-cert`/`--tls-key`, which serve HTTPS and TURN over TLS. `--plain-http` keeps signaling on HTTP behind a TLS-ending proxy, since TURN cannot pass through one.
  - `--turn-ip`, which turns TURN on; `--turn-host`, the certificate name used in the URLs; `--turn-port`; `--turn-tls-port`; `--relay-bind`; `--relay-ports` (default 49152–65535, for firewall rules).
  - `--ice-ttl` (12 h), `--stun` (default: the TURN port), `--json`.
  - The TURN secret comes from `KALKA_TURN_SECRET` or `--turn-secret-file`, never the command line. Without one, a random secret is made per run.
- **Tests:**
  - The M1 transport suite passes against the stricter server.
  - `TestKalkaRendezvous` (transport) runs the handshake through the assembled server under a path prefix and a custom key, with ICE from its `/kalka/ice` and both peers forced onto its TURN relay, then checks id-taken and offline.
  - `TestRendezvousTLS` (cmd) starts the real command with a test certificate. It checks HTTPS endpoints, `wss` signaling, refusal of a foreign Origin, a TURN-over-TLS allocation in the configured port range, and refusal of a wrong TURN password.
- **Run by hand** with three processes on one machine: `kalka rendezvous` with TURN, and Alice and Bob's daemons set up with `rendezvous: http://127.0.0.1:9000`. Checked: presence, first send with the code check, accept-new, a comment synced back, identical files.
- **TURN over TLS (open since M1):** the server side is verified. The client side is pion/ice, which checks the certificate against the system roots, so the TURN host needs a publicly trusted certificate. A run from a real UDP-blocked network is still open.

## 6h. Packaging status

Done 2026-11-13. The user guide is `docs/install.md`.

- **`kalka --background`** is the mode autostart uses. No browser opens at start, though setup still opens one when there is no config yet. Prompts still raise notifications or popups, and output goes to `<config>/kalka.log` (0600, one rotation at 5 MB). `--no-open` keeps its meaning: no browser and no popups, for scripts and tests.
- **`kalka autostart on|off|status [--json]`**, per user, with no administrator rights:
  - Windows: an HKCU `Run` value that starts `kalkaw.exe --background`, plus the AppUserModelID registration (`HKCU\Software\Classes\AppUserModelId\com.github.golbinski.kalka`). On, it also starts kalka now unless one is running.
  - macOS: a LaunchAgent, loaded with `launchctl bootstrap`, which starts it now. RunAtLoad with no KeepAlive, because a second, hand-started kalka holding the port would make launchd restart in a loop.
  - Linux: an XDG autostart entry, with the Exec value quoted per the spec.
  - `status` flags an entry whose program is gone.
- **`kalkaw.exe`** is `cmd/kalka` built with `-H=windowsgui` (the release checks the PE subsystem). A console child of a GUI process gets its own console window, so plugins are started with `CREATE_NO_WINDOW`. `rundll32` and PowerShell (also no window) are the only other children.
- **Windows toasts:** `notify.Toast` shows a reminder-scenario toast with "Open kalka" (protocol activation of the page URL) and "Later". It removes the toast when the prompt is answered or times out. It runs through Windows PowerShell 5.1's WinRT bridge: the script is a constant, and the XML (built with `encoding/xml`), tag and app id reach it through environment variables. Without the registration it reports unavailable, and the message box shows instead. **Not run on a real Windows desktop yet**, like the message box. The first Windows session should check:
  1. the toast's sender name;
  2. that a click opens the browser;
  3. removal when the prompt closes;
  4. no console flashes from plugins.
- **macOS `.app` bundle: dropped for now.** Notification-centre banners need a signed and notarized bundle (an Apple Developer account). The dialog works without it. Revisit if kalka is distributed at work.
- **Release** (`tools/release`, `make release VERSION=x.y.z`): archives for windows/amd64 and arm64, darwin/arm64 and amd64, linux/amd64 and arm64, plus `SHA256SUMS`. They are reproducible: `-trimpath`, no build id or VCS stamp, and entries sorted with the commit time and fixed owners. Two runs gave identical checksums. `kalka-plugin-gh` now takes its version from the build as well.
- **CI** (`.github/workflows/ci.yml`) runs web tests, vet and `go test -race` on Linux, Windows and macOS, and publishes a GitHub release on a `v*` tag. It has not run yet: the repository has no remote.

## 6i. R0 status: spec v0.4

Done 2026-11-13. The second feature request (teams, roles, replication, domains, seeds, blueprint reference) was triaged in `docs/plan-teams-replication.md`, which replaces `docs/plan-domains-seeds-reference.md`. The product owner accepted the defaults to all eight questions.

`specification.md` is now v0.4:

- New sections were appended so that existing numbers, cited throughout the code, stay valid: §19 Teams and roles, §20 Team replication, §21 Threat model. Inside §6 there are new subsections: §6.11 Seeds, §6.12 Domain documents, §6.13 Blueprint reference and hints.
- Amended:
  - §1 non-goals, and §2 with two new principles (content never blocks; brainless).
  - §3 glossary, §5 config files.
  - §6.2: the table of frontmatter keys kalka reads and writes.
  - §6.5: v2 signatures on every entry; `release` and `budded` kinds.
  - §7.2 (the verified copy wins in the union) and §7.6, rewritten.
  - §8 (team keys, invites), §9.2 (email ID and key ID), §9.3, §9.5 (`caps`, `invite`, the `team_*` messages), §9.6 (key-ID proof), §9.7.
  - §11.1 views, §12 CLI, §14 files.
  - §16: acceptance criteria for M6 and R1–R8, and scenarios 11–15.
  - §17 items 17–19.
  - §18, now open questions plus a table of fifteen decisions.
- The four financial examples (`internal/blueprint/trace_test.go`, `docs/example-trace.md`) were replaced with parcel logistics.

Order from here: R1 reference and hints, R2 team log library, R3 signed entries and releases, R4 seeds, M6 Canvas, R5 key IDs, R6 teams and invites, R7 replication, R8 domains (about 18 weeks).

## 6j. R1 status: blueprint reference and hints

- `internal/reference/BLUEPRINT-REFERENCE.md` is the reference itself. It is embedded in the binary and has Agent Skills frontmatter (`name`, `description`) plus `kalka_reference: kalka-blueprint@v1`. It contains rules for agents, the six sections each with a "why agents benefit" line and a parcel-delivery example, when to split off a seed, and a skeleton. `reference_test.go` checks that its version matches `blueprint.HintsVersion`, that its structure holds, and that it contains no financial examples.
- `kalka reference export [-o <file|dir>] [--force] [--json]` and `kalka reference version`. `kalka version` also reports the reference version.
- `blueprint.Hints(d)` returns info-level problems with `hint-*` codes, only for documents that opt in. They are never part of `Problems()`. `kalka check` lists them only with `--verbose` and in JSON, and they never change the exit code.
- The daemon's document view has a separate `hints` field. The Review page shows a quiet strip: each hint can be dismissed, the dismissal is kept in `localStorage` (`kalka.hint-dismissed.<kalka_id>.<code>`), and a "Show dismissed hints" link brings them back. The Blueprints list does not show hints.

## 6k. R2 status: team log library and harness

- New package `internal/teamlog`, with no I/O and no clock:
  - operations, their canonical encoding, hash, signature and strict decoder (`op.go`);
  - `Replay` (`replay.go`);
  - builders, where `Signer.Remove` returns the removal followed by its reassignments (`build.go`);
  - invite tokens, ids, proofs and `Check` (`invite.go`).
  - `internal/name` gains `TeamLogSigContext` and `InviteContext`.
- `docs/teamlog.md` fixes the encoding and the replay rules. Spec §19.1, §19.4, §19.5 and §21 were updated to match:
  - "dependants fall with their parent" was replaced by support checks, because the old rule let a removed owner poison the log;
  - seniority is a chain of grants, so nobody can appoint an owner senior to themselves;
  - self-removal is refused;
  - `reassign-document` names `from`.
- Tests: every required and added case of the plan, as scenario tests (`replay_test.go`); decoder fuzzing (`FuzzDecode`); invite tokens: expired, tampered, wrong team, relayed proof.
- The harness (`sim_test.go`) plays random honest, back-dated, ground and pointless operations, with partial gossip. It checks the invariants on every replica and on the union, and checks that the roster and owner order do not depend on the visiting order. The R2 exit run: 100 000 histories, 2.37 million operations, no failure, in 13 minutes on 4 cores. The harness found two problems while the rules were being settled: concurrent grants made the owner order depend on visiting order, and the greedy decision alone could leave no owner. Both are fixed. `go test` runs 400 histories; `KALKA_SIM_N` sets more.

## 6l. R3 status: signed entries, releases and tombstones

- **Entries.** `Entry.Digest`, `Sign`, and `Verify(kalkaID, key, allowV1)` returning unsigned / invalid / v1 / v2 (`internal/blueprint/entry.go`). `VerifySig` keeps the private-document rule: v2, or v1 on handoffs. New handoffs are signed with v2, so a v0.3 peer shows them as unverified (D3).
- **Who signs.** `blueprint.Append` takes a signer. The daemon signs everything it writes, and so does `kalka comment`, which loads `identity.key` from the config directory. A file without a `kalka_id` cannot be signed.
- **Releases.** Release entries (`release.go`): `Entry.Release()`, `NewRelease`, `Document.PlanHash`, `Team`, `Deleted`. The release body names the plan's owner, so ownership follows the chain. Spec §20.4 now says so.
- **Merge.** The classic merge keeps the copy of an entry whose signature verifies against the pinned key. `merge.MergeTeam`/`DocsTeam` apply §20.4 for documents with `team:` through the `merge.Team` interface (keys ever bound, plus the override from reassignment or domain assignment):
  - the release chain;
  - plan selection by the head, with unreleased local edits kept and a void local release replaced;
  - incoming entries filtered to team keys, with local ones kept;
  - tombstone stubs that keep only releases;
  - new documents only with a valid release.
  Nothing calls `MergeTeam` yet; replication (R7) will.
- **Rule change.** Two different releases of one version freeze the document ("ownership disputed") instead of picking the lower id. Otherwise any member could hijack a document with a forged version 1 and a searched-for id. A reassignment with `after: 0` settles a dispute.
- **CLI and UI.** `kalka check` reports signature counts (verified / unchecked / invalid / unsigned) in JSON and with `--verbose`, using the installation's own key and pinned peers when a config exists. The UI shows "unsigned" and "signature invalid" badges and renders release entries in words. The "by a removed member" badge needs the team log locally and comes with R6/R7.
- **Tests.** v2 sign and verify, moved signature, v1 only where allowed, release round trip, plan hash independent of log and line endings. At merge level:
  - relay of a release taken, relay of an unreleased edit refused;
  - a non-owner's release void;
  - ownership following releases;
  - reassignment cut-off, and a discarded reassignment restoring the old owner's release;
  - a tombstone beating a stale copy both ways and keeping only releases;
  - the entry filter and restored signatures (team and private);
  - new documents needing a release;
  - a forged version 1 freezing the document until a reassignment settles it;
  - a property test over random histories: idempotent, direction-independent plan and entries, version never down.
  The existing scenarios and golden files pass unchanged.

## 6m. R4 status: seeds

- **Library** (`internal/blueprint/seed.go`):
  - `Status`, `IsSeed`, `Parent`, `Origin`, `Created`;
  - `Bud`, which assembles the seed and appends the signed `budded` entry (anchored like its origin, `re:` the origin);
  - `AddBacklink`, the idempotent retry of the second write;
  - `Entry.Budded`, `Promote`, `NewKalkaID`.
  - Without `-m`, the problem defaults to the quoted origin entry.
- **Daemon** (`seeds.go`):
  - `Bud`, `Backlink`, `Promote` (owner only), `Seeds`;
  - API `GET/POST /api/seeds`, `POST /api/backlink`, `POST /api/promote`;
  - the document view carries `seed` (parent, missing backlink), and budded entries carry a link to their seed.
  - `Store.CreateIn` writes the seed beside its parent.
- **CLI.** `kalka seed <parent> --title … [--from <id>] [-m …] [--why …]` and `kalka seeds [--json]`. Both work without the daemon, as `kalka comment` does. A parent without a `kalka_id` gets one first.
- **UI.**
  - "Create seed" in the document header and on deviation entries; a dialog that offers "Add the backlink" when the second write failed.
  - A seed strip with its parent link, "Promote to draft" for the owner, and a missing-backlink offer.
  - Budded entries render as links.
  - A Pre-backlog page listing title, owner, parent and age.
  - Checked in Chrome: bud, open the seed, see the pre-backlog, promote.
- **Tests.**
  - Library: seed keys, parent plan unchanged, one signed budded entry, idempotent retry, budding without an origin, promotion.
  - Daemon: budding from a reviewer's copy with the backlink and seed surviving syncs both ways; backlink retry with no second seed; promotion by the owner only; handoff of a seed, then promotion.
  - CLI: id given to a local parent, seed written beside it, signed backlink, `seeds` output.
- **Not yet.** Publishing a seed with its parent's team waits for R7.

## 6n. M6 status: canvas review

- **No graph library.** The canvas is drawn by hand like the trace view (absolute cards, one SVG for the lines), so `@xyflow/react` (A9) was not added: the layout is a column per kind and needs no dragging, edge routing or saved state.
- **Layout** (`web/src/canvasLayout.ts`): body sections as cards down the middle in reading order, then General and, when there are any, Orphaned, as in the linear view. Entries are notes to the right, joined to their section's card; replies sit under the note they answer, indented (at most four levels) and joined to it. Plugin references a section links to (`refsIn`) are small cards to its left, joined with a dashed line. Card heights come from text length (`plainText`, clamped lines), never from the browser, so the same document gives the same layout. Nothing is saved; zoom is per page view.
- **Shared grouping** (`web/src/entries.ts`): `groupEntries` decides where an entry belongs, for the linear view and the canvas alike, so both show the same entries.
- **Selecting.** A note opens the linear view at `?at=e-<id>`; a section card at `?at=s-<slug>` (`general`, `orphaned` for the other two). The linear view scrolls the target to the middle and outlines it for two seconds. A reference card opens the same popup as in the linear view.
- **UI.** The Canvas tab is enabled (route `#/canvas/<path>`), with zoom in, zoom out, Fit and a legend. Checked in Chrome with the test folder: references resolve in place, and a note opens the document scrolled to it.
- **Tests** (`canvasLayout.test.ts`): every section and entry of the linear view appears beside the card the linear view puts it under (including nested headings, replies to entries missing from the file and orphans); reply threading; references once per section; reading order without overlap; the same layout for the same input; General without entries; note texts for handoffs, seeds and releases; `refsIn` and `plainText`.

## 6o. R5 status: key-derived peer IDs and capabilities

- **`internal/peerid`**: `Email` (moved from `transport.PeerID`, which now calls it), `Key`, `Fingerprint`, `Display`, `IsKey`, and the registration proof `Proof`/`CheckProof` (`kproof` = base64url(pubkey).unix.base64url(sig), context `kalka-keyid-v1`, 5-minute skew, no state). New name constants `KeyIDContext`, `KeyProofContext`.
- **Signaling.** `peerjs.DialQuery` adds query parameters. `kalka rendezvous` refuses a key id without a valid proof ("Key id refused: …"); `Server.NoKeyProof` plays a stock server in tests. Live check on the public cloud (`KALKA_CLOUD_TEST=1`, `TestCloudKeyIDProof`): a key id with the proof registers and is relayed to, so the cloud ignores the parameter. Stock `peerjs-server` reads only `id`, `token` and `key` from the query (its source); not run live this time, since it is not installed here.
- **Handshake.** `hello` gains `caps` (omitted when empty, so a hello without caps is byte-identical to v0.3's; fixture test). A remote key id must equal the key id of the presented key, checked before the verifier, so a squatter fails without a `key_changed` record. `Peer.Caps`, `Peer.ViaKey`; capability `keyid/1`. The `invite` field waits for R6, where it is used.
- **Daemon.** Two signaling loops, email id and key id (fresh proof on each reconnect); the email one decides "online" and readiness, the key one is reported separately (`key_online`, `key_error`). Each peer's announced caps are kept in `peers.yaml` (`caps`, for the pinned key only). Dialing and probing use the key id, from the key endpoint, when the key is pinned and the peer announced `keyid/1`; on a failed dial (offline) they fall back to the email id, never on a failed handshake. Progress says which id was used.
- **UI and CLI.** Status carries `key_peer_id` and the user's fingerprint; the Peers page shows "Your key fingerprint" and, per peer, the pinned key's fingerprint and whether it is reached by key id; `kalka peers` gained a FINGERPRINT column. The public-cloud notice is reworded per §9.7.
- **Tests.** `peerid` derivation vector and proof cases; rendezvous refusals (none, another key's proof, stale, junk) and acceptance; transport new-to-new over key ids, old and new over email ids both ways, the old-hello fixture, a squatter on a proof-less server (detected, no `key_changed`), and refusal of the squatter by `kalka rendezvous`; daemon `TestPeerIDChoice`: an old peer stays on email ids, after its upgrade the next contact records `keyid/1` and later syncs use key ids both ways, after a downgrade the key id is offline and the sync falls back to the email id. The M1 transport suite passes unchanged.

## 6p. R6 status: teams and the invite flow

- **Log file.** `teamlog.ReadFile`/`AppendFile`: `<folder>/<team>/<team>-team.log`, one canonical operation per line. `AppendFile` re-reads just before writing and writes the union atomically (compare-and-swap, retried); lines it cannot decode are kept and reported, never dropped. Teams are found by scanning the folder's direct subfolders; a second team with a held name gets `<name>-2`.
- **Config files.** `invites.yaml` (the issuing owner's open invites, holding the token and so the secret) and `joins.yaml` (pending, joined, failed), both owner-only, re-read before every change. An invite can be withdrawn (`DELETE /api/invites/{id}`, "Withdraw" in the UI); the spec does not name this, it only stops further use on this installation.
- **Handshake.** `hello.invite = {id, team, proof}`; `team` was added to the spec's `{id, proof}` so the owner can say "another team" instead of "proof does not match". `transport.HandshakeInvite`; `Peer.Invite`. The daemon's incoming handshake now pins after the handshake, so a joiner's key is vouched for by the invite instead of being pinned on sight. A v0.3 owner ignores the field and asks for a code; the joiner sees `verify`, ends, and says the owner needs to update. `team/1` is **not** announced yet: it promises replication (R7).
- **Owner side** (`join.go`, `admit`): unknown or withdrawn invite, other team, bad proof, used by another key, expired, other email (the invite's email wins), not an owner any more: each ends with a `bye` naming it (`invite-*`). Otherwise `add-member {email from the invite, key, invite id}`, the invite is marked used with the joiner's key, the key is pinned verified, and the whole log goes out as `team_log`. A used invite presented again by the same key is a lost-connection retry: the log is sent again and nothing is added.
- **Joiner side.** `kalka join` checks locally (token damage, expiry, email, own invite, already a member), warns when the token's rendezvous differs, stores the join and dials the owner's key id now and every `JoinEvery` (1 min) until expiry; this survives restarts. It checks that the log starts at the token's team hash and adds this key through this invite, writes it into the team folder, pins the owner verified and the other members where nothing is pinned. A clash with an existing pin is never overwritten: the pinned key stays for one-to-one syncs, and the Team view shows the clash loudly (§8.2).
- **Roster.** `CreateTeam`, `Invite`, `Remove` (with `reassign-document` for the member's published documents held here, cut-off at the last release seen), `SetOwner`, `AssignDomain` (cut-off from a held `domain:` document of the team). Every change checks owner status under the local key first and confirms the new operation is effective after replay. Self-removal is refused (rule 7).
- **API, CLI, UI.** `/api/teams…`, `/api/joins`; `kalka team create|invite|members|log|invites|remove|add-owner|remove-owner|assign-domain [--team] [--json]`, `kalka remove` shows the reassignment preview until `--yes`; `kalka join <token> [--wait]`. Team view: members with fingerprint and pin state, domains, invites with a copyable token, history (discarded operations struck through with the reason), Join and Create forms, removal with a preview. A `teams` bus event refreshes it.
- **Trust for team traffic** (`teamKeyFor`) is in place for R7; R6 has no team traffic besides the join itself, so one-to-one syncs keep using pins and codes. After a join both pins are verified, so their first one-to-one sync asks for no code.
- **Tests.** `TestInviteFlow` (required, real WebRTC on loopback): join with no prompt on either side, both logs hold the `add-member` by invite, both pins verified, files owner-only, a later sync asks no code, a reused token from another installation is refused as used, and a second `add-member` for the same invite id is discarded at replay. `TestInviteRefusals`: wrong email (locally and at the owner), token for another team, damaged secret, rendezvous hint, expired token (locally and at the owner). `TestJoinRetries`: owner offline, joiner restarts, owner returns, join completes. `TestTeamRoster`: name rules, members cannot change the roster, owners, domains, removal with reassignment. `teamlog.TestLogFile`, `identity.TestPinVerified`. Checked by hand with two daemons and `kalka rendezvous` on this machine: CLI create, invite, join, members, log, and the Team view in Chrome.

## 6q. No compatibility between milestones

Product owner, 2026-11-14: everything up to the first release is one version, so no milestone keeps earlier builds working. Removed: the `hello.caps` capability list and `keyid/1` (peers are dialled by key id whenever their key is pinned, with the email-id fallback kept for an unreachable key id), capabilities in `peers.yaml`, the daemon's v0.3 `legacy` test mode and the old-hello fixture, the v1 handoff signature (`SigV1`, `kalka-handoff-v1`; `Verify` takes no `allowV1`, and the states are unsigned, invalid, valid), and the joiner's branch for an owner too old to accept invites. The spec's compatibility paragraphs (9.5, 20.7) are gone and decision 4 in 18.2 now records this. Compatibility with external servers (stock `peerjs-server`, the PeerJS cloud) stays a requirement. In the plan above, "behind `team/1`" no longer applies to R7.

## 6r. R7 status: team replication

Done (spec 20.1–20.6):

- `kalka publish <file> [--team] [-m note]` and `kalka delete <file>`; in the UI, *Publish to team…* in the document header, and a team strip on published documents with *Publish update* (with a note), a two-step *Delete for the team*, and notes for unreleased edits, disputed ownership, a pending reassignment, an owner who left the team, and entries that stay local.
- Entry badges: *local only* (signed by someone outside the log) and *removed member*.
- `internal/daemon/replicate.go`: inventory and digest over the release chains and tombstones, the session (`team_sync`, `team_log`, `team_inv`, `team_want`, offers with `intent: replicate`), rounds at start, every ten minutes with jitter, when a member comes online, and a push after local changes and log changes. `merge.Replica` strips entries not signed by team keys before a document leaves.
- `internal/daemon/publish.go`: releases, the release made by a new owner after a reassignment, tombstones with the last copy in `.kalka/trash`.
- Team view: the last round (reached, not online, received), refusals with reasons, *Replicate now*, and the documents deleted for the team. `POST /api/teams/{team}/replicate`.
- Scenario tests in `replicate_test.go`: private documents never leave, a tombstone beats a stale peer, mutual removal, reassignment over four nodes, relaying of entries through a member who is neither author nor owner, and the push.
- Checked by hand with two daemons and `kalka rendezvous`: publish, push to the member's team folder, publish update from the UI, the member's view.

Not done: one-to-one syncs of published documents still merge with 7.2; replication corrects them at the next round.

## 6s. R8 status: team domain documents

Done (spec 6.12, 19.6):

- `internal/blueprint/domain.go`: `kind: domain`, `domain`, `version`, the `<team>/<domain>@v<N>` reference, snapshot names; warnings for a bad domain name or version.
- `merge.DomainRule`: for a domain document the assigned domain owner releases every version above the cut-off; unassigned, nobody does. The daemon gives the merge a domain-aware team per document (`held.teamFor`), preferring the local copy's kind, so stripping `kind: domain` from an incoming copy does not escape the rule.
- Store: snapshots are ignored by the index and the watcher; `SaveSnapshot`, `Snapshots`, `Snapshot`, `FindDomain`; `Info` carries team, domain and version.
- Daemon (`domains.go`, `publish.go`): `Release(path, note, incorporates)` on the publish path (version bump, incorporates checked against the document's entries, snapshot); snapshots of received releases; the new domain owner takes the document over after `assign-domain`; removing a member reassigns their domains; handoff of a domain document refused; `DocumentAt` for kept versions; `Domains()`; reference and incorporation fields in the document view. Background work (rounds, takeovers) is waited for when the daemon stops.
- Replication reports a refusal when an incoming plan's release is void, even when its entries are taken.
- CLI: `kalka release <file> [-m] [--incorporates id,...]`, `kalka domains [--json]`. API: `POST /api/release`, `GET /api/domains`, `GET /api/doc?v=`.
- UI: Domains view; domain strip with version, domain owner, draft note, *Release vN…* (dialog with a checklist of open comments), and a version switcher over kept versions, shown read only; comments folded as "incorporated in vN"; the dismissible "newer domain version available" indicator, and notes for a version not kept here or an ambiguous team.
- Tests: `TestDomainReleases` (merge), `TestDomainReleaseByNonAssignedOwner` (the required scenario: no peer applies it and each says why), `TestDomainRelayAndReassignment`, `TestDomainIncorporatedAndReferences`. Checked by hand with two daemons: assign, CLI release, reader comment, release from the dialog, the indicator and a kept version.

Not done at the time: the removal preview did not list the domains that move with a member. Since v0.6 a domain document is an ordinary document, so it was always listed; T9a labels it.

## 6t. D-series status: background desktop app

Plan: `docs/plan-desktop-app.md`; spec v0.5 (§4, §12, §13, §22, §23).

D1 done (core, daemon and CLI split):

- `internal/core` (was `internal/daemon`): `core.Node` holds all sync, prompts, teams, replication and the event bus (`bus.go`). It serves no HTTP; `Run` runs the node only. `Bus()`, `ConfigDir()`, `Logf()`, `Team()` and `TeamHash()` are what front ends use.
- `internal/daemon`: `Daemon{*core.Node}` adds the local API listener, `daemon.json`, the web UI and popups (`present` subscribes to prompts). API, client and popup tests moved with it on their own harness (`harness_test.go`).
- `internal/cli`: the whole CLI, `cli.Run(args, stdin, stdout, stderr)`. `cmd/kalka` is a thin main, so the desktop shell can link the same CLI. Release builds set `internal/cli.version`.
- `TestCoreImports`: the core imports no `net/http` itself, and nothing it reaches in this module imports the UI, popups, daemon, CLI, desktop shell, autostart or Wails.

D2 done (the lock, `kalka daemon`, `kalka stop`):

- `internal/lockfile`: an exclusive, non-blocking lock (flock; `LockFileEx` on one byte far past the content on Windows, so the PID stays readable). The PID in the file is for messages only. A killed holder leaves its PID, and the lock is free at once.
- `daemon.New` takes `<config>/daemon.lock` before the core reads anything, unless the caller passes one in `Options.Lock`. The CLI takes it before the setup form. `Run` releases it last, after `daemon.json` is gone. `POST /api/shutdown` and `Client.Shutdown` stop a daemon.
- Stopping drains: when Run's context ends, no new work starts and no new connection is made (dials are cancelled, further peers of a round are skipped). Sessions already connected finish on a work context that outlives the stop by up to `core.DrainTimeout` (30 s). The rendezvous connections stay up until then.
- CLI: `kalka daemon [--background] [--no-popups]` (a second one prints "already running (pid N)" and exits 0); `kalka stop [--wait 1m] [--json]` (waits for the lock to come free; with none running it exits 0); bare `kalka` starts `kalka daemon --background` detached when nothing answers, then opens the UI. `--no-open` and the bare-`kalka` daemon mode are gone.
- Autostart runs `kalka daemon --background` (`autostart.Args`). The macOS LaunchAgent has `KeepAlive` with `SuccessfulExit` false, so launchd restarts kalka only after a crash. `autostart.Spawn` is the detached start the CLI uses (Setsid; `DETACHED_PROCESS` on Windows).
- Tests: `TestAcquireTwice`, `TestKilledHolder` (lockfile); `TestOneDaemonPerConfigDir` (two daemons in one process, another port, shutdown over the API, restart); `TestSingleDaemon` (the CLI in subprocesses: a second start, a second `ui_port`, kill with no clean-up and a stale `daemon.json`, `kalka stop`, auto-start in the background); `TestStopDrains` (core). Checked by hand: with an offline team peer, `kalka stop` took 10 s before dials were cancelled on stop, and 0.2 s after.
- Not run on Windows yet (no machine; D8). `--no-ui` is D3.

D3 done (local API hardening; no socket, per decision 7):

- `Sec-Fetch-Site` on `/api/`: absent, `none` or `same-origin` pass; `cross-site` and `same-site` get 403 with kind `origin`, for reads and bearer requests too. The token link is exempt: Chrome sends `cross-site` for a link opened from another page (found by hand with an extension-opened tab), and the check adds nothing there.
- `kalka daemon --no-ui` (`Options.NoUI`): page paths answer 404, no cookie is set, popups are off; the API answers the CLI.
- `POST /api/open-external` (`Options.OpenExternal`, the CLI passes the system browser): only absolute `http`/`https` URLs with a host and no user info. The page does not call it yet; the desktop shell needs it (D6).
- Tests: `TestAPIGuards` is a 23-row table (Host, port, token forms, cookie, Origin, Sec-Fetch-Site, the token link, static files); `TestNoUI`; `TestOpenExternal` (file:, javascript:, custom schemes, scheme-relative, user info refused). Checked by hand in Chrome: the page loads, reads, writes, streams events, and the Mermaid frame draws.

D4 done (the CLI with and without a daemon):

- `internal/cli/access.go`: `daemonFor` for daemon commands (send, handoff, peers add|forget, verify, team changes, join, publish, release, delete, open): the running daemon, else `kalka daemon --background` started detached and waited for (`ensureDaemon`). `--no-start` (stripped from the arguments wherever it appears) or `KALKA_NO_START=1` fails with `not-running`; no config fails with `not-set-up`, starting nothing. Errors are `{"error", "kind"}` with `--json`.
- Views (`status`, `peers`, `team members|log|invites`, `domains`) use the daemon when it answers, else `core.Open`: a read-only node (`identity.LoadKey`, never creates a key) that is never run, so presence is unknown and nothing connects. View JSON carries `"daemon"`; text output ends with a "not running" note.
- New `kalka status [--json]`: running and pid, online state, identity, folder, rendezvous, peers online of known, prompts waiting. Paused and last sync come with D5.
- `kalka setup` also creates the identity key, so views work before the daemon first runs.
- Tests: `TestCLIClasses` (subprocesses: file commands start nothing; views from files with `"daemon": false`; `--no-start` and the variable; two daemon commands at once give exactly one daemon; live views; the daemon's index sees a CLI comment; no config starts nothing); `daemon` twice and `stop` twice are in `TestSingleDaemon`.

D5 done (sync controls, settings and notices, headless):

- Pause in the core: `SetPaused`, `Paused`, `Options.Paused`. A supervisor (`network`) runs both rendezvous registrations and the join and probe loops while not paused, and restarts them on resume. Rounds return at once while paused, leaving a queued push queued; the resume kicks a round. `Sync`, `Join` and `ReplicateNow` return `ErrPaused` (kind `paused`). A node started paused counts as ready. `Status.Paused`.
- `core.Summary`: online, paused, peers reachable of known (peers plus team members), last completed exchange either side (`noteSync` in rounds, replication responses and one-to-one syncs), prompts, pending joins, queued pushes. `GET /api/summary`.
- Notices (`notices.go`), a `notice` bus event: new comments by others on a document the local user owns (one-to-one through a store wrapper that sees before and after; replication in `applyReplica`), never for a first copy or one's own entries; join admitted (owner) and join done or failed (joiner); a newer domain version than one referenced by a local document (owned, or with no owner line yet), once per domain and version. `SetNotifications`. Handoffs stay prompts. The prompt hub's event type is now `PromptEvent`.
- Daemon: `state.yaml` (`paused`, `notifications`, `keep_running_on_close`, `close_notice_shown`, `autostart_initialized`), `GET`/`PUT /api/settings`, `POST /api/pause`, `POST /api/resume`. Start at login goes through `Options.Autostart` (the system entry in the real binary, nil in tests: reported unavailable). A start in the paused state raises one notice. Notices reach `Options.Notify` only while no page is connected (`Bus.Watch` subscribes without counting as a page); the CLI shows them as Windows toasts where registered, and drops them elsewhere (spec 22.4: unsigned macOS shows notices only in the window).
- CLI: `kalka pause|resume [--json]` (daemon commands); `kalka status` now reports paused, peers reachable of known, last sync, prompts, joins and queued pushes (null without a daemon, paused from `state.yaml`).
- Web: Settings view (pause, notifications, start at login, keep running when closed, with the summary line); a paused banner with *Resume sync* replaces the offline banner; `notice` events show as a browser notification when the tab is hidden, else as a toast.
- Tests: `TestPause` (registrations close, the peer's probe says offline, no round, no sync, no join, local publish reaches the peer after resume, summary numbers), `TestCommentNotices`, `TestJoinAndReplicaNotices` (core); `TestSyncWithNoWindow` (no listener, no page, then a page that came and went), `TestSettingsAndPause` (settings API, `state.yaml`, pause across a restart with one notice) (daemon); pause, status and a sync refused as `paused` in `TestCLIClasses`. Checked by hand in Chrome: Settings view, pause, banner, resume.
- Not done: the "window hidden" variant of the no-window test needs the shell (D6). Start at login was not toggled by hand, to avoid installing a real LaunchAgent.

D6 done (the desktop shell):

- `internal/desktop` (plain Go, tested without a display): the tray model from the summary and settings (status line, "N waiting for you", pause label, start-at-login check, icon state); the close policy and the start table of plan decision 3 as functions; the WebView2 version check (`pv` under the Evergreen client key, HKLM both views then HKCU; minimum 86.0.616.0); tray icons drawn in Go per state (template form on macOS); `TrayHost` on Linux asks D-Bus for a StatusNotifierWatcher.
- `Shell` drives the window and tray through a `UI` interface. `Prepare` takes the lock (in-process daemon via `cli.ServeDaemon` with `Shell: true`, the setup form shown in the window), or finds the holder: with a shell attached it sends `POST /api/ui/show` and exits 0 (`--hidden`: exits 0 silently); headless, it attaches. The shell always talks to the daemon over HTTP, in both modes. First start turns start at login on once (`autostart_initialized`). Quit asks first and stops the daemon after its rounds (the attached one too, and says so); closing hides with a one-time notice, minimises without a tray host, or with keep-running off quits without asking (an attached daemon keeps running). The daemon going away (`kalka stop`) ends the shell.
- Daemon: `GET /api/events?shell=1` registers a shell (`Bus.SubscribeShell`, not counted as a page); while one is attached, popups and `Options.Notify` are silent and the summary reports `"shell": "desktop"`. `POST /api/ui/show` publishes `ui-show` or fails with kind `no-shell`. The token link with `window=1` also sets the readable `kalka_window` cookie. Fixed a D5 race: the notice presenter now subscribes before the paused-start notice can be published.
- CLI: `cmdDaemon` is now `ServeDaemon(ctx, DaemonOptions)`, shared with the shell. Bare `kalka` shows the shell's window when one is attached, else opens the browser. `kalka status` reports the attached window.
- Autostart: `autostart.TargetFor` starts the shell with `--hidden` when the running binary is the shell (`DesktopBuild`, macOS) or `kalka-desktop` is installed next to it, else `kalka daemon --background` (`kalkaw.exe` on Windows). The macOS plist and Linux entry take any argument list.
- Web: in the window (cookie) links to other sites go through `/api/open-external`, browser notifications and their banner are off (the shell presents), and Settings shows "keep running when closed" only there.
- `cmd/kalka-desktop` (tag `desktop`; without the tag only the CLI): with arguments the CLI, without them or with `--hidden` the shell. The Wails v3.1.0 binding (`wails.go`): accessory app on macOS, tray menu, a hidden window that loads the token link, Wails notifications where its service starts (Windows, Linux, macOS inside an app bundle), else dialogs for prompts and notices only in the window; WebView2 missing offers the browser.
- Tests: `TestTrayModel`, `TestClosePolicy`, `TestStartTable`, `TestWebView2Version`, `TestTrayIcons`, `TestHashes`, `TestPresentWhenHidden` (the "window hidden" variant), `TestShellInProcess` and `TestShellAttach` (real daemons, fake UI and fake autostart: start table, ui/show, close notice once, pause and start at login from the tray, quit and cancelled quit, first-run autostart once, `kalka stop` ends the shell); `TestShellStream`, `TestWindowCookie` (daemon); `TestTargetFor` and the `--hidden` LaunchAgent (autostart); `TestSmoke` with `-tags desktop` (the real shell hidden, status, ui/show, stop). `shell.test.ts` (web).
- Checked by hand on macOS: the shell starts with the daemon in-process, its page connects, bare `kalka`, a second start and `--hidden` exit 0, attach to `kalka daemon`, `kalka stop` ends the shell in both modes. The window and tray were not looked at (no screen capture of other apps here); `docs/desktop-testing.md` is the checklist. `GOOS=windows` builds with the tag; the Linux shell needs cgo and GTK, so it was not built here. Build on macOS with `MACOSX_DEPLOYMENT_TARGET=13.0` to silence linker warnings (D7 puts it in the build).

D7 done (builds, packaging, signing configuration, documentation; no CI, per the product owner's answer 2):

- `tools/release` (split into `main.go`, `archive.go`, `signing.go`, `pe.go`, `desktop.go`): per target the headless archive as before, where the Windows zip now holds `kalka-desktop.exe` in place of `kalkaw.exe`; the per-user NSIS installer from `kalka.nsi.tmpl` (only the stock MUI2, LogicLib and nsExec; user PATH added and removed with a length guard; WebView2 check offering the download; uninstall entry under HKCU; upgrade stops the running kalka; uninstall keeps `%APPDATA%\kalka`); `Kalka.app` in a zip (Info.plist with `LSUIElement`, an `.icns` drawn by `desktop.AppIcon`, `kalka-plugin-gh` beside the executable, macOS 11 minimum); the Linux desktop archive (`.desktop` file, icon, `PACKAGES.txt`). Desktop artifacts the host cannot build are skipped with the reason. `-purego` builds the macOS and Linux shells without cgo.
- Signing is configuration: `KALKA_WIN_SIGN_CMD` (a command template with `{file}`), `KALKA_MAC_IDENTITY` (hardened runtime, timestamp), `KALKA_MAC_NOTARY_PROFILE` (notarytool, then stapler), from the environment or a `KALKA_SIGNING` file. Without them each artifact says "unsigned"; macOS apps are ad-hoc signed. A failing signing command fails the build. The PE subsystem check runs on every Windows build: `kalka.exe` console, `kalka-desktop.exe` GUI.
- `kalkaw.exe` is gone: start at login on Windows runs `kalka-desktop.exe` (`--hidden`, or `daemon --background` with the new `kalka autostart on --headless`). Plugins are also found beside the kalka executable, so the app started at login finds `kalka-plugin-gh` without a PATH. On macOS the single binary started bare in a terminal hands off to a detached shell. The window, bundle and Linux entry use the new app icon.
- `make desktop`; `make release` builds what the host can. `docs/install.md` rewritten around the desktop app; new `docs/headless.md` (running without the app, what IT needs to allow); package checks added to `docs/desktop-testing.md`; spec §15 and §23 (Windows notification registration, the cgo-free try-out).
- cgo-free back ends (Wails v3.1.0 `purego`): `kalka-desktop` builds for macOS (both architectures) and Linux from this Mac, and the macOS build passes `TestSmoke`. Kept optional until used by hand.
- Tests: `TestSplitCommand`, `TestLoadSigning`, `TestSignWindows` (unsigned succeeds and says so, a configured command runs on the file, a failing one fails), `TestPESubsystem` (real console and GUI builds, a swapped pair fails), `TestNSISScript`, `TestNSISCompile` (compiles the script around placeholder files when makensis is installed), `TestInfoPlist` (release); `TestTargetFor` updated, `TestFindAndDescribe` covers the executable's folder, `TestAppIcon`.
- Checked by hand: `go run ./tools/release -version 0.3.0-rc1` on this Mac built both Windows zips, both macOS archives and apps (arm64 cross-compiled with cgo), both Linux headless archives, and skipped the installers (no makensis) and the Linux desktop archives with their reasons. The unzipped amd64 `Kalka.app` passes `codesign --verify --strict`, runs as the CLI, starts hidden with the daemon in-process and the shell attached, and stops with `kalka stop`.
- Installer compiled (NSIS 3.13 from Homebrew): `-targets windows/amd64,windows/arm64` builds both `_setup.exe` files; each holds the three executables of its architecture and `README.txt`. makensis runs with `-WX`, so NSIS warnings fail the release. The installers have not been run: install, upgrade, PATH handling, the WebView2 check and uninstall wait for the Windows pass (D8).
- Fixed a race in `kalka stop` (seen once as a `TestSingleDaemon` failure under the full `-race` run): an idle daemon's `Run` returned and `hs.Close()` dropped the `POST /api/shutdown` answer before it was sent, so the client reported "not running". The handler now flushes before shutting down, and the server shuts down gracefully for at most a second, with request contexts cancelled at shutdown so event streams end at once.
- Not done: no Windows or Linux desktop run; webview differences not yet recorded (the window not looked at by eye).

## 6u. T-series status: kalka around teams

The request (teams only, several teams and Personal, two roles, take-over by team owners, transfers between teams, join links, the app as the primary interface, a separate rendezvous binary) was triaged in `docs/plan-teams-only.md`. The product owner took every default of its section 10 on 2026-11-15.

**T0, spec v0.6.** Done 2026-11-15.

- `specification.md` v0.6. Deleted rather than marked: one-to-one sync and its merge rules (old §7.1, §7.2 rules 2, 4, 5, §7.4, §7.5, the private half of §7.6), pinning and the verification code (old §8.2, §8.3), email IDs and the config `team` salt, `verify` and the `sync`/`handoff` intents, `sync_reply` and `sync_ack`, the Peers view, prompts, `.conflict-` files, the domain-owner role and `assign-domain`.
- Rewritten: §7 Ownership (owner, immediate handoff, reassignment and the race, take-over, displaced edits); §8 Identity and trust (team logs only; channel binding is now §8.3, so code comments citing "8.4" are updated in T1); §9.2 key IDs only, §9.3 presence per team, §9.5 messages with `team_doc`, §9.6 `kalka-rendezvous`; §11 with the switcher, join screen, waiting list (new §11.3); §12 with the command table, the parity rule (§12.2) and the error kinds (§12.3); §13 scoped endpoints; §14 the folder layout; §16 the T milestones and fifteen team scenarios; §18.4 the v0.6 decisions; §19.1 rule version 2 with `accept-transfer` and `decline-transfer`; §19.3 two roles; §19.4 rules 8 and 9; §19.5 reassignment and removal; §19.6 transfer decisions (was domain assignment); §19.7 links and parsing; §20.4 the release chain with anchors; new §20.7 transfers with the attestation scheme; §21; §22 tray and notices; new §23.1a the URL scheme.
- `docs/teamlog.md` rewritten for rule version 2: signature domain `kalka-teamlog-v2`, the new argument tables, rules 8 to 10, the harness invariants for reassignments and transfer decisions, `inviter` and `ParseJoin` for invites.
- One choice T0 made that the plan left to T1: documents travel in a session as `team_doc {team, kalka_id, filename, content}`. Replication already sent only `sync_offer` with intent `replicate` and never used `sync_reply` or `sync_ack`, so those two are deleted.

**T1, one-to-one sharing removed.** Done 2026-11-15.

- Deleted: `Sync`, the prompt hub and `--no-popups` popups, pins and `peers.yaml`, verification codes, email IDs and the second signaling registration, the email-ID dial fallback, `KeyChanged` refusals, the one-to-one merge (`merge.Docs`/`Merge` now take the team and nothing else), handoff entries and their views, `OwnerEdited` and `SaveConflict`, `sync_offer`/`sync_reply`/`sync_ack`/`verify`, the Peers view, the sync and handoff dialogs, `/api/sync`, `/api/peers`, `/api/verify`, `/api/prompts`, `kalka send`, `handoff`, `peers`, `verify`, `setup --team`. `kalka daemon --no-notifications` replaces `--no-popups`.
- One registration per node, on the key id (`Status.peer_id` is now the key id; `key_peer_id`, `key_online`, `key_error` and `Summary.prompts` are gone). Presence probes go to the members of the teams held, by the key the log binds. Replication sends documents as `team_doc`.
- Incoming connections: an invite goes to the join, a first `team_sync` opens a session, anything else gets `bye not-member`. Spec 20.3 has the responder merge the initiator's log operations before it checks membership, so a pre-check by team was wrong (it broke the mutual-removal convergence test). Instead, the responder computes the inventory digest, which reads the files, only for a caller the log already counts: anyone else gets the log heads alone, then `not-member` after the merge if it still is not a member.
- Tests: the one-to-one scenarios are deleted. Seeds, signatures, hints, traces and comment notices are re-seated on publish plus replication (`node.share`, `node.rel`). The merge tests and the golden round trip run as team merges (fixture entries without a signature stay in Alice's file; the golden outputs were regenerated). New: one registration per node (`rendezvous.Server.IDs`), a stranger and an impostor with a member's email both get `not-member` with nothing but heads sent, a key the log does not bind fails the initiator's handshake, and `TestNoOneToOneLeft` keeps the deleted messages, endpoints, files and flags out of the Go code and the page.
- Left for later milestones: handoff returns as a release naming the new owner in T3 (`kalka handoff` is absent until then, and so is the seed handoff test). `kalka check` checks signatures against the user's own key only; team keys there belong with T6. `kalka join` while paused reports on stderr without a JSON `kind`, which is part of the T6/T7 parity work. `Summary.peers_online`/`peers_known` keep their names but count team members.

**T2, Personal and team folders.** Done 2026-11-15.

- Location decides scope (`core/scope.go`). A file belongs to the team whose folder holds it, at any depth. Anything else is Personal: `personal/`, files at the root, and other top-level folders that are not a team's. `store.ScopeOf` names the top folder, and `store.Info.Scope` carries it. Duplicates now count per scope: the same `kalka_id` in Personal and in a team's folder is not a duplicate. `Store.Find` stays global, and `Store.FindIn(dir, id)` searches one folder.
- One test of what replicates, `isTeamDoc`. The file must lie in the team's folder, `team:` must name that team, it must have no format errors and no duplicate in the scope, and it must hold a valid release under the team's log. `replicaSet` takes only that folder's files. An incoming document's fallback lookup searches the team's folder only. Every place that went from `team:` to a team now goes from the location: local changes, delete, the doc view, seeds, domains and removal reassignments (`heldAt`). `heldByName` remains only for a domain referenced by name.
- Publishing a Personal file picks the team named, else the file's `team:`, else the only team. It releases the file, then moves it into that team's folder (`Store.Move`, which refuses if the file changed underneath) and reports the new path in `PublishResult.Path`. A Personal file that already holds a release for that team is only moved. A file in a team's folder is published in place, and naming another team for it is an error.
- Mismatches are listed and never replicated. `TeamView.not_shared` lists each with its reason: not published, `team:` names another team, no valid release, duplicate id, or a Personal file whose `team:` names the team. `DocView` gains `scope` and `mismatch`. `kalka team` prints them as `not shared:` lines; the Team view has a "Not shared" list, and the document view has a banner.
- `personal/` is created by `kalka setup`, by the setup page and when a node starts. It is never a team's folder (`teamDirs` skips it). `team create personal` is refused, and a joined team of that name gets the folder `personal-2`.
- Tests (`core/scope_test.go`):
  - Two-team isolation, with recorded channels both ways and probes for a foreign id answered like an unknown one.
  - Two same-name teams on one machine (`parcels`, `parcels-2`) exchange nothing.
  - Personal never leaves: a draft with `team:`, and a file holding a real release that was moved back into Personal, neither appear on the channel and both are listed.
  - Publish moves the file and reports the path; a second publish stays in place.
  - Mismatches are listed with reasons and not replicated.
  - `personal` is refused as a team name.
  - A mutation check confirmed the tests catch a dropped location filter.
  - Test helpers follow moved files (`node.follow`, used by `node.publish`). Domain documents are written in the team's folder. Recording uses a tap switch installed once per node, so swapping recorders does not race the sessions.

**T3, roles and ownership.** Done 2026-11-15.

- **Team log, rule version 2** (`internal/teamlog`):
  - Signatures now cover `kalka-teamlog-v2`.
  - `assign-domain` is deleted, along with `State.Domains` and `ValidDomainName`.
  - `reassign-document` carries `plan`, which must be empty exactly when `after` is 0. It needs no removal, and `from` is not judged (rule 9).
  - A cut-off below one the signer already saw is discarded as a "stale cut-off" (rule 8).
  - `State.Reassigned` holds every effective reassignment per document as an anchor, ordered by cut-off, one per cut-off (the later in visiting order). This fixes the defect where a document reassigned twice lost its first cut-off.
  - The harness generates reassignments of present members' documents at random cut-offs, concurrent with removals and with each other. It checks that the new owner is a member in the operation's view, and that no counting reassignment has a stale cut-off. 100 000 histories pass (22 269 stale cut-offs exercised; 903 s).
- **Merge** (`internal/merge`):
  - `Team.Override` became `Team.Anchors(id)`. At an anchor with cut-off n, only its owner may sign n+1, and the chain restarts there: a gap or dispute below does not void what stands above.
  - `ReleaseChain` gains `Pending` (the last anchor's owner has not released yet) and `NextSigner()`. `DomainRule` is deleted.
  - A void release above the head no longer blocks the owner. Only a dispute above the last anchor, or a tombstone, does.
- **Core:**
  - `Publish` now only takes a Personal file, or an unreleased file in a team's folder, to its first release. For an already-published file it is an error pointing to `kalka release`. `Release` makes every later version.
  - `Handoff` is a release naming the recipient: no network, works paused. It refuses a non-member (`not-member`) and a Personal file (`in-personal`).
  - `Reassign` writes the operation with the highest valid release held, and needs `not-team-owner` to pass. When the new owner is the local user, the release follows at once (`takeReassigned`, also run after every log change). The new owner releases the plan they hold.
  - `TakeOverRelease`/`TakeOverDelete` are the UI's take-over (spec 7.4): a reassignment to the actor, then the action.
  - `release()` now holds the document lock, so the background release and an explicit one cannot both happen. kalka's own release after a reassignment is skipped when already made.
  - Notices:
    - `reassigned`: once per operation, to the previous and the new owner. Seeded at start; operations made locally are never noticed.
    - `handoff`: when a release someone else signed makes you the owner.
    - `displaced`: replication replaced a plan no release names. The file is first copied to `<team>/.kalka/trash/<name>.displaced-<time>.md`, which is never indexed.
  - Domain documents follow their owner like any document. Releasing a second document for a domain the team folder already holds is refused (`domain-exists`). Concurrent ones are both kept, and each shows `domain_clash`. A `domain:` reference to the name is shown as ambiguous.
  - Removal reassigns the documents whose next signer is the member, from the team's replica set, citing each head's plan.
  - Stable error kinds (spec 12.3) start in `core/errors.go`. `ownerOf` returns `not-member`/`not-team-owner`, and the API passes kinds through.
- **CLI and API:**
  - New commands `kalka handoff <file> <email>` and `kalka reassign <file> <email>`; both put the error kind in `--json`. `team assign-domain` is gone.
  - Routes: `POST /api/handoff` and `/api/reassign` are new; `/api/release` and `/api/delete` take `take_over`.
  - The page: *Release* replaces *Publish update*. *Hand off…*, *Reassign…*, and a team owner's *Release…*/*Delete for the team…* say "you will become the owner first". The domain-assignment form and the Team view's Domains list are gone.
- **Tests:**
  - Enumerated race (`core/race_test.go`): every order of the reassignment and the two concurrent releases. It runs over the real replay and merge, and ends with one chain; after one settling round, one plan and owner. Two team owners reassigning concurrently converge on one anchor.
  - Immediate handoff with the recipient offline and the owner paused, then handed back. Refusals for a non-member and a Personal file.
  - Reassignment without consent: one notice each, none repeated, and `not-team-owner` for a plain member.
  - Displaced edits land in the trash with a notice.
  - A domain document released, handed off and reassigned. A second document for the domain is refused, and concurrent ones are flagged.
  - Merge: reassigned twice, a gap below an anchor, and a dispute ended by a reassignment.
  - Teamlog: anchors, stale cut-off, plan checks on decode.
  - Existing tests moved off `assign-domain`, and helpers release after the first publish.
- **Left for later milestones:**
  - The settling round in the race test stands for replication offering a document again whose plan differs. Peers do not re-merge held files when only the log changes.
  - Error kinds beyond handoff/reassign in the CLI's `--json` belong with T6's parity work.
  - Waiting items for handoffs and disputes (D10) are T6.

**T4, transfers between teams.** Done 2026-11-15.

- **Team log** (`internal/teamlog`):
  - `accept-transfer {doc, owner, transfer}` and `decline-transfer {doc, transfer}`, built by `Signer.Accept` and `Signer.Decline`.
  - One accept counts per transfer, and per document, so a forged second offer with the same `kalka_id` cannot bring a second version 1.
  - A decline is discarded when an accept of the same transfer counts, whichever is visited first. An accept whose signer is concurrently removed falls (rule 2), and the decline counts again.
  - `State.Transfers` maps each transfer digest to its decision. An accept is also an anchor at cut-off 0 (`Assignment.Transfer`).
  - The harness generates accepts and declines of a few offers, among them a second offer for one document and owners who are not members. It checks: at most one accept per transfer and per document, no decline beside an accept, and the named owner is a member in the operation's view. 100 000 histories pass (48 021 "already decided" discards; 818 s).
- **Blueprint** (`transfer.go`):
  - The `transfer` entry, whose `to:` names the destination as well as its hash (`to:library-desk/<hash>`), so source members outside it can read the banner.
  - `HistoryDigest` and `EntryIDs`; the `transfer-withdrawn` entry (`re: <id>`, then `withdrawn`, `move declined`, `move withdrawn` or `move ended as a copy`).
  - `copied-from`, `moved-from` and `moved-to`; `Release.Moved` and `NewMoveTombstone`; `DeleteFrontmatter`.
- **Merge** (`merge/transfer.go`):
  - `Team.Accepted(id)` is new. `Attested` matches the listed entries against the signed history, trying the signed copy first. `Trusted` is the entry filter (signed by a team key, or attested), used by `Docs`, `Replica`, `describe` and the views.
  - Attested releases take no part in the chain.
  - `CheckOffer` makes the checks of spec 20.7, and also refuses any entry that is neither attested nor signed by a member. `Pending` merges two copies of an offer, and `CheckStub` checks a stub.
- **Core** (`core/transfer.go`):
  - `Copy` and `Move(path, team, takeOver)`. They refuse a non-member of the destination (`not-member`), a Personal file (`in-personal`), a dispute (`disputed`), a transfer already pending (`transfer-pending`), and a file that differs from its head release. A move by a team owner who is not the owner needs `takeOver`, which reassigns the document first.
  - The offer is written to `<team>/.kalka/incoming/<new id>.md`. For a move, the same entry, signed under the source id, records the start in the source.
  - Replication:
    - Pending offers and their stubs get inventory lines marked `pending` (`InvItem.Pending`) and travel as signed (`TeamDoc.Pending`) through `applyPending`. A pending copy gives way to the released document.
    - A refusal is reported with its reason, prefixed "transfer refused:".
  - Decisions:
    - `AcceptTransfer(id, owner)`, `DeclineTransfer` and `WithdrawTransfer`, with the kinds `unknown-transfer`, `transfer-decided` and `domain-exists`.
    - `afterTransfers` runs on every log change, after a pending merge, and at start. The owner of version 1 installs the document and releases it, serialized by `Node.installs`. Declined or withdrawn offers become stubs, the full offer going to `trash/<id>.offer.md`. A stub whose offer was accepted after all is restored.
  - `finishMoves` is derived from files. The source's open move entry has the same digest as the offer's, so the destination log's decision is looked up directly. An accept writes the `moved:` tombstone (`deleteLocked`, shared with `Delete`). If the actor no longer releases the source, the move ends as a copy, with a notice. A decline appends "move declined".
  - Notices (`transfer`): an offer waits for your team (to its owners); your transfer was accepted or declined; you were named owner; the move finished or ended as a copy.
  - Views: `Transfers()`, shown to the destination's owners and to the actor. `DocView.transfer` carries the source, the actor and the accepting owner, or `moving_to` for the source's banner. Entries carry `team_state`/`sig` `attested`.
  - `store.Info.From` and `Store.Resolve` let a seed's `parent` and a budded link find a copied or moved document.
- **API and CLI:**
  - `POST /api/transfers {path, team, mode, take_over}`, `GET /api/transfers`, `POST /api/transfers/{id}/accept|decline|withdraw`.
  - `kalka copy|move <file> --to <team>`, `kalka transfers`, `kalka transfer accept <id> [--owner] | decline <id> | withdraw <id>`, all with `--json` kinds.
- **Tests:**
  - `core/transfer_test.go`, over two teams with real sessions:
    - copy accepted (new id, `copied-from`, source bytes unchanged, accepting owner, handoff, history attested by actor and owner, held by a member outside the source team);
    - move accepted (banner, no tombstone before Ewa sees the accept, the `moved:` tombstone after it, gone for Marek, and the source id resolves);
    - declines of a copy (every peer stubs it, a stale offer cannot come back, a later accept is `transfer-decided`) and of a move (no tombstone, banner gone, a new transfer may start);
    - offer checks (an altered, removed or added attested entry, and a transfer signed by Marek's key, are refused on two peers with the reason; Marek's own copy is `not-member`);
    - an offer relayed while the actor is offline;
    - two accepts, accept against decline, and accept against withdrawal: one version 1 everywhere, the accept winning;
    - a move ending as a copy after a reassignment;
    - a move there and back;
    - a seed's parent after a move.
  - Teamlog: `TestTransferDecisions`, `TestAcceptFallsWithRemoval`. Blueprint: the transfer entry round trip and the move tombstone. Daemon: `TestTransferAPI`.
- **Left for later milestones:**
  - The page has no transfer dialogs or attested badge yet; that is T7 (`Transfer.tsx`), on the API above. `kalka transfers` starts the daemon, where spec 12.1 calls it a view; it also belongs with T6.
  - Waiting items for pending transfers (D10) are T6.
  - The acceptance check of a domain document (`domain-exists`) is made by the accepting owner only; a concurrent clash is flagged as for any two domain documents.

**T5, join links.** Done 2026-11-16.

- **Token and parsing** (`teamlog/invite.go`):
  - The token gains `inviter`, the inviting owner's email. A token without it is refused.
  - `Invite.Link()` is `kalka://join/<token>`.
  - `NormalizeJoin` and `ParseJoin` are the one parser for what a user gives. They accept:
    - a link or a bare token, in any letter case;
    - surrounding whitespace, and angle brackets;
    - line breaks inside;
    - one trailing slash.

    They refuse, with `invite-token`: another scheme or path, extra parts, a query, a fragment, more than 4 KiB, and a bad checksum.
- **Core** (`join.go`):
  - `PreviewInvite` reads the token alone, for the setup form.
  - `Node.JoinPreview` adds the checks in `joinProblem`: `own-invite`, `invite-email`, `already-member`, `invite-expired`, `rendezvous-mismatch`. It also reports whether the invite is already being redeemed here.
  - `Join` refuses with the same kinds. It used to warn about another rendezvous and go ahead; now `peerjs.SameServer` compares the two settings, so `peerjs-cloud` and its URL match.
  - `InviteResult.Link` is new, and `JoinView` gains `Inviter` in place of `Warning`.
  - New error kinds: `invite-token`, `invite-expired`, `invite-email`, `rendezvous-mismatch`.
- **API:**
  - `POST /api/joins/preview` works in setup mode as well, from the token alone.
  - `POST /api/ui/show` takes `{"route"}`. `daemon.ValidRoute` allows only `#/` followed by URL-safe characters.
  - `daemon.JoinRoute(link)` gives `#/join/<token>`, or `#/join/` when the link does not parse.
  - The client gains `JoinPreview` and `ShowRoute`.
- **CLI:**
  - `kalka join --show <link or token>` asks the daemon when one runs; with no daemon it reads the token alone and says the checks were not made.
  - `kalka join` takes a link and reports kinds with `--json`.
  - `kalka team invite` prints the link, then the token.
  - `kalka open [--print] <link>` opens the join screen.
- **`internal/urlscheme`:**
  - `Register`, `Unregister` and `Status`, over a `Target` that holds the OS, the executable, a `Registry`, the data directory and a `Runner`.
  - Windows: HKCU `Software\Classes\kalka`, written by `hkcu` (`registry_windows.go`).
  - Linux: `kalka.desktop` in `$XDG_DATA_HOME/applications`, then `xdg-mime default`.
  - macOS: nothing at run time; `PlistURLTypes` puts the scheme in the bundle.
  - `DesktopEntry` quotes the `Exec` path as the desktop-entry format requires.
- **Desktop shell:**
  - `StartArgs`: a single `kalka:` argument starts the shell on a route. A link start is never hidden or detached.
  - `Prepare(ctx, dir, hidden, route)`:
    - with a shell running, it hands the link over with `ShowRoute` and the start exits;
    - otherwise the plan carries the route into `openUI` (the setup form) and `connected`.
  - `Shell.Link` handles macOS's `ApplicationLaunchedWithUrl` event. Before the page has loaded, the route is queued.
  - `registerScheme` re-checks the registration at each start. `KALKA_NO_URL_SCHEME=1` skips it; the smoke test sets it.
- **Release:**
  - `Info.plist` carries `CFBundleURLTypes`.
  - The archive's `kalka.desktop` comes from `urlscheme.DesktopEntry` (with `%u` and `MimeType`). The Linux README says the shell rewrites the file at start.
  - The NSIS script writes the four registry values and the uninstaller deletes the key.
- **Web:**
  - New `Join.tsx`: the join screen (`#/join/<token>`) with the facts, then *Join*, a problem line, or "joining…" / "you joined".
  - `PasteBox` ("Join with link or token") opens the join screen and replaces the old direct-join form.
  - Setup fills in the email and the rendezvous from the invite (`setupFromInvite`), and focuses the folder.
  - The invite box shows the link, with *Copy link* and *Copy token*.
- **Spec and docs:**
  - Spec 19.7 parsing now names line breaks, the trailing slash and the fragment.
  - Spec 13 describes the preview's fields, the setup-mode preview, and `ui/show {"route"}`.
  - `docs/desktop-testing.md` has a join-link checklist, to run only on installed builds.
- **Tests:**
  - `TestParseJoin` and `TestInviteNeedsInviter`.
  - Core: `TestJoinPreview` (each problem, and that a preview never joins), `TestPreviewInvite`, and the updated `TestInviteRefusals`.
  - Daemon: `TestJoinPreviewAPI` and `TestSetupPreview`.
  - CLI: in `TestCLIClasses`, the invite's two lines, `open --print <link>` (also with a bad link), `join --show` as JSON and text, junk, and joining one's own invite. Also `TestJoinShowOffline`.
  - Desktop: `TestStartArgs`, `TestLinkHandOver` (a start with a link, hand-over to a running shell, a link sent to the shell, a bad link, routes refused) and `TestLinkAttach`.
  - `urlscheme`: Windows against a fake registry (including the re-check that writes nothing, and a moved install), Linux with a temp data directory and a recording runner, macOS, and the quoting.
  - Release: the NSIS lines, `plutil` reading the URL scheme (it reads the file and never opens a bundle), the plist text, and the `.desktop` text.
  - Web: the join route and the setup prefill.
  - No test registers a real handler.
- **Checked by hand:** two throwaway daemons on a local rendezvous; no shell was started.
  - The join screen for a usable invite showed the facts and *Join*.
  - *Join* went through to "You joined parcel-room".
  - Reopening the link showed "You joined".
  - A link with a query showed the refusal and the paste box.
  - The invite box showed the link.
- **Left open:**
  - The Windows and Linux registration is untested on real systems; that is the product owner's pass, and the checklist is in `docs/desktop-testing.md`.
  - On macOS the scheme is registered only once `Kalka.app` has been opened, which no test does.
  - `kalka join --show` is a view, but it is not yet in a parity table; that is T6.

### T6. Waiting items, the scoped API and the missing commands (done)

- **Waiting items** (`internal/core/waiting.go`, spec 11.3):
  - Four kinds: `transfer` (a pending transfer the local user can decide), `handed` (handed off, reassigned or accepted to you), `disputed` and `owner-gone` (both for team owners).
  - A handed item is cleared by any release of yours; the release your kalka makes on its own after a reassignment does not count. Its seen mark is the handoff release's id or the reassignment's operation, kept in `state.yaml` (`seen:`). A mark that no longer matches is dropped on the next write.
  - Opening a document (`GET /api/doc`), `POST /api/waiting/seen` and `kalka waiting --seen <file>` clear it.
  - `Summary.waiting` is `{total, oldest:{team, at, target}, teams:[{team, name, count}]}`. Joins and pushes are no longer counted as waiting. `GET /api/teams` carries each team's count.
- **Scope:** `?team=<name|hash|personal>` on `/api/blueprints`, `/api/seeds`, `/api/domains`, `/api/waiting` and `/api/transfers`. The filters are in core (`ResolveScope`, `SeedsIn`, `DomainsIn`, `BlueprintsIn`, `TransfersIn`), shared by the daemon and the views that run with no daemon.
- **Error kinds:**
  - `core.KindOf` gives every error a kind; untyped ones are `error`.
  - Team lookups return `no-team`, `unknown-team` and `ambiguous-team`. Removing yourself as the only owner returns `last-owner`. Pausing returns `paused`.
  - Join failures carry `invite-used`, `invite-expired`, `invite-email` or `invite-token`.
  - `not-set-up`, `not-running` and `no-shell` are constants in core too.
  - Spec 12.3 gained `error`, and its table sits between `error-kinds` markers.
- **CLI:**
  - The dispatch switch is a command table (`internal/cli/commands.go`), with a parity class for each command: workflow, view or process. `rendezvous` is process until T8 moves it out.
  - New commands: `teams`, `waiting [--team] [--seen <file>]`, `promote <file> [--status]`, `seed backlink <seed>` (a file command, as spec 12.1 says), `team uninvite <id>`, `team sync`, and `settings [--notifications on|off] [--keep-running on|off]`. With no option, `settings` shows the settings, from `state.yaml` when no daemon runs.
  - `--team` on `seeds`, `domains`, `transfers` and `waiting`.
  - `seeds` and `transfers` are views now: with no daemon they read the files, and their JSON is an object carrying `daemon`.
  - `team remove` takes `--dry-run`, as in the spec, in place of `--yes`.
- **Fixes found on the way:**
  - `team remove` printed success when the removal failed: a shadowed `err` in the CLI.
  - A reassignment whose cut-off release arrived after the log, in the same session, was never taken over until the next log change. Received documents now retry it.
  - A paused node said online until its connection loop noticed. `State` now reports it offline at once (spec 22.3).
- **Tray:** it shows "N waiting for you" from the total and opens the oldest item's team waiting list (`#/t/<team>/waiting`, the T7 route).
- **Web:** types for `WaitingItem`, `WaitingSummary` and the team count; `api.waiting`, `api.markSeen`, and the scope parameter on the lists; the `waiting` bus event. The Settings line uses the waiting total. The screens themselves are T7.
- **Tests:**
  - Core: `TestWaitingHanded` (a handoff, seen marks, a stale mark, your own release, a reassignment with the automatic release, a take-over), `TestWaitingDisputed`, `TestWaitingOwnerGone`, `TestWaitingTransfer` and `TestWaitingSummaryOrder` (three teams).
  - Daemon: `TestWaitingAPI` (scope, summary, teams count, opening clears, the `state.yaml` mark, repeat seen, scoped blueprints and domains).
  - CLI: `TestNewCommands`. It runs every new command with `--json`, views with no daemon starting none, and kinds `no-team`, `unknown-team`, `ambiguous-team`, `last-owner` and `error`.
  - CLI: `TestCommandTable`.
  - CLI: `TestErrorKinds`. Every declared kind is in spec 12.3, and every listed kind is declared and returned somewhere. It found `invite-used`, which was returned only by string equality.
  - Desktop: the tray cases for the total and the oldest team.
- **Left for T7:** `parity.json` with its Go and web tests, `actions.ts`, and the switcher, waiting list and tray route in the page.

### T7. The app and parity (done)

- **Parity (spec 12.2):**
  - `internal/cli/parity.json` has one row per command line: a workflow names its action id, a view its place in the UI, and a process command why it is exempt.
  - `web/src/actions.ts` declares the action ids. A control carries one with `{...act('id')}`, which renders `data-action`.
  - Go: `TestParityTable` checks that every command has a row, rows name only commands in the table, and each command's class matches its rows. `TestParityJSONFlag` runs every non-process line with `--json --help` in process and checks its own flags list `-json`. The general usage text also mentions `--json`, so that text fails the test.
  - Web: `parity.test.ts` checks the table, `actions.ts` and the controls are one set. It caught `Settings.tsx`, whose double quotes the first regex missed.
- **The app:**
  - `Switcher.tsx`: the wordmark and current scope at the top left. The menu lists Personal, then teams by name with waiting badges (a short hash when names clash), the pending joins, *Create team…* and *Join with link or token…*.
  - Routes carry the scope: `#/t/<team or personal>/[seeds|team|domains|waiting]`. The scope is remembered in the page's storage, and opening a document switches to its scope. With nothing remembered, the first team by name is shown, else Personal. Personal hides Team, Domains and Waiting.
  - `Waiting.tsx`: the team's items, oldest first, with *Accept* (optionally naming the owner), *Decline*, *Open* and *Reassign…*. The nav shows the team's count.
  - `Transfer.tsx`: *Copy to team…* and *Move to team…* in the review's owner actions (a team owner's move takes the document over first), the "being moved" banner, the attested strip, and the Team view's transfers with *Accept*, *Decline* and *Withdraw*.
  - Review: an "attested" badge on entries carried by a transfer.
  - Blueprints: *Publish to team…* per file in Personal; "not shared" on team files that do not replicate.
  - Team: the selected team only, with member presence, *Cancel* on invites, *Sync now*, *Make owner* and *Remove as owner*. The create and join forms moved to the switcher.
  - Settings: *Blueprint reference: Save a copy* (a download) and *Copy*, and the version line.
- **Go:**
  - `MemberView.Presence`, from the daemon's probes.
  - `GET /api/about` (from `daemon.Options.About`, set by the CLI) and `GET /api/reference`. The page never writes the reference into the folder, where it would be indexed as a blueprint.
  - Spec 12.2 and 13 updated.
- **Fixtures rule:** `TestFixturesStayInNeutralDomains` reads the string literals of every Go test file (not imports), the web tests and `testdata`, and fails on terms of the excluded domains. Library "loans" are allowed. It caught "stock-server" in a rendezvous test, now reworded.
- **Tests:**
  - Web: `scope.test.ts` covers routes with a scope and the tray's route, the scope choice, the switcher's order, badges and clashing names, the waiting rows, the join screen's four states, and the attested text.
  - Daemon: `TestAppWorkflows` walks presence, *Sync now*, publish from Personal, handoff, seed and promote, settings, about and the reference through the page's endpoints.
- **Checked in a browser** against a throwaway daemon: the switcher, Personal, the waiting list, accepting a copy from the Team view, the attested strip and Settings.
- **Not done:**
  - Open invites offer *Copy link* and *Copy token* only right after the invite is made. The daemon does not keep tokens of earlier invites.
  - Team history shows `accept-transfer` and other new operations by their raw names.
  - Reassigning from a member's row in the Team view is not built. Reassign is in Review and Waiting.

### T8. Rendezvous binary, packaging and documents (done)

- **`cmd/kalka-rendezvous`:** the server from `internal/cli/rendezvous.go`, moved with its tests (flags, HTTPS signaling, TURN over TLS).
  - It has its own exit codes, `version`, and `name.RendezvousBinary` in its messages.
  - It needs no config directory, identity or folder.
  - `TestServerImports` walks every module package the server reaches and refuses the core, the daemon, the UI, the CLI, the desktop shell, the transport and `pion/webrtc`. `go version -m` on the built binary lists no WebRTC.
- **`kalka rendezvous`:** gone from the command table and `parity.json`, so every `kalka` command is now a user's command. It answers "moved: run kalka-rendezvous" with exit 2 (`TestRendezvousMoved`).
- **Release (`tools/release`):**
  - Every target gets `kalka-rendezvous_<v>_<os>_<arch>.tar.gz` (`.zip` on Windows), holding the binary and a README. It is signed on Windows like the rest, and its version is set through `main.version`.
  - A `build` hook on the release lets a test stand in for `go build`. `TestReleaseArtifacts` runs a whole linux/amd64 release with placeholder binaries. It checks that both archives are written, the server's holds only its binary and README, the headless archive holds no server, and `SHA256SUMS` covers every file in the output and nothing else.
- **Makefile:** `make rendezvous`; `clean` and `.gitignore` know the binary.
- **Documents:**
  - `docs/rendezvous.md`: the binary and its archive, one key-derived id (the email-derived id went in T1), the team log in place of pinned keys, a systemd unit.
  - `docs/headless.md`: the run classes (views start nothing, file commands need nothing, `--json` everywhere, `kalka waiting --json` for agents); the stale terminal prompts are gone.
  - `docs/install.md`: the server archive, which is not for users.
  - `docs/m1-transport.md`: a note that the server is now `kalka-rendezvous`.
  - `docs/desktop-testing.md`: rewritten as the T9 script, one ordered pass for two installations. It covers install and the URL key, setup and the first team, invite and join by link in each starting state, publish, release, handoff and the waiting list, reassignment, copy and move with accept, decline and withdraw, tray and settings, close, login and headless, the Windows-only items and uninstall, plus what differs on macOS and Linux.
  - `internal/reference/BLUEPRINT-REFERENCE.md` names no removed command; unchanged.
  - The spec already described the split (v0.6, 9.6, 15, 16, 23).
- **Left for T9:** the product owner's Windows pass with `docs/desktop-testing.md`.

### T9a. Gaps before the Windows pass (done)

- **Open invites can be passed on again.** `InviteView` carries `token` and `link` while an invite is open (never once it is used, expired or cancelled). The daemon always kept them in the owner-only `invites.yaml`; only the view left them out. The Team view's invite list has *Show link*, which reopens the box with *Copy link* and *Copy token* (spec 11, Team). `kalka team invites` prints the link of each open invite under the table.
- **History in words for every operation.** `accept-transfer` and `decline-transfer` read "accepted "<title>" from another team, owned by …" and "declined "<title>", offered from another team". Those two and `reassign-document` name the document by its title when it is held in the team's folder, otherwise by a short id.
- **Reassign from a member's row.** For team owners, *Reassign…* in the Members table lists that member's published documents in the team, each with a new owner to choose (you by default) and *Reassign*. It uses the same list as the removal preview.
- **Domains in the removal and reassign lists.** `Reassigned.domain` is set for a domain document, and the page and `kalka team remove` show it.
- **Watcher: a folder that arrives with files inside.** A new folder was scanned before it was watched, and only the folder was announced. Files in a copied or unpacked folder were indexed, but the page and replication heard of them only at the next unrelated event, and a file written between the scan and the watch was missed. The watcher now watches first, then scans, and announces each file. `TestWatchFolderMovedIn` fails on the old code. This was found as a flake of `TestWatchSuppressesOwnWrites` under full-suite load.
- **Tests:** `TestInviteFlow` (token and link only while open), `historySays` in the copy, decline and reassignment tests, the domain in `TestDomainOwnership`'s removal preview. Checked against two throwaway daemons through the page's endpoints (the Chrome extension was not connected, so the page itself was not looked at).

### T9b. Sample blueprints (done)

- **`samples/blueprints`:** five blueprints for an invented library's lending desk, for the manual pass and demos. They hold no `kalka_id`, owner or review log, so they are published and reviewed under the tester's own identity.
  - A domain document (`circulation`): glossary, contexts, invariants, events.
  - A full opted-in blueprint: every section, a Mermaid diagram, and a trace block with a divergence.
  - A short plan that shows three hints.
  - A file with no frontmatter.
  - A plan for a second team (`returns-desk`), for transfers.
- **`samples/README.md`:** what each file exercises, how to copy the folder into Personal (never point kalka at it), and a short walk-through: publish the domain (v1), the blueprints resolving `lending-desk/circulation@v1`, release v2 for the "newer domain version" indicator, a second team for transfers.
- **Tests:** `TestSamples` runs `kalka check --json` on each file and requires no error or warning, valid trace blocks and exactly the hints the README lists. It also fails on a sample the README does not list. `TestFixturesStayInNeutralDomains` now covers `samples/`; it caught "checkout" in the glossary.
- **`docs/desktop-testing.md`:** section 4 copies the folder in in one go (which also exercises T9a's watcher fix) and walks the samples. It adds checks for the T9a items: *Show link* on an older invite, *Reassign…* from a member's row, and the history wording for transfers.
- Walked through with a throwaway daemon: the folder copied into Personal, the domain published as v1 and listed in *Domains*, and both blueprints' `domain_ref` resolved to it.

### T9c. Two bugs found by flaky tests (done)

- **A push during a running round waited up to 10 minutes.** A push kick that found the team's round still running returned. The push flag stayed set, but nothing started another round before the periodic one (`ReplicateEvery`, 10 minutes). Saving an edit and releasing a moment later hit it: the release travelled only at the next periodic round. `endRound` now kicks the team again when a push was asked for meanwhile. `TestPushDuringRound` stands in for a running round and fails on the old code. Found as a one-in-six failure of the daemon's `TestSyncWithNoWindow`.
- **The key id check moved ahead of `auth` (spec 8.3 step 4).** It needs only the `hello`, so a squatter fails before this side signs anything for it. The result also no longer depends on reading the peer's last message. CI caught `TestKeyIDSquatter` failing on macOS and Linux: the squatter closed the connection right after its handshake, and the dialer's read of its `auth` was aborted. The spec says when the check is made.
- **Seen once, not fixed:** `TestTURNOverTCPRelayOnly` timed out on a Linux runner (a 20 s handshake deadline over TURN over TCP). It passed on every other run. Watch for it.

## 7. Sequencing and rough size

| Milestone | Depends on | Size (one experienced engineer) |
|---|---|---|
| M0 Format and merge | — | 2 weeks |
| M1 Transport spike | — (parallel with M0) | 2 weeks, time-boxed |
| M2 Daemon and e2e sync | M0, M1 | 3 weeks |
| M3 Web UI, linear | M2 | 3 weeks |
| M4 Plugins | M0 (library), M3 (rendering) | 1.5 weeks |
| M5 Rendezvous | M1 | 1.5 weeks (can run alongside M3/M4) |
| M7 Trace | M0, M3, M4 | 3 weeks |
| M6 Canvas | M3 | 1.5 weeks |

Release work in every milestone from M2 on: cross-compiled archives for `windows/amd64` (first), `darwin/arm64`, `linux/amd64`, each containing `kalka` and `kalka-plugin-gh`; CI runs the test suite on Windows as well as Linux, because path handling, file locking and line endings are tested there first (§2.5).
