// Layout of the graph canvas (spec 11.1): computed from the graph and the
// expansion state every time, never saved. The same input gives the same
// picture. Two bands: the domain tree on the left, the plan tree to its
// right, each a layered tree with the root at the top. An expanded card
// lists its items; an expanded criterion shows, beside the card, the trace
// steps that cover it and the steps that realize those, down to the code
// lane.
import type { Graph, GraphEdge, GraphNode } from './api'

export const CARD_W = 300
export const HEAD_H = 84
export const ITEM_H = 30
export const PAD = 10
export const GAP_X = 40
export const GAP_Y = 64
export const BAND_GAP = 120
export const STEP_W = 200
export const STEP_H = 62
export const STEP_GAP = 14
/** An expanded level shows this many nodes, then "+N more". */
export const MORE_LIMIT = 40

/** What is open: cards at level 2, criteria at level 3, the steps of a
 * card not under any criterion, and cards showing all their items. */
export interface Expansion {
  cards: string[]
  criteria: string[]
  steps: string[]
  more: string[]
}

export const emptyExpansion = (): Expansion => ({ cards: [], criteria: [], steps: [], more: [] })

export interface Box {
  id: string
  x: number
  y: number
  w: number
  h: number
}

export interface ItemBox extends Box {
  node: GraphNode
}

export interface StepGroup {
  /** The criterion the steps are under; '' for the other steps. */
  under: string
  label: string
  y: number
  h: number
}

export interface CardBox extends Box {
  node: GraphNode
  /** The card's own frame: header and items. w and h are its footprint,
   * the steps beside it included. */
  frameH: number
  expanded: boolean
  items: ItemBox[]
  /** Items not shown: the "+N more" row. */
  more: number
  steps: ItemBox[]
  groups: StepGroup[]
  /** Steps of the card under no criterion, offered as "Other steps", and
   * where that row is. */
  otherSteps: number
  otherY: number
}

export interface Wire {
  edge: GraphEdge
  d: string
  /** Where a label goes. */
  lx: number
  ly: number
}

export interface Layout {
  width: number
  height: number
  cards: CardBox[]
  /** Every visible card, item and step by node id. */
  boxes: Map<string, Box>
  wires: Wire[]
  /** Visible nodes in reading order: domain band, then plan tree, top to bottom. */
  order: string[]
}

const has = (xs: string[], x: string) => xs.includes(x)

/** The items a card lists at level 2: goals, criteria and invariants of a
 * plan node; terms and invariants of a domain node. */
export function cardItems(g: Graph, card: GraphNode): GraphNode[] {
  const kinds = card.kind === 'domain' ? ['term', 'invariant'] : ['goal', 'criterion', 'invariant']
  const items = g.nodes.filter((n) => n.card === card.id && kinds.includes(n.kind))
  // Terms after invariants read as the domain's vocabulary closing the card.
  return [...items.filter((n) => n.kind !== 'term'), ...items.filter((n) => n.kind === 'term')]
}

/** stepsUnder are the steps a criterion opens: those that cover it, then
 * those that realize them, lane by lane, down to the code. */
export function stepsUnder(g: Graph, criterion: string): GraphNode[] {
  const byId = new Map(g.nodes.map((n) => [n.id, n]))
  const out: string[] = []
  let frontier = g.edges.filter((e) => e.kind === 'covers' && e.to === criterion).map((e) => e.from)
  while (frontier.length > 0) {
    const next: string[] = []
    for (const id of frontier) {
      if (out.includes(id)) continue
      out.push(id)
      for (const e of g.edges) if (e.kind === 'step-realizes' && e.to === id) next.push(e.from)
    }
    frontier = next
  }
  const rank = (n: GraphNode) => (n.lanes ?? []).indexOf(n.lane ?? '')
  const order = new Map(g.nodes.map((n, i) => [n.id, i]))
  return out
    .map((id) => byId.get(id)!)
    .filter(Boolean)
    .sort((a, b) => rank(a) - rank(b) || order.get(a.id)! - order.get(b.id)!)
}

/** criteriaLeadingTo are the criteria whose steps include step. */
export function criteriaLeadingTo(g: Graph, step: string): string[] {
  return g.nodes.filter((n) => n.kind === 'criterion' && stepsUnder(g, n.id).some((s) => s.id === step)).map((n) => n.id)
}

