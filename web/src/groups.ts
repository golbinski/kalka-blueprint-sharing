// Grouping blueprints by what their issue tracker says (spec 11.1, 10.5):
// by a parent issue at a chosen level, or by status category. The page
// offers only what an installed plugin declares; the choice and "show
// archived" are kept per scope in the page's storage.
import type { IssuesView, IssueView } from './api'

export type GroupBy = 'none' | 'parent' | 'status'

export interface GroupChoice {
  by: GroupBy
  /** For parent: 1 is the nearest parent (an epic), 2 the one above it. */
  level: number
}

export const NO_GROUPS: GroupChoice = { by: 'none', level: 1 }

export type GroupKind = 'parent' | 'status' | 'no-parent' | 'unavailable' | 'no-issue'

export interface Group<T> {
  key: string
  kind: GroupKind
  label: string
  /** For a parent group: the parent issue, linked. */
  issue?: string
  title?: string
  url?: string
  rows: T[]
}

export interface Level {
  level: number
  /** The issue type most often seen at this level, such as Epic. */
  label: string
}

const STATUS_ORDER = ['todo', 'in_progress', 'done'] as const
export const STATUS_LABEL: Record<string, string> = { todo: 'To do', in_progress: 'In progress', done: 'Done' }

/** groupOptions are the choices the installed plugins make possible. */
export function groupOptions(caps: IssuesView['caps'] | undefined): GroupBy[] {
  const out: GroupBy[] = ['none']
  if (caps?.hierarchy) out.push('parent')
  if (caps?.meta) out.push('status')
  return out
}

/** usable falls back to no grouping when a choice is no longer offered. */
export function usable(c: GroupChoice, caps: IssuesView['caps'] | undefined): GroupChoice {
  return groupOptions(caps).includes(c.by) ? c : NO_GROUPS
}

/** levels lists the parent levels the fetched issues have, nearest first. */
export function levels(issues: Record<string, IssueView>): Level[] {
  const types: Map<string, number>[] = []
  for (const v of Object.values(issues)) {
    ;(v.meta?.parents ?? []).forEach((p, i) => {
      types[i] ??= new Map()
      const t = p.type || 'Parent'
      types[i].set(t, (types[i].get(t) ?? 0) + 1)
    })
  }
  return types.map((m, i) => ({ level: i + 1, label: [...m.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))[0][0] }))
}

/**
 * groupRows groups rows, keeping their order inside each group. Groups of
 * parents come in the order of their first row; status groups in the order
 * to do, in progress, done. Blueprints with no parent at the level, whose
 * issue cannot be read, or with no issue at all come last, in that order.
 */
export function groupRows<T extends { issue?: string }>(rows: T[], c: GroupChoice, issues: Record<string, IssueView>, levelLabel = 'parent'): Group<T>[] {
  if (c.by === 'none') return [{ key: 'all', kind: 'parent', label: '', rows }]
  const groups = new Map<string, Group<T>>()
  const tail = {
    'no-parent': { key: '~no-parent', kind: 'no-parent', label: `No ${levelLabel.toLowerCase()}`, rows: [] } as Group<T>,
    unavailable: { key: '~unavailable', kind: 'unavailable', label: 'Issue not available', rows: [] } as Group<T>,
    'no-issue': { key: '~no-issue', kind: 'no-issue', label: 'No issue', rows: [] } as Group<T>,
  }
  for (const r of rows) {
    if (!r.issue) {
      tail['no-issue'].rows.push(r)
      continue
    }
    const m = issues[r.issue]?.meta
    if (!m?.ok) {
      tail.unavailable.rows.push(r)
      continue
    }
    if (c.by === 'status') {
      const cat = m.status?.category ?? 'todo'
      const g = groups.get(cat) ?? { key: cat, kind: 'status', label: STATUS_LABEL[cat] ?? cat, rows: [] }
      g.rows.push(r)
      groups.set(cat, g)
      continue
    }
    const p = m.parents?.[c.level - 1]
    if (!p) {
      tail['no-parent'].rows.push(r)
      continue
    }
    const g = groups.get(p.issue) ?? {
      key: p.issue,
      kind: 'parent',
      label: p.title ? `${p.issue} · ${p.title}` : p.issue,
      issue: p.issue,
      title: p.title,
      url: p.url,
      rows: [],
    }
    g.rows.push(r)
    groups.set(p.issue, g)
  }
  let out = [...groups.values()]
  if (c.by === 'status') out = STATUS_ORDER.flatMap((s) => (groups.has(s) ? [groups.get(s)!] : []))
  for (const g of [tail['no-parent'], tail.unavailable, tail['no-issue']]) if (g.rows.length > 0) out.push(g)
  return out
}

const groupKey = (scope: string) => 'kalka.group-by.' + scope
const archivedKey = (scope: string) => 'kalka.show-archived.' + scope

type Store = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

/** loadGroupBy reads the choice for a scope; none when storage fails. */
export function loadGroupBy(scope: string, storage?: Store): GroupChoice {
  try {
    const raw = (storage ?? globalThis.localStorage).getItem(groupKey(scope))
    if (!raw) return NO_GROUPS
    const v = JSON.parse(raw) as Partial<GroupChoice>
    if ((v.by === 'parent' || v.by === 'status' || v.by === 'none') && Number.isInteger(v.level) && (v.level ?? 0) >= 1) return { by: v.by, level: v.level! }
  } catch {
    // Storage may be unavailable (a locked-down webview): no grouping.
  }
  return NO_GROUPS
}

export function saveGroupBy(scope: string, c: GroupChoice, storage?: Store) {
  try {
    const st = storage ?? globalThis.localStorage
    if (c.by === 'none') st.removeItem(groupKey(scope))
    else st.setItem(groupKey(scope), JSON.stringify(c))
  } catch {
    // Not remembered; the choice still holds on this page.
  }
}

/** loadShowArchived: archived documents are hidden unless asked. */
export function loadShowArchived(scope: string, storage?: Store): boolean {
  try {
    return (storage ?? globalThis.localStorage).getItem(archivedKey(scope)) === '1'
  } catch {
    return false
  }
}

export function saveShowArchived(scope: string, show: boolean, storage?: Store) {
  try {
    const st = storage ?? globalThis.localStorage
    if (show) st.setItem(archivedKey(scope), '1')
    else st.removeItem(archivedKey(scope))
  } catch {
    // Not remembered.
  }
}
