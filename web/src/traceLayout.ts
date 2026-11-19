// Trace layout (spec 6.10: divergences come from Go, layout is
// presentation and lives here). Lanes are columns, most abstract on the
// left; flow runs top to bottom on a grid of rows.
//
// - The most abstract lane takes one row per node, in flow order.
// - A lower-lane node sits level with the first node it realizes, and a
//   node realizing several stretches to the last of them (C3 in the
//   mockup). It never moves above the node before it in its own lane.
// - A node realizing nothing takes the row after its predecessor; a row is
//   inserted for it in the lanes to its left and left empty there: the
//   flow arrow across it already joins the steps on either side.
// - next edges have no cycles (the daemon refuses them); back edges, which
//   return to an earlier step, take no part in the order and are drawn
//   around the right side of the lane, outside the forward edges.
//
// Nothing is saved: the same block always gives the same picture.
import type { Trace, TraceNode } from './api'

export const ROW = 136 // row pitch
export const CARD_H = 88
export const LANE_W = 440
export const CARD_W = 360
export const LANE_GAP = 48
export const PAD_X = 40 // card inset inside its lane
export const HEAD_H = 48 // lane title band above the first row

export interface Placed {
  node: TraceNode
  lane: number
  row: number // first row
  end: number // last row (> row when stretched)
  x: number
  y: number
  h: number
}

export interface Edge {
  kind: 'flow' | 'back' | 'realizes'
  from: string
  to: string
  label?: string
  d: string // SVG path
  lx?: number // label position
  ly?: number
}

export interface Layout {
  width: number
  height: number
  lanes: { name: string; x: number }[]
  placed: Map<string, Placed>
  edges: Edge[]
}

// flowOrder orders a lane's nodes along their next edges; ties fall back
// to the order in the block. next has no cycles in a valid block; should
// one arrive, the earliest remaining node is taken.
export function flowOrder(nodes: TraceNode[]): TraceNode[] {
  const ids = new Set(nodes.map((n) => n.id))
  const indeg = new Map(nodes.map((n) => [n.id, 0]))
  for (const n of nodes) for (const x of n.next ?? []) if (ids.has(x.to)) indeg.set(x.to, indeg.get(x.to)! + 1)
  const pos = new Map(nodes.map((n, i) => [n.id, i]))
  const byId = new Map(nodes.map((n) => [n.id, n]))
  const out: TraceNode[] = []
  const done = new Set<string>()
  const ready = nodes.filter((n) => indeg.get(n.id) === 0).map((n) => n.id)
  while (out.length < nodes.length) {
    if (ready.length === 0) {
      // A cycle: take the earliest remaining node.
      const rest = nodes.find((n) => !done.has(n.id))!
      ready.push(rest.id)
    }
    ready.sort((a, b) => pos.get(a)! - pos.get(b)!)
    const id = ready.shift()!
    if (done.has(id)) continue
    done.add(id)
    const n = byId.get(id)!
    out.push(n)
    // Depth first along the flow keeps a run of steps together.
    const next: string[] = []
    for (const x of n.next ?? []) {
      if (!ids.has(x.to) || done.has(x.to)) continue
      indeg.set(x.to, indeg.get(x.to)! - 1)
      if (indeg.get(x.to) === 0) next.push(x.to)
    }
    ready.unshift(...next)
  }
  return out
}

