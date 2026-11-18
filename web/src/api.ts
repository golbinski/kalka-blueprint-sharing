// Client for the daemon's local API (spec 13). The browser holds the token
// as an HttpOnly cookie, so requests only need same-origin credentials.

export interface Status {
  setup?: boolean
  defaults?: { folder: string; rendezvous: string }
  email: string
  /** The key id this node registers (spec 9.2). */
  peer_id: string
  fingerprint?: string
  rendezvous: string
  folder: string
  online: boolean
  paused?: boolean
  error?: string
  notice?: string
}

/** ?team= of a list (spec 13): '' is everything, 'personal' is Personal. */
export function scope(team: string): string {
  return team ? '?team=' + encodeURIComponent(team) : ''
}

// Summary is the one-line state (spec 22.1).
export interface Summary {
  online: boolean
  paused: boolean
  error?: string
  peers_online: number
  peers_known: number
  last_sync?: string
  /** What waits for the user over every team (spec 11.3). */
  waiting: WaitingSummary
  /** Joins in progress and queued pushes: not waiting items. */
  joins: number
  pushes: number
  shell?: 'desktop'
}

export interface WaitingSummary {
  total: number
  /** The oldest item; target is its team's waiting list. */
  oldest?: { team: string; at: string; target: string }
  teams: { team: string; name: string; count: number }[]
}

// WaitingItem is one waiting item (spec 11.3).
export interface WaitingItem {
  kind: 'transfer' | 'handed' | 'disputed' | 'owner-gone'
  team: string
  team_name: string
  at: string
  /** The page route: the document, or the team's waiting list. */
  target: string
  title: string
  path?: string
  kalka_id?: string
  transfer?: string
  by?: string
  /** A dispute: the members whose releases compete. */
  candidates?: string[]
  /** A transfer: copy or move, and the team it comes from. */
  mode?: 'copy' | 'move'
  from?: string
  message: string
}

/** A team blueprint with comments the user has not seen (GET /api/unread). */
export interface Unread {
  path: string
  rel: string
  kalka_id: string
  title: string
  team: string
  owner?: string
  mine?: boolean
  comments: number
  deviations: number
  authors: string[]
  /** Someone else's blueprint that arrived and was never opened here. */
  never?: boolean
  since?: string
}

// Settings are the daemon's own settings (spec 22.2).
export interface Settings {
  paused?: boolean
  notifications?: boolean
  keep_running_on_close?: boolean
  start_at_login?: boolean
  start_at_login_available?: boolean
  start_at_login_error?: string
}

// Notice is a notification the daemon decided on (spec 22.4).
export interface Notice {
  kind: string
  title: string
  message: string
  path?: string
  team?: string
}

export interface BlueprintInfo {
  path: string
  rel: string
  kalka_id?: string
  title?: string
  owner?: string
  rev?: string
  entries: number
  errors: number
  parse_error?: string
  duplicate?: boolean
  modified: string
  /** Comments and deviations by others not seen yet. */
  new?: number
  /** The newest entry's time, or the file's. */
  activity?: string
  /** The team's released version, and whether this copy has edits not released. */
  version?: number
  unreleased?: boolean
}

export interface Heading {
  level: number
  text: string
  slug: string
  line: number
  nested?: boolean
}

export interface Section {
  heading?: Heading
  slugs: string[]
  markdown: string
  line: number
}

export interface Entry {
  id: string
  kind: string
  author: string
  timestamp: string
  anchor: string
  section?: string
  node?: string
  rev: string
  reply_to?: string
  // ext are header fields a later kalka added, as written.
  ext?: string[]
  body: string
  outdated?: boolean
  orphaned?: boolean
  valid: boolean
  /** Signature state (spec 6.5). */
  sig?: 'unsigned' | 'invalid' | 'unknown' | 'verified' | 'attested'
  budded_seed?: string
  budded_title?: string
  budded_rel?: string
  unparsed?: string
  /** For a published document: signed by a member, a removed member, or only in this file (spec 11.1). */
  team_state?: 'member' | 'removed-member' | 'local-only' | 'attested'
  /** The domain version whose release took this comment up (spec 6.12). */
  incorporated_in?: number
  /** A release entry as the daemon read it (spec 20.4). */
  release?: { version: number; plan: string; owner: string; deleted?: boolean; incorporates?: number; note?: string }
}

