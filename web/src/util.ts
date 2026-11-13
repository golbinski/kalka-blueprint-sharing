import { useEffect, useState } from 'react'

const fmt = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })

// localTime shows an entry timestamp in local time; the file keeps UTC.
export function localTime(utc: string): string {
  const d = new Date(utc)
  return isNaN(d.getTime()) ? utc : fmt.format(d)
}

export type Route =
  | { view: 'list' }
  | { view: 'doc'; path: string }
  | { view: 'trace'; path: string; index: number; node?: string }
  | { view: 'peers' }
  | { view: 'seeds' }

export function parseRoute(hash: string): Route {
  const [h, query] = hash.replace(/^#\/?/, '').split('?')
  const q = new URLSearchParams(query ?? '')
  if (h.startsWith('doc/')) return { view: 'doc', path: decodeURIComponent(h.slice(4)) }
  if (h.startsWith('trace/')) {
    const rest = h.slice(6)
    const i = rest.lastIndexOf('/')
    const index = Number(rest.slice(i + 1)) - 1
    return { view: 'trace', path: decodeURIComponent(rest.slice(0, i)), index: Number.isInteger(index) && index >= 0 ? index : 0, node: q.get('node') ?? undefined }
  }
  if (h === 'peers') return { view: 'peers' }
  if (h === 'seeds') return { view: 'seeds' }
  return { view: 'list' }
}

export function docHref(rel: string): string {
  return '#/doc/' + encodeURIComponent(rel)
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