function placeSteps(steps: GraphNode[], x0: number, y0: number, placed: Map<string, ItemBox>): { boxes: ItemBox[]; h: number; w: number } {
  const lanes: string[] = []
  for (const s of steps) for (const l of s.lanes ?? []) if (!lanes.includes(l)) lanes.push(l)
  const rows = new Map<number, number>()
  const out: ItemBox[] = []
  let h = 0
  for (const s of steps) {
    if (placed.has(s.id)) continue
    const col = Math.max(0, lanes.indexOf(s.lane ?? ''))
    const row = rows.get(col) ?? 0
    rows.set(col, row + 1)
    const b: ItemBox = { id: s.id, node: s, x: x0 + col * (STEP_W + STEP_GAP), y: y0 + row * (STEP_H + STEP_GAP), w: STEP_W, h: STEP_H }
    placed.set(s.id, b)
    out.push(b)
    h = Math.max(h, b.y + b.h - y0)
  }
  return { boxes: out, h, w: lanes.length * (STEP_W + STEP_GAP) }
}

/** cardBox lays out one card at the origin: header, items, steps. */
function cardBox(g: Graph, n: GraphNode, exp: Expansion): CardBox {
  const expanded = !n.placeholder && has(exp.cards, n.id)
  const c: CardBox = { id: n.id, node: n, x: 0, y: 0, w: CARD_W, h: HEAD_H, frameH: HEAD_H, expanded, items: [], more: 0, steps: [], groups: [], otherSteps: 0, otherY: 0 }
  if (!expanded) return c
  let all = cardItems(g, n)
  if (all.length > MORE_LIMIT && !has(exp.more, n.id)) {
    c.more = all.length - MORE_LIMIT
    all = all.slice(0, MORE_LIMIT)
  }
  let y = HEAD_H
  for (const it of all) {
    c.items.push({ id: it.id, node: it, x: PAD, y, w: CARD_W - 2 * PAD, h: ITEM_H - 4 })
    y += ITEM_H
  }
  if (c.more > 0) y += ITEM_H
  const stepsOf = g.nodes.filter((s) => s.card === n.id && s.kind === 'step')
  const rest = stepsOf.filter((s) => !g.nodes.some((cr) => cr.card === n.id && cr.kind === 'criterion' && stepsUnder(g, cr.id).some((x) => x.id === s.id)))
  c.otherSteps = rest.length
  if (rest.length > 0) {
    c.otherY = y
    y += ITEM_H
  }
  c.h = y + PAD
  c.frameH = c.h
  // Level 3, beside the card.
  const placed = new Map<string, ItemBox>()
  const sx = CARD_W + 24
  let sy = HEAD_H
  let width = 0
  let shown = 0
  for (const it of c.items) {
    if (it.node.kind !== 'criterion' || !has(exp.criteria, it.id)) continue
    const top = Math.max(sy, it.y)
    const steps = stepsUnder(g, it.id)
    const r = placeSteps(steps.slice(0, MORE_LIMIT - shown), sx, top + 20, placed)
    shown += r.boxes.length
    c.steps.push(...r.boxes)
    const h = 20 + Math.max(r.h, STEP_H)
    c.groups.push({ under: it.id, label: steps.length > 0 ? `${it.node.item}: covered by` : `${it.node.item}: no trace step covers it`, y: top, h })
    sy = top + h + STEP_GAP
    width = Math.max(width, r.w)
  }
  if (has(exp.steps, n.id) && rest.length > 0) {
    const r = placeSteps(rest.slice(0, Math.max(0, MORE_LIMIT - shown)), sx, sy + 20, placed)
    c.steps.push(...r.boxes)
    const h = 20 + Math.max(r.h, STEP_H)
    c.groups.push({ under: '', label: 'Steps under no criterion', y: sy, h })
    sy += h + STEP_GAP
    width = Math.max(width, r.w)
  }
  if (c.groups.length > 0) {
    c.w = sx + width
    c.h = Math.max(c.h, sy)
  }
  return c
}