/** A diagnostic (spec 6.17), as kalka check reports it. */
export interface Problem {
  rule: string
  severity: 'info' | 'warning' | 'error'
  line?: number
  column?: number
  end_line?: number
  end_column?: number
  message: string
  node?: string
  /** A structural error: the file cannot be published. */
  blocks?: boolean
  fix?: { text: string; line: number; column: number; end_line: number; end_column: number }
}

/** A rule of the registry (spec 6.17). */
export interface Rule {
  id: string
  severity: 'info' | 'warning' | 'error'
  title: string
  meaning: string
  why: string
  wrong?: string
  right?: string
  blocks?: boolean
}

export type Role = 'owner' | 'reviewer' | 'local'

export interface TraceEdge {
  to: string
  label?: string
}

export interface TraceNode {
  id: string
  lane: string
  text: string
  next?: TraceEdge[]
  back?: TraceEdge[]
  realizes?: string[]
  ref?: string
  why?: string
  change?: 'new' | 'modified' | 'removed'
  /** Invariants the step keeps true: an id, or <domain>#<id> (spec 6.15). */
  guards?: string[]
  line: number
}

/** One entry of a domain glossary (spec 6.16). */
export interface Term {
  term: string
  definition: string
  aliases?: string[]
  forms?: string[]
  context?: string
  see?: string[]
  line: number
}

/** The glossary a document is linked against. */
export interface Glossary {
  /** The domain version, such as parcels/delivery@v3. */
  ref?: string
  state: 'held' | 'not-kept' | 'none'
  terms: Term[]
}

/** One row of the invariants matrix (spec 6.15, 12.4). */
export interface InvariantRow {
  /** The id, or <domain>#<id> for a domain invariant. */
  ref: string
  id: string
  domain?: string
  statement?: string
  context?: string
  why?: string
  verified_by: string[]
  status?: 'new' | 'changed' | 'removed'
  /** The heading it is under; its comment anchor is section#id. */
  section?: string
  line: number
  resolved: boolean
  guarded_by: { trace: number; node: string; section?: string }[]
  gaps: ('unguarded' | 'unverified')[]
  /** For a domain document: the held blueprints naming it. */
  referenced_by?: string[]
}

/** One check of kalka doctor (spec 12.5). */
export interface DoctorResult {
  check: string
  state: 'ok' | 'warn' | 'fail' | 'skip'
  detail: string
  fix?: string
}

export interface DoctorReport {
  results: DoctorResult[]
  summary: { ok: number; warn: number; fail: number }
}

/** A held blueprint a domain release would concern. */
export interface Affected {
  path: string
  title: string
  problems: Problem[]
}

export type DivClass =
  | 'implementation-only'
  | 'unrealized'
  | 'merged'
  | 'expanded'
  | 'scattered'
  | 'reordered'
  | 'skipped'
  | 'unverified'
  | 'stale'

export interface Divergence {
  class: DivClass
  node: string
  nodes?: string[]
  lane?: string
  detail?: string
}

// TraceProblem is a validation error or warning (T001…).
export interface TraceProblem {
  code: string
  node?: string
  line: number
  message: string
}

export interface Trace {
  index: number
  line: number
  section?: string
  title?: string
  nested?: boolean
  source: string
  lanes?: string[]
  nodes?: TraceNode[]
  // version is the language version the fence names; a block with a
  // version this kalka does not know is shown as code (T005).
  version: number
  errors?: TraceProblem[]
  warnings?: TraceProblem[]
  divergences: Divergence[]
  // graph is how many of divergences come from the graph alone.
  graph: number
}

export interface RefView {
  ref: string
  scheme?: string
  payload?: string
  plugin?: string
  pending?: boolean
  ok: boolean
  error?: string
  title?: string
  url?: string
  status?: string
  snippet?: { lang?: string; start_line?: number; text: string }
  warnings?: { text: string }[]
}

export interface Doc {
  path: string
  rel: string
  kalka_id?: string
  title: string
  owner?: string
  role: Role
  self: string
  rev: string
  frontmatter?: string
  sections: Section[]
  headings: Heading[]
  entries: Entry[]
  log_lead?: string
  /** Diagnostics (spec 6.17), hints (H…) included. */
  problems: Problem[]
  invariants: InvariantRow[]
  glossary: Glossary
  duplicate?: boolean
  /** "personal" or the name of the team whose folder holds the file. */
  scope: string
  mismatch?: string
  traces: Trace[]
  refs: Record<string, RefView>
  seed?: { parent?: string; parent_rel?: string; parent_title?: string; backlink_missing?: boolean }
  team?: TeamDocState
  domain?: DomainInfo
  domain_ref?: DomainRefView
  /** A document that came by a copy or a move, or is being moved away (spec 20.7). */
  transfer?: TransferInfo
}

