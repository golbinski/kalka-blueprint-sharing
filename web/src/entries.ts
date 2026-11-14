// Grouping of review-log entries by where the linear view shows them.
// The canvas uses the same grouping, so both views show the same entries.
import type { Doc, Entry } from './api'

export interface Grouped {
  byId: Map<string, Entry>
  // replies of an entry held in this file, in file order
  replies: Map<string, Entry[]>
  // top-level entries per section, keyed by the section heading's slug
  bySlug: Map<string, Entry[]>
  general: Entry[]
  orphaned: Entry[]
}

export function groupEntries(doc: Doc): Grouped {
  const byId = new Map(doc.entries.map((e) => [e.id, e]))
  const replies = new Map<string, Entry[]>()
  const top: Entry[] = []
  for (const e of doc.entries) {
    if (e.reply_to && byId.has(e.reply_to) && e.reply_to !== e.id) {
      replies.set(e.reply_to, [...(replies.get(e.reply_to) ?? []), e])
    } else top.push(e)
  }
  const slugOf = new Map<string, string>()
  for (const s of doc.sections) for (const sl of s.slugs) slugOf.set(sl, s.heading?.slug ?? '')
  const bySlug = new Map<string, Entry[]>()
  const general: Entry[] = []
  const orphaned: Entry[] = []
  for (const e of top) {
    if (!e.section) general.push(e)
    else if (e.orphaned || !slugOf.has(e.section)) orphaned.push(e)
    else {
      const k = slugOf.get(e.section)!
      bySlug.set(k, [...(bySlug.get(k) ?? []), e])
    }
  }
  return { byId, replies, bySlug, general, orphaned }
}

// thread lists an entry and its replies, depth first, with their depth.
// A reply cycle (only possible in a hand-edited file) is cut.
export function thread(e: Entry, replies: Map<string, Entry[]>, depth = 0, seen = new Set<string>()): { e: Entry; depth: number }[] {
  if (seen.has(e.id)) return []
  seen.add(e.id)
  const out = [{ e, depth }]
  for (const k of replies.get(e.id) ?? []) out.push(...thread(k, replies, depth + 1, seen))
  return out
}
