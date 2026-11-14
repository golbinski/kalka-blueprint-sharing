// Client for the daemon's local API (spec 13). The browser holds the token
// as an HttpOnly cookie, so requests only need same-origin credentials.

export interface Status {
  setup?: boolean
  defaults?: { folder: string; rendezvous: string }
  email: string
  peer_id: string
  key_peer_id?: string
  fingerprint?: string
  key_online?: boolean
  key_error?: string
  rendezvous: string
  folder: string
  online: boolean
  error?: string
  notice?: string
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
  owner_edited?: boolean
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
  handoff?: 'verified' | 'unverified'
  budded_seed?: string
  budded_title?: string
  budded_rel?: string
  handoff_target?: string
  handoff_note?: string
  unparsed?: string
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
  owner_edited?: boolean
  duplicate?: boolean
  traces: Trace[]
  refs: Record<string, RefView>
  seed?: { parent?: string; parent_rel?: string; parent_title?: string; backlink_missing?: boolean }
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

export interface Peer {
  email: string
  pinned: boolean
  verified: boolean
  first_seen?: string
  key_changed?: boolean
  code?: string
  fingerprint?: string
  key_id?: boolean
  presence: 'online' | 'offline' | 'unknown'
  checked?: string
}

export interface CodeView {
  email: string
  code: string
  verified: boolean
  key_changed?: boolean
}

export type PromptKind = 'verify-code' | 'accept-new' | 'accept-handoff' | 'converge'

export interface Prompt {
  id: string
  scope?: string
  created: string
  kind: PromptKind
  peer: string
  kalka_id?: string
  title?: string
  size?: number
  code?: string
  message: string
}

export interface SyncResult {
  kalka_id: string
  title?: string
  path?: string
  peer: string
  intent: string
  outcome: 'new' | 'unchanged' | 'updated' | 'rejected'
  reason?: string
  detail?: string
  conflict?: string
  notes?: string[]
}

export interface SyncEvent {
  type: 'progress' | 'prompt' | 'prompt-closed' | 'result' | 'error'
  message?: string
  prompt?: Prompt
  result?: SyncResult
  error?: string
  kind?: string
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

// Teams (spec 19).
export interface TeamMember {
  email: string
  owner?: boolean
  fingerprint: string
  since?: string
  you?: boolean
  pin: 'match' | 'none' | 'clash'
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
  teams: () => call<Team[] | null>('GET', '/api/teams').then((l) => l ?? []),
  createTeam: (name: string) => call<Team>('POST', '/api/teams', { name }),
  invite: (team: string, email: string, expires: string) => call<InviteResult>('POST', `/api/teams/${enc(team)}/invites`, { email, expires }),
  cancelInvite: (id: string) => call<object>('DELETE', '/api/invites/' + enc(id)),
  removalPreview: (team: string, email: string) =>
    call<Reassigned[] | null>('GET', `/api/teams/${enc(team)}/removal?email=${enc(email)}`).then((l) => l ?? []),
  removeMember: (team: string, email: string) => call<Team>('POST', `/api/teams/${enc(team)}/remove`, { email }),
  setOwner: (team: string, email: string, owner: boolean) => call<Team>('POST', `/api/teams/${enc(team)}/owners`, { email, owner }),
  assignDomain: (team: string, domain: string, email: string) => call<Team>('POST', `/api/teams/${enc(team)}/domains`, { domain, email }),
  joins: () => call<Join[] | null>('GET', '/api/joins').then((l) => l ?? []),
  join: (token: string) => call<Join>('POST', '/api/joins', { token }),
  seeds: () => call<SeedRow[] | null>('GET', '/api/seeds').then((l) => l ?? []),
  bud: (r: { path: string; from?: string; title: string; problem?: string; why?: string }) => call<BudResult>('POST', '/api/seeds', r),
  backlink: (seed: string) => call<{ entry: string }>('POST', '/api/backlink', { seed }),
  promote: (path: string, status = 'draft') => call<{ ok: boolean }>('POST', '/api/promote', { path, status }),
  status: () => call<Status>('GET', '/api/status'),
  setup: (c: { email: string; rendezvous: string; folder: string }) => call<{ saved: boolean }>('POST', '/api/setup', c),
  blueprints: () => call<BlueprintInfo[] | null>('GET', '/api/blueprints').then((l) => l ?? []),
  doc: (path: string) =>
    call<Doc>('GET', '/api/doc?path=' + enc(path)).then((d) => ({ ...d, traces: d.traces ?? [], refs: d.refs ?? {} })),
  addEntry: (e: { path: string; kind: string; anchor: string; re?: string; quote?: string; body: string }) =>
    call<{ id: string }>('POST', '/api/entries', e),
  peers: () => call<Peer[] | null>('GET', '/api/peers').then((l) => l ?? []),
  addPeer: (email: string) => call<Peer>('POST', '/api/peers', { email }),
  forget: (email: string) => call<{ forgotten: boolean }>('DELETE', '/api/peers/' + enc(email)),
  code: (email: string) => call<CodeView>('GET', '/api/verify/' + enc(email)),
  confirm: (email: string) => call<CodeView>('POST', '/api/verify/' + enc(email), {}),
  prompts: () => call<Prompt[] | null>('GET', '/api/prompts').then((l) => l ?? []),
  answer: (id: string, accept: boolean) => call<{ answered: boolean }>('POST', '/api/prompts/' + enc(id), { accept }),

  // sync streams NDJSON events until the sync ends.
  async sync(req: { path: string; to: string; intent: 'sync' | 'handoff' }, on: (e: SyncEvent) => void): Promise<void> {
    let r: Response
    try {
      r = await fetch('/api/sync', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(req),
      })
    } catch {
      throw new ApiError('kalka is not reachable. Is it still running?', 0, 'down')
    }
    if (!r.ok || !r.body) {
      const t = await r.text()
      let msg = t
      try {
        msg = JSON.parse(t).error ?? t
      } catch {
        /* plain text */
      }
      throw new ApiError(msg, r.status)
    }
    const reader = r.body.getReader()
    const dec = new TextDecoder()
    let buf = ''
    for (;;) {
      const { done, value } = await reader.read()
      if (value) buf += dec.decode(value, { stream: true })
      let i: number
      while ((i = buf.indexOf('\n')) >= 0) {
        const line = buf.slice(0, i).trim()
        buf = buf.slice(i + 1)
        if (line) on(JSON.parse(line) as SyncEvent)
      }
      if (done) return
    }
  },
}

export type BusType = 'folder' | 'prompt' | 'prompt-closed' | 'sync' | 'status' | 'peers' | 'refs' | 'teams'

// events subscribes to the daemon's live updates. EventSource reconnects
// by itself after a restart of the daemon.
export function events(on: (type: BusType, data: unknown) => void, onState?: (open: boolean) => void): () => void {
  const es = new EventSource('/api/events')
  const types: BusType[] = ['folder', 'prompt', 'prompt-closed', 'sync', 'status', 'peers', 'refs', 'teams']
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
