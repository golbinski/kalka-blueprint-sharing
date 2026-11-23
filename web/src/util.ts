import { useEffect, useState } from 'react'

const fmt = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })

// localTime shows an entry timestamp in local time; the file keeps UTC.
export function localTime(utc: string): string {
  const d = new Date(utc)
  return isNaN(d.getTime()) ? utc : fmt.format(d)
}

/** The views that show one scope: a team, or Personal (spec 11.1). */
export type ScopedView = 'list' | 'seeds' | 'team' | 'domains'
const scopedViews: ScopedView[] = ['seeds', 'team', 'domains']

export type Route =
  /** scope is the route's `#/t/<scope>/`; '' when the route names none. */
  | { view: ScopedView; scope: string }
  | { view: 'doc'; path: string; version?: number }
  /** The graph canvas: by kalka_id in a scope, or by path for a document with none. */
  | { view: 'graph'; scope: string; id?: string; path?: string }
  | { view: 'trace'; path: string; index: number; node?: string }
  | { view: 'settings' }
  /** Needs you: every team at once (spec 11.3). */
  | { view: 'needs' }
  | { view: 'join'; token: string }

function decode(s: string): string | null {
  try {
    return decodeURIComponent(s)
  } catch {
    return null
  }
}

export function parseRoute(hash: string): Route {
  const [h, query] = hash.replace(/^#\/?/, '').split('?')
  const q = new URLSearchParams(query ?? '')
  if (h.startsWith('doc/')) {
    const v = Number(q.get('v'))
    return { view: 'doc', path: decodeURIComponent(h.slice(4)), version: Number.isInteger(v) && v > 0 ? v : undefined }
  }
  if (h.startsWith('graph/')) return { view: 'graph', scope: '', path: decode(h.slice(6)) ?? '' }
  if (h.startsWith('trace/')) {
    const rest = h.slice(6)
    const i = rest.lastIndexOf('/')
    const index = Number(rest.slice(i + 1)) - 1
    return { view: 'trace', path: decodeURIComponent(rest.slice(0, i)), index: Number.isInteger(index) && index >= 0 ? index : 0, node: q.get('node') ?? undefined }
  }
  if (h.startsWith('t/')) {
    // #/t/<scope>/<view>; a team's old waiting list is Needs you.
    const [raw, view = '', id = ''] = h.slice(2).split('/')
    if (view === 'waiting') return { view: 'needs' }
    const scope = decode(raw) ?? ''
    if (view === 'graph' && id) return { view: 'graph', scope, id: decode(id) ?? '' }
    return { view: (scopedViews as string[]).includes(view) ? (view as ScopedView) : 'list', scope }
  }
  if ((scopedViews as string[]).includes(h)) return { view: h as ScopedView, scope: '' }
  if (h === 'settings') return { view: 'settings' }
  if (h === 'needs' || h === 'waiting') return { view: 'needs' }
  if (h === 'join' || h.startsWith('join/')) {
    return { view: 'join', token: decode(h.slice(5)) ?? '' }
  }
  return { view: 'list', scope: '' }
}

/** scopeHref is a view of a scope: a team's key, or 'personal'. */
export function scopeHref(scope: string, view: ScopedView = 'list'): string {
  return '#/t/' + encodeURIComponent(scope) + '/' + (view === 'list' ? '' : view)
}

// docHref names a document; version names a kept domain version (spec 6.12).
export function docHref(rel: string, version?: number): string {
  return '#/doc/' + encodeURIComponent(rel) + (version ? '?v=' + version : '')
}

// graphHref is the graph canvas centred on a document: by kalka_id in its
// scope (spec 11.1), or by path when it has no id yet.
export function graphHref(d: { rel: string; kalka_id?: string; scope?: string }): string {
  if (d.kalka_id && d.scope) return '#/t/' + encodeURIComponent(d.scope) + '/graph/' + encodeURIComponent(d.kalka_id)
  return '#/graph/' + encodeURIComponent(d.rel)
}

// atHref names a place in the document view: an entry, a section or an
// item, scrolled to and flashed.
export function atHref(rel: string, id: string, version?: number): string {
  return docHref(rel, version) + (version ? '&' : '?') + 'at=' + encodeURIComponent(id)
}

// traceHref names trace block `index` (0-based) of a blueprint; the URL
// counts from 1, as the view does.
export function traceHref(rel: string, index: number): string {
  return '#/trace/' + encodeURIComponent(rel) + '/' + (index + 1)
}

export function useRoute(): Route {
  const [route, setRoute] = useState(() => parseRoute(location.hash))
  useEffect(() => {
    const on = () => setRoute(parseRoute(location.hash))
    window.addEventListener('hashchange', on)
    return () => window.removeEventListener('hashchange', on)
  }, [])
  return route
}

export function plural(n: number, one: string, many = one + 's'): string {
  return `${n} ${n === 1 ? one : many}`
}

// joinHref is the join screen for what a user pasted, a link or a token;
// the daemon parses it (spec 19.7).
export function joinHref(input: string): string {
  return '#/join/' + encodeURIComponent(input.trim())
}

// setupFromInvite is what the setup form takes from an invite opened
// before setup (spec 11.1): the email, and the rendezvous as the form's
// two choices.
export function setupFromInvite(p: { email: string; rendezvous: string }): { email: string; mode: 'cloud' | 'server'; server: string } {
  if (p.rendezvous === 'peerjs-cloud') return { email: p.email, mode: 'cloud', server: '' }
  return { email: p.email, mode: 'server', server: p.rendezvous }
}

// warningOf is the warning a result carries: a removal or reassignment
// signed while no other member could be reached (spec 19.5).
export function warningOf(r: unknown): string {
  return r && typeof r === 'object' && 'warning' in r && typeof r.warning === 'string' ? r.warning : ''
}

const timeOnly = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit' })

// shortTime is a time of today as 11:02, any other as localTime does.
export function shortTime(utc: string, now = new Date()): string {
  const d = new Date(utc)
  if (isNaN(d.getTime())) return utc
  return d.toDateString() === now.toDateString() ? timeOnly.format(d) : fmt.format(d)
}

/** ago is a time as how long ago it was, for lists: "just now",
 * "12 min ago", "3 h ago", "yesterday", "4 days ago", then the date. */
export function ago(utc?: string, now = Date.now()): string {
  if (!utc) return '—'
  const t = new Date(utc).getTime()
  if (isNaN(t)) return '—'
  const min = Math.floor((now - t) / 60_000)
  if (min < 1) return 'just now'
  if (min < 60) return `${min} min ago`
  const h = Math.floor(min / 60)
  if (h < 24) return `${h} h ago`
  const d = Math.floor(h / 24)
  if (d === 1) return 'yesterday'
  if (d < 7) return `${d} days ago`
  return new Date(t).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: new Date(t).getFullYear() === new Date(now).getFullYear() ? undefined : 'numeric' })
}