export function layout(t: Trace): Layout {
  const lanes = t.lanes ?? []
  const nodes = t.nodes ?? []
  const placed = new Map<string, Placed>()

  const put = (n: TraceNode, lane: number, row: number, end: number) =>
    placed.set(n.id, { node: n, lane, row, end, x: 0, y: 0, h: 0 })

  lanes.forEach((name, li) => {
    const mine = flowOrder(nodes.filter((n) => n.lane === name))
    let prevEnd = -1
    for (const n of mine) {
      const targets = (n.realizes ?? []).map((r) => placed.get(r)).filter((p): p is Placed => !!p)
      let row: number
      let end: number
      if (li === 0 || targets.length === 0) {
        row = prevEnd + 1
        end = row
        if (li > 0) {
          // Insert a row in the lanes to the left, unless nothing there
          // starts at or below it.
          const shift = [...placed.values()].some((p) => p.lane < li && p.end >= row)
          if (shift) {
            for (const p of placed.values()) {
              if (p.lane >= li) continue
              if (p.row >= row) {
                p.row++
                p.end++
              } else if (p.end >= row) p.end++
            }
          }
        }
      } else {
        row = Math.max(prevEnd + 1, Math.min(...targets.map((p) => p.row)))
        end = Math.max(row, ...targets.map((p) => p.end))
      }
      put(n, li, row, end)
      prevEnd = end
    }
  })

  const laneX = (li: number) => li * (LANE_W + LANE_GAP)
  for (const p of placed.values()) {
    p.x = laneX(p.lane) + PAD_X
    p.y = HEAD_H + p.row * ROW
    p.h = (p.end - p.row) * ROW + CARD_H
  }
  let rows = 0
  for (const p of placed.values()) rows = Math.max(rows, p.end + 1)

  const edges: Edge[] = []
  for (const p of placed.values()) {
    // Flow: straight down when nothing in the lane sits between the two
    // steps, otherwise around the right side of the lane.
    for (const x of p.node.next ?? []) {
      const q = placed.get(x.to)
      if (!q) continue
      const cx = p.x + CARD_W / 2
      const between = [...placed.values()].some(
        (o) => o.lane === p.lane && o !== p && o !== q && o.row > p.end && o.end < q.row,
      )
      if (q.row > p.end && !between) {
        const y1 = p.y + p.h
        const y2 = q.y
        edges.push({ kind: 'flow', from: p.node.id, to: q.node.id, label: x.label, d: `M ${cx} ${y1} L ${cx} ${y2 - 4}`, lx: cx + 8, ly: (y1 + y2) / 2 })
      } else {
        const side = p.x + CARD_W + 20
        const y1 = p.y + p.h / 2
        const y2 = q.y + q.h / 2
        edges.push({
          kind: 'flow',
          from: p.node.id,
          to: q.node.id,
          label: x.label,
          d: `M ${p.x + CARD_W} ${y1} L ${side} ${y1} L ${side} ${y2} L ${q.x + CARD_W + 4} ${y2}`,
          lx: side + 6,
          ly: (y1 + y2) / 2,
        })
      }
    }
    // Back: from this step's right edge, further out than forward edges,
    // up to the step the flow returns to. A step that repeats itself gets
    // a small loop.
    for (const x of p.node.back ?? []) {
      const q = placed.get(x.to)
      if (!q) continue
      const side = p.x + CARD_W + 32
      const y1 = p.y + p.h / 2 + (q === p ? 12 : 0)
      const y2 = q.y + q.h / 2 - (q === p ? 12 : 0)
      edges.push({
        kind: 'back',
        from: p.node.id,
        to: q.node.id,
        label: x.label,
        d: `M ${p.x + CARD_W} ${y1} L ${side} ${y1} L ${side} ${y2} L ${q.x + CARD_W + 4} ${y2}`,
        lx: side + 6,
        ly: (y1 + y2) / 2,
      })
    }
    // Realization: from the realizer's left edge to the realized node's
    // right edge, at the realized node's middle when the realizer covers it.
    for (const r of p.node.realizes ?? []) {
      const q = placed.get(r)
      if (!q) continue
      const y2 = q.y + q.h / 2
      const y1 = Math.min(Math.max(y2, p.y + 16), p.y + p.h - 16)
      edges.push({ kind: 'realizes', from: p.node.id, to: q.node.id, d: `M ${p.x} ${y1} L ${q.x + CARD_W} ${y2}` })
    }
  }

  return {
    width: lanes.length * LANE_W + Math.max(0, lanes.length - 1) * LANE_GAP,
    height: HEAD_H + Math.max(rows, 1) * ROW - (ROW - CARD_H) + 24,
    lanes: lanes.map((name, i) => ({ name, x: laneX(i) })),
    placed,
    edges,
  }
}

// laneIndex is a small helper for the views.
export function laneIndex(t: Trace, lane: string): number {
  return (t.lanes ?? []).indexOf(lane)
}