export interface TransferInfo {
  mode?: 'copy' | 'move'
  /** <team>/<kalka_id> of the source. */
  from?: string
  /** Who offered it and who accepted it: the two who attest its history. */
  actor?: string
  by?: string
  moving_to?: string
}

/** A pending or decided transfer into a team (spec 20.7). */
export interface TransferView {
  id: string
  team: string
  mode: 'copy' | 'move'
  from: string
  title: string
  actor: string
  at: string
  status: 'pending' | 'accepted' | 'withdrawn'
  mine?: boolean
  can_decide?: boolean
  owner?: string
  path?: string
  /** The attested entries the offer brings. */
  entries: number
}

/** The version line of Settings (GET /api/about). */
export interface About {
  version: string
  wire_protocol: number
  plugin_protocol: number
  reference: string
}

/** A domain document in the document view (spec 6.12). */
export interface DomainInfo {
  name: string
  team: string
  version: number
  versions: number[]
  viewing?: number
  owner?: string
}
/** A blueprint's `kalka_domain:` reference (spec 6.12). */
export interface DomainRefView {
  ref: string
  team: string
  domain: string
  version: number
  latest?: number
  rel?: string
  held: boolean
  missing?: boolean
  ambiguous?: boolean
}
export interface DomainRow {
  team: string
  domain: string
  title: string
  path: string
  rel: string
  version: number
  owner?: string
  you?: boolean
  unreleased?: boolean
  open: number
  versions: number[]
}

/** What the document view shows about a published document (spec 11.1, 19.5). */
export interface TeamDocState {
  team: string
  held: boolean
  version: number
  releaser?: string
  can_act?: boolean
  unreleased?: boolean
  disputed?: boolean
  reassigned_to?: string
  owner_not_member?: boolean
  local_only?: number
  void_releases?: number
  /** You are a team owner: you may reassign, or take the document over (spec 7.4). */
  team_owner?: boolean
  domain?: string
  /** Another published document of the team claims the same domain. */
  domain_clash?: string
}
export interface ReassignResult {
  path: string
  team: string
  from: string
  owner: string
  after: number
  version?: number
  /** Set when no other member was reached before signing. */
  warning?: string
}
export interface PublishResult {
  path: string
  team: string
  version: number
  local_only: number
}
export interface Refusal {
  kalka_id: string
  title?: string
  from: string
  reason: string
  at: string
}
export interface ReplicationStatus {
  last_round?: string
  reached: string[] | null
  tried: number
  error?: string
  refused: Refusal[] | null
  received: number
}

export interface SeedRow {
  rel: string
  kalka_id: string
  title: string
  owner: string
  mine: boolean
  parent?: string
  parent_rel?: string
  parent_title?: string
  created?: string
  active?: string
}

export interface BudResult {
  seed: string
  seed_id: string
  entry?: string
  backlink_error?: string
}

