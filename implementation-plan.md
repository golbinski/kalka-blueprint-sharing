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
cmd/kalka/                 main: daemon, CLI subcommands, rendezvous mode
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
internal/daemon/           wiring, per-blueprint locks, presence prober, prompts
internal/httpapi/          localhost API, token, Host/Origin checks, SSE (§13)
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
- **Canvas and Trace tabs** are shown disabled until M6 and M7. The mockups' Review log tab is left out (Q5).
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