/** layoutGraph places the visible part of the graph. */
export function layoutGraph(g: Graph, exp: Expansion): Layout {
  const cards = g.nodes.filter((n) => !n.card)
  const boxes = new Map<string, Box>()
  const out: CardBox[] = []
  const order: string[] = []
  let x0 = 0
  let height = 0
  for (const band of ['domain', 'plan'] as const) {
    const inBand = cards.filter((n) => n.band === band)
    if (inBand.length === 0) continue
    const ids = new Set(inBand.map((n) => n.id))
    const tree = g.edges.filter((e) => (e.kind === 'parent' || e.kind === 'domain-parent') && e.marker !== 'cycle' && ids.has(e.from) && ids.has(e.to))
    // Depth: the longest way down from a root, so a node is below all it hangs from.
    const depth = new Map<string, number>()
    for (const n of inBand) if (!tree.some((e) => e.to === n.id)) depth.set(n.id, 0)
    for (let pass = 0; pass < inBand.length; pass++) {
      for (const e of tree) {
        const d = depth.get(e.from)
        if (d !== undefined && (depth.get(e.to) ?? -1) < d + 1) depth.set(e.to, d + 1)
      }
    }
    const rows: CardBox[][] = []
    for (const n of inBand) {
      const d = depth.get(n.id) ?? 0
      ;(rows[d] ??= []).push(cardBox(g, n, exp))
    }
    let y = 0
    let bandW = 0
    for (const row of rows) {
      if (!row) continue
      let x = x0
      let rowH = 0
      for (const c of row) {
        c.x = x
        c.y = y
        x += c.w + GAP_X
        rowH = Math.max(rowH, c.h)
        out.push(c)
      }
      bandW = Math.max(bandW, x - GAP_X - x0)
      y += rowH + GAP_Y
    }
    height = Math.max(height, y - GAP_Y)
    x0 += bandW + BAND_GAP
  }
  for (const c of out) {
    // Edges meet the card's frame, not the steps beside it.
    boxes.set(c.id, { id: c.id, x: c.x, y: c.y, w: CARD_W, h: c.frameH })
    order.push(c.id)
    for (const it of [...c.items, ...c.steps]) {
      const b = { ...it, x: c.x + it.x, y: c.y + it.y }
      boxes.set(it.id, b)
      order.push(it.id)
    }
  }
  const cardOf = new Map(g.nodes.map((n) => [n.id, n.card ?? n.id]))
  const wires: Wire[] = []
  for (const e of g.edges) {
    let a = boxes.get(e.from)
    let b = boxes.get(e.to)
    const liftedA = !a
    const liftedB = !b
    a ??= boxes.get(cardOf.get(e.from) ?? '')
    b ??= boxes.get(cardOf.get(e.to) ?? '')
    // An edge between two hidden items, or within one card, is not drawn.
    if (!a || !b || a === b || (liftedA && liftedB)) continue
    wires.push(wire(e, a, b))
  }
  return { width: Math.max(0, x0 - BAND_GAP), height, cards: out, boxes, wires, order }
}

function wire(edge: GraphEdge, a: Box, b: Box): Wire {
  let x1: number, y1: number, x2: number, y2: number
  if (a.x + a.w <= b.x) {
    ;[x1, y1, x2, y2] = [a.x + a.w, a.y + Math.min(a.h / 2, 40), b.x, b.y + Math.min(b.h / 2, 40)]
  } else if (b.x + b.w <= a.x) {
    ;[x1, y1, x2, y2] = [a.x, a.y + Math.min(a.h / 2, 40), b.x + b.w, b.y + Math.min(b.h / 2, 40)]
  } else if (a.y + a.h <= b.y) {
    ;[x1, y1, x2, y2] = [a.x + a.w / 2, a.y + a.h, b.x + b.w / 2, b.y]
  } else {
    ;[x1, y1, x2, y2] = [a.x + a.w / 2, a.y, b.x + b.w / 2, b.y + b.h]
  }
  const horizontal = Math.abs(x2 - x1) > Math.abs(y2 - y1)
  const k = horizontal ? (x2 - x1) / 2 : (y2 - y1) / 2
  const d = horizontal ? `M${x1},${y1} C${x1 + k},${y1} ${x2 - k},${y2} ${x2},${y2}` : `M${x1},${y1} C${x1},${y1 + k} ${x2},${y2 - k} ${x2},${y2}`
  return { edge, d, lx: (x1 + x2) / 2, ly: (y1 + y2) / 2 }
}
