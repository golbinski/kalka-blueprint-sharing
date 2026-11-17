import { useEffect, useState } from 'react'

const fmt = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })

// localTime shows an entry timestamp in local time; the file keeps UTC.
export function localTime(utc: string): string {
  const d = new Date(utc)
  return isNaN(d.getTime()) ? utc : fmt.format(d)
}

/** The views that show one scope: a team, or Personal (spec 11.1). */
export type ScopedView = 'list' | 'seeds' | 'team' | 'domains' | 'waiting'
const scopedViews: ScopedView[] = ['seeds', 'team', 'domains', 'waiting']

export type Route =
  /** scope is the route's `#/t/<scope>/`; '' when the route names none. */
  | { view: ScopedView; scope: string }
  | { view: 'doc'; path: string; version?: number }
  | { view: 'canvas'; path: string }
  | { view: 'trace'; path: string; index: number; node?: string }
  | { view: 'settings' }
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
  if (h.startsWith('canvas/')) return { view: 'canvas', path: decodeURIComponent(h.slice(7)) }
  if (h.startsWith('trace/')) {
    const rest = h.slice(6)
    const i = rest.lastIndexOf('/')
    const index = Number(rest.slice(i + 1)) - 1
    return { view: 'trace', path: decodeURIComponent(rest.slice(0, i)), index: Number.isInteger(index) && index >= 0 ? index : 0, node: q.get('node') ?? undefined }
  }
  if (h.startsWith('t/')) {
    // #/t/<scope>/<view>: the tray opens #/t/<team>/waiting (spec 22.1).
    const [raw, view = ''] = h.slice(2).split('/')
    const scope = decode(raw) ?? ''
    return { view: (scopedViews as string[]).includes(view) ? (view as ScopedView) : 'list', scope }
  }
  if ((scopedViews as string[]).includes(h)) return { view: h as ScopedView, scope: '' }
  if (h === 'settings') return { view: 'settings' }
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

export function canvasHref(rel: string): string {
  return '#/canvas/' + encodeURIComponent(rel)
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

export function roleText(role: string, owner?: string): string {
  switch (role) {
    case 'owner':
      return 'you own this plan'
    case 'reviewer':
      return `owner ${owner} · you are reviewing`
    default:
      return 'not shared yet'
  }
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
