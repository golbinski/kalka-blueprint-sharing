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
| A8 | **Prompts are daemon objects** (`pending prompt`, with id, kind, timeout) pushed to the UI over SSE, answered by POST. If no UI tab is connected when a prompt arrives, the daemon opens the browser. | Responder-side prompts (accept, verify, handoff) can arrive while the UI is closed. *Open question Q3.* |
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
- **Q3** Responder side with no UI open: open the browser automatically, show an OS notification, or reject with `declined` after the timeout?
- **Q4** Move Canvas review (M6) after Trace (M7)? The trace view has mockups and a clearer purpose; the canvas view repeats the linear view's data.
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

Done 2026-11-12, findings in `docs/m1-transport.md`. Stock `peerjs-server` sync passes; MITM detection passes; TURN/TCP relay-only passes in-process; `peerjs-go` not adopted. **The public PeerJS cloud registers clients but does not relay messages** (any client, any id format): `peerjs-cloud` mode is broken today — product decision pending (Q6). Still open: a run on a real UDP-blocked network, TURN over TLS.

- **Q6** Drop `rendezvous: peerjs-cloud` and use `kalka rendezvous` for demos (recommended), or keep it labelled unreliable?

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
