// The document rail's pure parts (spec 11.1): whether it is collapsed,
// and where a row leads from the view on screen.
import { docHref, traceHref } from './util'

/** RAIL_KEY holds "1" while the user keeps the rail collapsed. */
export const RAIL_KEY = 'kalka.rail-collapsed'

/** NARROW is the window width below which the rail starts collapsed;
 * the desktop window opens at 1200, where the rail is open. */
export const NARROW = 1000

// loadCollapsed is the remembered state; open when nothing is stored or
// storage is unavailable (a private window, a blocked origin).
// Reading localStorage itself throws where storage is blocked, so it is
// looked up inside the try.
export function loadCollapsed(storage?: Pick<Storage, 'getItem'>): boolean {
  try {
    return (storage ?? globalThis.localStorage).getItem(RAIL_KEY) === '1'
  } catch {
    return false
  }
}

export function saveCollapsed(collapsed: boolean, storage?: Pick<Storage, 'setItem' | 'removeItem'>): void {
  try {
    const st = storage ?? globalThis.localStorage
    if (collapsed) st.setItem(RAIL_KEY, '1')
    else st.removeItem(RAIL_KEY)
  } catch {
    /* not remembered: it opens next time */
  }
}

/** The views a document opens in. */
export type DocView = 'doc' | 'trace' | 'canvas'

// railHref opens a document in the same view where it has one: from a
// trace, its first trace when it has any, else its document; from the
// document or its map, its document.
export function railHref(target: { rel: string; traces?: number }, view: DocView): string {
  if (view === 'trace' && (target.traces ?? 0) > 0) return traceHref(target.rel, 0)
  return docHref(target.rel)
}
