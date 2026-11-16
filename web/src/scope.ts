// The models behind the switcher, the waiting list, the join screen and
// the transfer badges (spec 11.1, 11.3, 20.7). Pure, so they are tested
// without a page.
import type { Join, JoinPreview, Team, TransferInfo, WaitingItem } from './api'
import { plural, scopeHref } from './util'

export const PERSONAL = 'personal'

/** teamKey is how routes and ?team= name a team: its name, or its hash
 * when two teams held here share the name. */
export function teamKey(t: Team, teams: Team[]): string {
  return teams.some((o) => o.team !== t.team && o.name === t.name) ? t.team : t.name
}

/** findTeam is the team a scope names, by name or by hash. */
export function findTeam(scope: string, teams: Team[]): Team | undefined {
  return teams.find((t) => t.team === scope) ?? teams.find((t) => t.name === scope)
}

/** resolveScope picks the scope shown: the route's, else the remembered
 * one while it still exists, else the first team, else Personal. */
export function resolveScope(route: string, remembered: string, teams: Team[] | null): string {
  if (route) return route
  if (remembered === PERSONAL) return PERSONAL
  if (teams === null) return remembered || ''
  if (remembered && findTeam(remembered, teams)) return remembered
  const first = sortTeams(teams)[0]
  return first ? teamKey(first, teams) : PERSONAL
}

const SCOPE_KEY = 'kalka.scope'

export function rememberedScope(): string {
  try {
    return localStorage.getItem(SCOPE_KEY) ?? ''
  } catch {
    return ''
  }
}

export function rememberScope(scope: string) {
  try {
    if (scope) localStorage.setItem(SCOPE_KEY, scope)
  } catch {
    /* a private window */
  }
}

function sortTeams(teams: Team[]): Team[] {
  return [...teams].sort((a, b) => a.name.localeCompare(b.name) || a.team.localeCompare(b.team))
}

export interface SwitcherRow {
  key: string
  label: string
  /** A short hash, when two teams share the name. */
  hash?: string
  waiting: number
  current: boolean
  href: string
  personal?: boolean
}

/** switcherRows: Personal first, then every team by name, each with its
 * waiting count (spec 11.1). */
export function switcherRows(teams: Team[], scope: string): SwitcherRow[] {
  const cur = scope === PERSONAL ? undefined : findTeam(scope, teams)
  const rows: SwitcherRow[] = [
    { key: PERSONAL, label: 'Personal', waiting: 0, current: scope === PERSONAL, href: scopeHref(PERSONAL), personal: true },
  ]
  for (const t of sortTeams(teams)) {
    const key = teamKey(t, teams)
    rows.push({
      key,
      label: t.name,
      hash: key === t.team ? t.team.slice(0, 8) : undefined,
      waiting: t.waiting ?? 0,
      current: cur?.team === t.team,
      href: scopeHref(key),
    })
  }
  return rows
}

/** scopeLabel is the switcher's button text. */
export function scopeLabel(scope: string, teams: Team[]): string {
  if (scope === PERSONAL) return 'Personal'
  return findTeam(scope, teams)?.name ?? scope
}

/** joiningLines: "joining <team>…" for each join still pending. */
export function joiningLines(joins: Join[]): string[] {
  return joins.filter((j) => j.state === 'pending').map((j) => `joining ${j.name}…`)
}

export type WaitingButton = 'accept' | 'decline' | 'open' | 'reassign'

export interface WaitingRow {
  key: string
  kind: string
  text: string
  by?: string
  at: string
  href?: string
  buttons: WaitingButton[]
  item: WaitingItem
}

const KIND_LABEL: Record<WaitingItem['kind'], string> = {
  transfer: 'Transfer',
  handed: 'Yours now',
  disputed: 'Ownership disputed',
  'owner-gone': 'Owner left',
}

/** waitingRows: each item with its kind, who and when, and its buttons
 * (spec 11.1, 11.3). The list stays in the daemon's order, oldest first. */
export function waitingRows(items: WaitingItem[]): WaitingRow[] {
  return items.map((it) => {
    const doc = it.kind !== 'transfer' && it.target.startsWith('#/doc/')
    const buttons: WaitingButton[] =
      it.kind === 'transfer' ? ['accept', 'decline'] : it.kind === 'handed' ? ['open'] : ['open', 'reassign']
    return {
      key: it.kind + ':' + (it.transfer ?? it.kalka_id ?? it.path ?? it.title),
      kind: KIND_LABEL[it.kind] ?? it.kind,
      text: it.message || it.title,
      by: it.by,
      at: it.at,
      href: doc ? it.target : undefined,
      buttons: doc ? buttons : buttons.filter((b) => b !== 'open'),
      item: it,
    }
  })
}

export type JoinState = 'ready' | 'problem' | 'joining' | 'joined'

/** joinState is what the join screen shows in place of its button. */
export function joinState(p: JoinPreview): JoinState {
  if (p.joining && p.joining.state === 'joined') return 'joined'
  if (p.joining && p.joining.state === 'pending') return 'joining'
  if (p.problem) return 'problem'
  return 'ready'
}

/** attestedText says where a copied or moved history came from and who
 * vouches for it (spec 20.7). */
export function attestedText(t: TransferInfo): string {
  if (!t.from) return ''
  const how = t.mode === 'move' ? 'Moved' : 'Copied'
  const who = t.actor && t.by ? ` Its history is attested by ${t.actor}, who offered it, and ${t.by}, who accepted it.` : ''
  return `${how} from ${t.from}.${who}`
}

/** transferEntries is what an offer brings, for its row. */
export function transferEntries(n: number): string {
  return n === 0 ? 'no review entries' : plural(n, 'attested entry', 'attested entries')
}

/** attestedBadge is the title of an entry's "attested" badge: written in
 * the team the document came from, vouched for by the transfer (20.7). */
export function attestedBadge(author: string): string {
  return `written by ${author} in the team this document came from; carried over by the transfer and vouched for by the two who made it`
}
