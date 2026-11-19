import { describe, expect, it } from 'vitest'
import type { Trace, TraceNode } from './api'
import { flowOrder, layout } from './traceLayout'

const n = (id: string, lane: string, extra: Partial<TraceNode> = {}): TraceNode => ({ id, lane, text: id, line: 0, ...extra })

// The spec's example block (spec 6.10), as the mockup draws it.
const spec: Trace = {
  index: 0,
  line: 1,
  source: '',
  version: 1,
  divergences: [],
  graph: 0,
  lanes: ['business', 'code'],
  nodes: [
    n('B1', 'business', { next: [{ to: 'B2' }] }),
    n('B2', 'business', { next: [{ to: 'B3' }] }),
    n('B3', 'business', { next: [{ to: 'B4' }] }),
    n('B4', 'business'),
    n('C1', 'code', { realizes: ['B1'], next: [{ to: 'C2' }] }),
    n('C2', 'code', { next: [{ to: 'C3' }] }),
    n('C3', 'code', { realizes: ['B2', 'B3'], next: [{ to: 'C4' }] }),
    n('C4', 'code', { realizes: ['B4'] }),
  ],
}

const rows = (t: Trace) => {
  const l = layout(t)
  return Object.fromEntries([...l.placed.values()].map((p) => [p.node.id, p.row === p.end ? p.row : `${p.row}-${p.end}`]))
}

describe('trace layout', () => {
  it('matches the mockup', () => {
    // B1 | C1; gap | C2; B2,B3 | C3 spanning both; B4 | C4.
    expect(rows(spec)).toEqual({ B1: 0, B2: 2, B3: 3, B4: 4, C1: 0, C2: 1, C3: '2-3', C4: 4 })
    const l = layout(spec)
    // Row 1 is left empty in the business lane: nothing is drawn there.
    expect([...l.placed.values()].filter((p) => p.lane === 0 && p.row <= 1 && p.end >= 1)).toEqual([])
    // C3 is as tall as B2 and B3 together, gap included.
    const c3 = l.placed.get('C3')!
    expect(c3.y).toBe(l.placed.get('B2')!.y)
    expect(c3.y + c3.h).toBe(l.placed.get('B3')!.y + l.placed.get('B3')!.h)
    expect(l.edges.filter((e) => e.kind === 'realizes')).toHaveLength(4)
    // B1 → B2 passes the gap, which is empty: a straight arrow.
    expect(l.edges.find((e) => e.from === 'B1')!.d).toMatch(/^M \S+ \S+ L /)
  })

  it('never moves a step above the one before it in its lane', () => {
    const t: Trace = {
      ...spec,
      nodes: [
        n('B1', 'business', { next: [{ to: 'B2' }] }),
        n('B2', 'business'),
        n('C1', 'code', { realizes: ['B2'], next: [{ to: 'C2' }] }),
        n('C2', 'code', { realizes: ['B1'] }),
      ],
    }
    expect(rows(t)).toEqual({ B1: 0, B2: 1, C1: 1, C2: 2 })
  })

  it('routes a flow arrow around steps it skips', () => {
    const t: Trace = {
      ...spec,
      nodes: [
        n('B1', 'business', { next: [{ to: 'B2', label: 'small' }, { to: 'B3', label: 'large' }] }),
        n('B2', 'business'),
        n('B3', 'business'),
        n('C1', 'code', { realizes: ['B1', 'B2', 'B3'] }),
      ],
    }
    const l = layout(t)
    const toB3 = l.edges.find((e) => e.to === 'B3')!
    expect(toB3.label).toBe('large')
    expect(toB3.d.split(' L ')).toHaveLength(4)
  })

  it('places three lanes and is deterministic', () => {
    const t: Trace = {
      ...spec,
      lanes: ['business', 'service', 'code'],
      nodes: [
        n('B1', 'business'),
        n('S1', 'service', { realizes: ['B1'], next: [{ to: 'S2' }] }),
        n('S2', 'service'),
        n('K1', 'code', { realizes: ['S1'], next: [{ to: 'K2' }] }),
        n('K2', 'code', { realizes: ['S2'] }),
      ],
    }
    expect(rows(t)).toEqual({ B1: 0, S1: 0, S2: 1, K1: 0, K2: 1 })
    expect(JSON.stringify([...layout(t).placed])).toBe(JSON.stringify([...layout(t).placed]))
  })

  it('orders a lane along its flow, not the block order', () => {
    const order = flowOrder([n('X3', 'a'), n('X1', 'a', { next: [{ to: 'X2' }] }), n('X2', 'a', { next: [{ to: 'X3' }] })])
    expect(order.map((x) => x.id)).toEqual(['X1', 'X2', 'X3'])
  })

  it('draws back edges apart from the flow, and keeps the forward order', () => {
    const t: Trace = {
      ...spec,
      lanes: ['business', 'code'],
      nodes: [
        n('A1', 'business'),
        n('F1', 'code', { realizes: ['A1'], next: [{ to: 'F2' }] }),
        n('F2', 'code', { realizes: ['A1'], back: [{ to: 'F1', label: 'while pages remain' }, { to: 'F2' }] }),
      ],
    }
    expect(rows(t)).toEqual({ A1: 0, F1: 0, F2: 1 })
    const back = layout(t).edges.filter((e) => e.kind === 'back')
    expect(back.map((e) => [e.from, e.to, e.label])).toEqual([
      ['F2', 'F1', 'while pages remain'],
      ['F2', 'F2', undefined],
    ])
    expect(layout(t).edges.filter((e) => e.kind === 'flow')).toHaveLength(1)
  })
})