export class ApiError extends Error {
  status: number
  kind?: string
  constructor(message: string, status: number, kind?: string) {
    super(message)
    this.status = status
    this.kind = kind
  }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  let r: Response
  try {
    r = await fetch(path, {
      method,
      credentials: 'same-origin',
      headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError('kalka is not reachable. Is it still running?', 0, 'down')
  }
  const text = await r.text()
  let data: unknown
  try {
    data = text ? JSON.parse(text) : undefined
  } catch {
    data = undefined
  }
  if (!r.ok) {
    const e = data as { error?: string; kind?: string } | undefined
    throw new ApiError(e?.error ?? (text || r.statusText), r.status, e?.kind)
  }
  return data as T
}

const enc = encodeURIComponent

/** GET /api/reference downloads BLUEPRINT-REFERENCE.md. */
export const referenceURL = '/api/reference'

// openExternal asks the daemon to open an http(s) address in the system
// browser (the desktop window, spec 22).
export function openExternal(url: string): Promise<object> {
  return call<object>('POST', '/api/open-external', { url })
}

// Teams (spec 19).
export interface TeamMember {
  email: string
  owner?: boolean
  fingerprint: string
  since?: string
  you?: boolean
  /** What this kalka last saw of the member (spec 9.3); absent for you. */
  presence?: 'online' | 'offline' | 'unknown'
  /** When this kalka last found the member online, since it started. */
  last_seen?: string
}
export interface TeamInvite {
  id: string
  email: string
  expires: string
  state: 'open' | 'used' | 'expired' | 'canceled'
  used_at?: string
  /** Only while open: the invite can be passed on again. */
  token?: string
  link?: string
}
export interface TeamHistory {
  hash: string
  at: string
  signer: string
  op: string
  text: string
  effective: boolean
  reason?: string
}
export interface Team {
  team: string
  name: string
  dir: string
  log: string
  me: 'owner' | 'member' | 'none'
  error?: string
  /** After a removal or demotion: no other member was reached first. */
  warning?: string
  /** The team's waiting items (spec 11.3). */
  waiting: number
  members: TeamMember[]
  owners: string[]
  invites: TeamInvite[]
  history: TeamHistory[]
  bad_lines?: { line: number; error: string }[]
  rejected?: number
  pending?: number
  replication?: ReplicationStatus
  published: number
  deleted: { kalka_id: string; title: string; version: number; by: string }[] | null
  /** Files whose location and team: disagree: listed, never replicated (spec 14). */
  not_shared: { rel: string; title: string; kalka_id?: string; reason: string }[] | null
}
export interface InviteResult {
  token: string
  /** kalka://join/<token> */
  link: string
  id: string
  team: string
  name: string
  email: string
  expires: string
}
export interface Join {
  id: string
  team: string
  name: string
  owner?: string
  created: string
  expires: string
  state: 'pending' | 'joined' | 'failed'
  detail?: string
  /** The error kind of a failed join. */
  kind?: string
  tries: number
  last_try?: string
  inviter?: string
}
/** What the join screen shows (spec 11.1, 19.7). The team name and the
 * inviter are claims until the join completes: render them as text. */
export interface JoinPreview {
  token: string
  team: string
  name: string
  inviter: string
  email: string
  expires: string
  rendezvous: string
  /** What stops the join; absent when Join can go ahead. */
  problem?: 'invite-email' | 'invite-expired' | 'already-member' | 'own-invite' | 'rendezvous-mismatch'
  message?: string
  joining?: Join
}
export interface Reassigned {
  kalka_id: string
  title: string
  path: string
  after: number
  /** Set for a domain document: the domain moves with the document. */
  domain?: string
}

export const api = {
  summary: () => call<Summary>('GET', '/api/summary'),
  settings: () => call<Settings>('GET', '/api/settings'),
  applySettings: (s: Settings) => call<Settings>('PUT', '/api/settings', s),
  setPaused: (paused: boolean) => call<Settings>('POST', paused ? '/api/pause' : '/api/resume', {}),
  teams: () => call<Team[] | null>('GET', '/api/teams').then((l) => l ?? []),
  waiting: (team = '') =>
    call<WaitingItem[] | null>('GET', '/api/waiting' + scope(team)).then((l) => l ?? []),
  markSeen: (path: string) => call<{ cleared: boolean }>('POST', '/api/waiting/seen', { path }),
  createTeam: (name: string) => call<Team>('POST', '/api/teams', { name }),
  invite: (team: string, email: string, expires: string) => call<InviteResult>('POST', `/api/teams/${enc(team)}/invites`, { email, expires }),
  cancelInvite: (id: string) => call<object>('DELETE', '/api/invites/' + enc(id)),
  removalPreview: (team: string, email: string) =>
    call<Reassigned[] | null>('GET', `/api/teams/${enc(team)}/removal?email=${enc(email)}`).then((l) => l ?? []),
  removeMember: (team: string, email: string) => call<Team>('POST', `/api/teams/${enc(team)}/remove`, { email }),
  setOwner: (team: string, email: string, owner: boolean) => call<Team>('POST', `/api/teams/${enc(team)}/owners`, { email, owner }),
  publish: (path: string, note = '', team = '') => call<PublishResult>('POST', '/api/publish', { path, team, note }),
  release: (path: string, note: string, incorporates: string[], takeOver = false) =>
    call<PublishResult>('POST', '/api/release', { path, note, incorporates, take_over: takeOver }),
  handoff: (path: string, email: string) => call<PublishResult>('POST', '/api/handoff', { path, email }),
  reassign: (path: string, email: string) => call<ReassignResult>('POST', '/api/reassign', { path, email }),
  domains: (team = '') => call<DomainRow[] | null>('GET', '/api/domains' + scope(team)).then((l) => l ?? []),
  deleteForTeam: (path: string, takeOver = false) => call<object>('POST', '/api/delete', { path, take_over: takeOver }),
  replicate: (team: string) => call<ReplicationStatus>('POST', `/api/teams/${enc(team)}/replicate`, {}),
  joins: () => call<Join[] | null>('GET', '/api/joins').then((l) => l ?? []),
  forgetJoin: (id: string) => call<object>('DELETE', '/api/joins/' + enc(id)),
  unread: () => call<Unread[] | null>('GET', '/api/unread').then((l) => l ?? []),
  markRead: (path: string) => call<object>('POST', '/api/read', { path }),
  /** The system's folder dialog; '' when canceled. */
  pickFolder: (start: string) => call<{ folder: string }>('POST', '/api/pick-folder', { start }).then((r) => r.folder),
  openFolder: (rel: string) => call<unknown>('POST', '/api/open-folder', { path: rel }),
  join: (token: string) => call<Join>('POST', '/api/joins', { token }),
  joinPreview: (token: string) => call<JoinPreview>('POST', '/api/joins/preview', { token }),
  seeds: (team = '') => call<SeedRow[] | null>('GET', '/api/seeds' + scope(team)).then((l) => l ?? []),
  bud: (r: { path: string; from?: string; title: string; problem?: string; why?: string }) => call<BudResult>('POST', '/api/seeds', r),
  backlink: (seed: string) => call<{ entry: string }>('POST', '/api/backlink', { seed }),
  promote: (path: string, status = 'draft') => call<{ ok: boolean }>('POST', '/api/promote', { path, status }),
  transfers: (team = '') => call<TransferView[] | null>('GET', '/api/transfers' + scope(team)).then((l) => l ?? []),
  startTransfer: (path: string, team: string, mode: 'copy' | 'move', takeOver = false) =>
    call<{ id: string; team: string }>('POST', '/api/transfers', { path, team, mode, take_over: takeOver }),
  decideTransfer: (id: string, action: 'accept' | 'decline' | 'withdraw', owner = '') =>
    call<TransferView | { id: string }>('POST', `/api/transfers/${enc(id)}/${action}`, { owner }),
  about: () => call<About>('GET', '/api/about'),
  rules: () => call<Rule[]>('GET', '/api/rules'),
  doctor: () => call<DoctorReport>('GET', '/api/doctor'),
  samples: () => call<{ written: string[] }>('POST', '/api/samples'),
  domainImpact: (path: string) => call<Affected[]>('GET', '/api/domain-impact?path=' + enc(path)),
  /** The blueprint reference as text (spec 6.13). */
  reference: async () => {
    const r = await fetch(referenceURL, { credentials: 'same-origin' })
    if (!r.ok) throw new ApiError(r.statusText, r.status)
    return r.text()
  },
  status: () => call<Status>('GET', '/api/status'),
  setRendezvous: (rendezvous: string, force = false) => call<Status>('POST', '/api/rendezvous', { rendezvous, force }),
  setup: (c: { email: string; rendezvous: string; folder: string }) => call<{ saved: boolean }>('POST', '/api/setup', c),
  blueprints: (team = '') => call<BlueprintInfo[] | null>('GET', '/api/blueprints' + scope(team)).then((l) => l ?? []),
  doc: (path: string, version?: number) =>
    call<Doc>('GET', '/api/doc?path=' + enc(path) + (version ? '&v=' + version : '')).then((d) => ({
      ...d,
      traces: d.traces ?? [],
      refs: d.refs ?? {},
      invariants: d.invariants ?? [],
      glossary: d.glossary ?? { state: 'none', terms: [] },
    })),
  addEntry: (e: { path: string; kind: string; anchor: string; re?: string; quote?: string; body: string }) =>
    call<{ id: string }>('POST', '/api/entries', e),
}

export type BusType = 'folder' | 'status' | 'refs' | 'teams' | 'waiting' | 'notice' | 'settings'

// events subscribes to the daemon's live updates. EventSource reconnects
// by itself after a restart of the daemon.
export function events(on: (type: BusType, data: unknown) => void, onState?: (open: boolean) => void): () => void {
  const es = new EventSource('/api/events')
  const types: BusType[] = ['folder', 'status', 'refs', 'teams', 'waiting', 'notice', 'settings']
  for (const t of types) {
    es.addEventListener(t, (ev) => {
      let data: unknown
      try {
        data = JSON.parse((ev as MessageEvent).data)
      } catch {
        data = undefined
      }
      on(t, data)
    })
  }
  es.onopen = () => onState?.(true)
  es.onerror = () => onState?.(false)
  return () => es.close()
}
