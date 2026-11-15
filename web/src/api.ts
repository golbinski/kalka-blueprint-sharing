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

// Summary is the one-line state (spec 22.1).
export interface Summary {
  online: boolean
  paused: boolean
  error?: string
  peers_online: number
  peers_known: number
  last_sync?: string
  joins: number
  pushes: number
  shell?: 'desktop'
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
  body: string
  outdated?: boolean
  orphaned?: boolean
  valid: boolean
  /** Signature state (spec 6.5). */
  sig?: 'unsigned' | 'invalid' | 'unknown' | 'verified'
  budded_seed?: string
  budded_title?: string
  budded_rel?: string
  unparsed?: string
  /** For a published document: signed by a member, a removed member, or only in this file (spec 11.1). */
  team_state?: 'member' | 'removed-member' | 'local-only'
  /** The domain version whose release took this comment up (spec 6.12). */
  incorporated_in?: number
}

export interface Problem {
  severity: 'info' | 'warning' | 'error'
  code: string
  line?: number
  message: string
}

export type Role = 'owner' | 'reviewer' | 'local'

export interface TraceNext {
  to: string
  label?: string
}

export interface TraceNode {
  id: string
  lane: string
  text: string
  next?: TraceNext[]
  realizes?: string[]
  ref?: string
  why?: string
  line: number
}

export type DivClass =
  | 'implementation-only'
  | 'unrealized'
  | 'merged'
  | 'expanded'
  | 'scattered'
  | 'reordered'
  | 'unverified'
  | 'stale'

export interface Divergence {
  class: DivClass
  node: string
  nodes?: string[]
  lane?: string
  detail?: string
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
  errors?: { node?: string; line: number; message: string }[]
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
  problems: Problem[]
  /** Soft hints of the blueprint reference (spec 6.13); never errors. */
  hints: Problem[]
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
/** A blueprint's `domain:` reference (spec 6.12). */
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
  domain?: string
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
}
export interface TeamInvite {
  id: string
  email: string
  expires: string
  state: 'open' | 'used' | 'expired' | 'canceled'
  used_at?: string
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
  members: TeamMember[]
  owners: string[]
  domains: { domain: string; owner: string; after: number }[]
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
  tries: number
  last_try?: string
  warning?: string
}
export interface Reassigned {
  kalka_id: string
  title: string
  path: string
  after: number
}

export const api = {
  summary: () => call<Summary>('GET', '/api/summary'),
  settings: () => call<Settings>('GET', '/api/settings'),
  applySettings: (s: Settings) => call<Settings>('PUT', '/api/settings', s),
  setPaused: (paused: boolean) => call<Settings>('POST', paused ? '/api/pause' : '/api/resume', {}),
  teams: () => call<Team[] | null>('GET', '/api/teams').then((l) => l ?? []),
  createTeam: (name: string) => call<Team>('POST', '/api/teams', { name }),
  invite: (team: string, email: string, expires: string) => call<InviteResult>('POST', `/api/teams/${enc(team)}/invites`, { email, expires }),
  cancelInvite: (id: string) => call<object>('DELETE', '/api/invites/' + enc(id)),
  removalPreview: (team: string, email: string) =>
    call<Reassigned[] | null>('GET', `/api/teams/${enc(team)}/removal?email=${enc(email)}`).then((l) => l ?? []),
  removeMember: (team: string, email: string) => call<Team>('POST', `/api/teams/${enc(team)}/remove`, { email }),
  setOwner: (team: string, email: string, owner: boolean) => call<Team>('POST', `/api/teams/${enc(team)}/owners`, { email, owner }),
  assignDomain: (team: string, domain: string, email: string) => call<Team>('POST', `/api/teams/${enc(team)}/domains`, { domain, email }),
  publish: (path: string, note = '', team = '') => call<PublishResult>('POST', '/api/publish', { path, team, note }),
  release: (path: string, note: string, incorporates: string[]) =>
    call<PublishResult>('POST', '/api/release', { path, note, incorporates }),
  domains: () => call<DomainRow[] | null>('GET', '/api/domains').then((l) => l ?? []),
  deleteForTeam: (path: string) => call<object>('POST', '/api/delete', { path }),
  replicate: (team: string) => call<ReplicationStatus>('POST', `/api/teams/${enc(team)}/replicate`, {}),
  joins: () => call<Join[] | null>('GET', '/api/joins').then((l) => l ?? []),
  join: (token: string) => call<Join>('POST', '/api/joins', { token }),
  seeds: () => call<SeedRow[] | null>('GET', '/api/seeds').then((l) => l ?? []),
  bud: (r: { path: string; from?: string; title: string; problem?: string; why?: string }) => call<BudResult>('POST', '/api/seeds', r),
  backlink: (seed: string) => call<{ entry: string }>('POST', '/api/backlink', { seed }),
  promote: (path: string, status = 'draft') => call<{ ok: boolean }>('POST', '/api/promote', { path, status }),
  status: () => call<Status>('GET', '/api/status'),
  setup: (c: { email: string; rendezvous: string; folder: string }) => call<{ saved: boolean }>('POST', '/api/setup', c),
  blueprints: () => call<BlueprintInfo[] | null>('GET', '/api/blueprints').then((l) => l ?? []),
  doc: (path: string, version?: number) =>
    call<Doc>('GET', '/api/doc?path=' + enc(path) + (version ? '&v=' + version : '')).then((d) => ({ ...d, traces: d.traces ?? [], refs: d.refs ?? {} })),
  addEntry: (e: { path: string; kind: string; anchor: string; re?: string; quote?: string; body: string }) =>
    call<{ id: string }>('POST', '/api/entries', e),
}

export type BusType = 'folder' | 'status' | 'refs' | 'teams' | 'notice' | 'settings'

// events subscribes to the daemon's live updates. EventSource reconnects
// by itself after a restart of the daemon.
export function events(on: (type: BusType, data: unknown) => void, onState?: (open: boolean) => void): () => void {
  const es = new EventSource('/api/events')
  const types: BusType[] = ['folder', 'status', 'refs', 'teams', 'notice', 'settings']
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
