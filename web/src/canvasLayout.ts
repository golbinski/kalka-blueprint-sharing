// Canvas layout (spec 11.1, "Review (canvas)"): the linear view's data as
// a graph. Body sections are cards in reading order down the middle; each
// entry is a note to the right, joined to its section's card, replies
// joined to the note they answer; the references a section links to are
// small cards to its left.
//
// Sizes come from the text, not from the browser, so the same document
// always gives the same picture, and nothing needs to be saved.
import type { Doc, Entry } from './api'
import { groupEntries, thread } from './entries'
import { plainText, refsIn } from './markdown'

export const PAD = 24
export const REF_W = 240
export const REF_H = 56
export const SEC_W = 440
export const NOTE_W = 340
export const COL_GAP = 72
export const INDENT = 24 // per reply level
export const MAX_DEPTH = 4
export const ROW_GAP = 32
export const STACK_GAP = 10

export const REF_X = PAD
export const SEC_X = REF_X + REF_W + COL_GAP
export const NOTE_X = SEC_X + SEC_W + COL_GAP

const SEC_CHARS = 58 // per excerpt line
const SEC_LINES = 5
const NOTE_CHARS = 44
const NOTE_LINES = 3

export type CardKind = 'section' | 'general' | 'orphaned'

export interface SecCard {
  key: string // 's<index>', 'general' or 'orphaned'
  kind: CardKind
  index: number // into doc.sections, -1 for the others
  slug?: string
  level?: number
  title: string
  excerpt: string
  lines: number
  traces: number // trace blocks in the section
  entries: number // notes beside it, replies included
  x: number
  y: number
  w: number
  h: number
}

export interface NoteCard {
  entry: Entry
  depth: number
  card: string // the SecCard key
  text: string
  lines: number
  x: number
  y: number
  w: number
  h: number
}

export interface RefCard {
  key: string
  ref: string
  card: string
  x: number
  y: number
  w: number
  h: number
}

export interface CanvasEdge {
  kind: 'note' | 'reply' | 'ref'
  from: string
  to: string
  d: string
}

export interface CanvasLayout {
  width: number
  height: number
  sections: SecCard[]
  notes: NoteCard[]
  refs: RefCard[]
  edges: CanvasEdge[]
}

const releaseRe = /^v(\d+) · plan:[0-9a-f]{64} · owner:(\S+)( · deleted)?$/

// noteText is what a note shows: the entry in words, without markup.
export function noteText(e: Entry): string {
  switch (e.kind) {
    case 'budded':
      if (e.budded_seed) return 'Split off a seed: ' + (e.budded_title || e.budded_seed)
      break
    case 'handoff':
      return 'Ownership passed to ' + (e.handoff_target ?? '?')
    case 'release': {
      const m = releaseRe.exec(e.body.split('\n')[0])
      if (m) return m[3] ? `Deleted for the team at version ${m[1]}` : `Released version ${m[1]}, owner ${m[2]}`
      break
    }
  }
  return plainText(e.body) || '(empty)'
}

const lines = (text: string, per: number, max: number) => (text ? Math.min(max, Math.max(1, Math.ceil(text.length / per))) : 0)

// Heights follow the card CSS in styles.css (.cv-*): padding, a title
// line, the clamped text lines and a foot line.
export const secHeight = (n: number) => 24 + 24 + n * 20 + 24
export const noteHeight = (n: number) => 20 + 20 + n * 18

const curve = (x1: number, y1: number, x2: number, y2: number) => {
  const dx = Math.max(24, (x2 - x1) / 2)
  return `M${x1} ${y1} C${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`
}

export function layout(doc: Doc): CanvasLayout {
  const g = groupEntries(doc)
  const sections: SecCard[] = []
  const notes: NoteCard[] = []
  const refs: RefCard[] = []
  const edges: CanvasEdge[] = []
  let y = PAD
  let right = NOTE_X + NOTE_W

  const row = (card: Omit<SecCard, 'x' | 'y' | 'w' | 'h' | 'lines' | 'entries'>, top: Entry[], links: string[]) => {
    const c: SecCard = { ...card, lines: lines(card.excerpt, SEC_CHARS, SEC_LINES), entries: 0, x: SEC_X, y, w: SEC_W, h: 0 }
    c.h = secHeight(c.lines)
    sections.push(c)
    const anchorY = y + 24
    let ny = y
    for (const t of top) {
      const items = thread(t, g.replies)
      const placed: NoteCard[] = []
      for (const { e, depth } of items) {
        const d = Math.min(depth, MAX_DEPTH)
        const text = noteText(e)
        const n = lines(text, NOTE_CHARS, NOTE_LINES)
        const nc: NoteCard = { entry: e, depth: d, card: c.key, text, lines: n, x: NOTE_X + d * INDENT, y: ny, w: NOTE_W, h: noteHeight(n) }
        right = Math.max(right, nc.x + nc.w)
        notes.push(nc)
        c.entries++
        placed.push(nc)
        if (depth === 0) {
          edges.push({ kind: 'note', from: c.key, to: e.id, d: curve(SEC_X + SEC_W, anchorY, nc.x, ny + 18) })
        } else {
          const parent = placed.find((p) => p.entry.id === e.reply_to)
          if (parent) {
            const px = parent.x + 12
            edges.push({ kind: 'reply', from: parent.entry.id, to: e.id, d: `M${px} ${parent.y + parent.h} V${ny + 18} H${nc.x}` })
          }
        }
        ny += nc.h + STACK_GAP
      }
    }
    let ry = y
    links.forEach((ref, i) => {
      const rc: RefCard = { key: `${c.key}-r${i}`, ref, card: c.key, x: REF_X, y: ry, w: REF_W, h: REF_H }
      refs.push(rc)
      edges.push({ kind: 'ref', from: rc.key, to: c.key, d: curve(REF_X + REF_W, ry + REF_H / 2, SEC_X, anchorY) })
      ry += REF_H + STACK_GAP
    })
    y = Math.max(y + c.h, ny - STACK_GAP, ry - STACK_GAP) + ROW_GAP
  }

  doc.sections.forEach((s, i) => {
    const slug = s.heading?.slug
    row(
      {
        key: 's' + i,
        kind: 'section',
        index: i,
        slug,
        level: s.heading?.level,
        title: s.heading?.text ?? (i === 0 ? 'Before the first heading' : '(no heading)'),
        excerpt: plainText(s.markdown),
        traces: doc.traces.filter((t) => t.line >= s.line && (i + 1 >= doc.sections.length || t.line < doc.sections[i + 1].line)).length,
      },
      slug !== undefined ? g.bySlug.get(slug) ?? [] : [],
      refsIn(s.markdown),
    )
  })
  row({ key: 'general', kind: 'general', index: -1, title: 'General', excerpt: 'Entries about the whole plan.', traces: 0 }, g.general, [])
  if (g.orphaned.length > 0) {
    row(
      { key: 'orphaned', kind: 'orphaned', index: -1, title: 'Orphaned', excerpt: 'Entries pointing at headings no longer in the plan.', traces: 0 },
      g.orphaned,
      [],
    )
  }
  return { width: right + PAD, height: y - ROW_GAP + PAD, sections, notes, refs, edges }
}
