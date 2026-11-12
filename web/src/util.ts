import { useEffect, useState } from 'react'

const fmt = new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })

// localTime shows an entry timestamp in local time; the file keeps UTC.
export function localTime(utc: string): string {
  const d = new Date(utc)
  return isNaN(d.getTime()) ? utc : fmt.format(d)
}

export type Route = { view: 'list' } | { view: 'doc'; path: string } | { view: 'peers' }

export function parseRoute(hash: string): Route {
  const h = hash.replace(/^#\/?/, '')
  if (h.startsWith('doc/')) return { view: 'doc', path: decodeURIComponent(h.slice(4)) }
  if (h === 'peers') return { view: 'peers' }
  return { view: 'list' }
}

export function docHref(rel: string): string {
  return '#/doc/' + encodeURIComponent(rel)
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
