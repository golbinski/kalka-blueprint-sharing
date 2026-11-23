// @vitest-environment jsdom
// The graph canvas (spec 6.18, 11.1): layout, expansion and the open-items
// panel, on the library sample as kalka graph --json prints it.
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, type Doc, type Graph } from './api'
import { DocBar } from './DocNav'
import fixture from './graph.fixture.json'
import { GraphPage, loadExpansion, saveExpansion } from './Graph'
import { emptyExpansion, layoutGraph, MORE_LIMIT, stepsUnder } from './graphLayout'
;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

// kalka graph prints no rel; the app's graph says where each card is held.
const g = { ...(fixture as unknown as Graph) }
g.nodes = g.nodes.map((n) => (n.id === 'plan:3e9b7a60-1f24-4d8e-b0c5-7a2d91e4f803' ? { ...n, rel: 'return-creates-hold.md', scope: 'lending' } : n))
const FOCUS = 'plan:3e9b7a60-1f24-4d8e-b0c5-7a2d91e4f803'
const doc = {
  path: '/f/library/return-creates-hold.md',
  rel: 'return-creates-hold.md',
  kalka_id: '3e9b7a60-1f24-4d8e-b0c5-7a2d91e4f803',
  title: 'Return creates a hold and notifies the queue head',
  role: 'owner',
  self: 'marek@example.org',
  rev: '',
  sections: [],
  headings: [],
  entries: [],
  problems: [],
  invariants: [],
  glossary: { state: 'none', terms: [] },
  scope: 'lending',
  traces: [],
  refs: {},
} as unknown as Doc

function memStorage(): Storage {
  const m = new Map<string, string>()
  return {
    get length() {
      return m.size
    },
    clear: () => m.clear(),
    getItem: (k) => m.get(k) ?? null,
    key: (i) => [...m.keys()][i] ?? null,
    removeItem: (k) => void m.delete(k),
    setItem: (k, v) => void m.set(k, String(v)),
  }
}

describe('graph layout', () => {
  it('draws the lens as two bands, the focus open at level 2, the same every time', () => {
    const exp = { ...emptyExpansion(), cards: [FOCUS] }
    const a = layoutGraph(g, exp)
    expect(a.cards.map((c) => c.node.band + ' ' + (c.node.domain ?? c.node.issue))).toEqual([
      'domain library',
      'domain library.reservations',
      'plan jira:DEV-100',
      'plan jira:DEV-123',
    ])
    const focus = a.cards.find((c) => c.id === FOCUS)!
    expect(focus.items.map((i) => i.node.item)).toEqual(['AC1', 'AC2'])
    expect(a.cards.filter((c) => c.expanded).map((c) => c.id)).toEqual([FOCUS])
    // The domain band is left of the plan band; roots at the top.
    const lib = a.cards[0]
    const res = a.cards[1]
    expect(lib.y).toBeLessThan(res.y)
    expect(focus.x).toBeGreaterThan(lib.x + lib.w)
    expect(JSON.stringify(layoutGraph(g, exp))).toBe(JSON.stringify(a))
  })
  it('opens a criterion into the steps that cover it, down to the code', () => {
    expect(stepsUnder(g, FOCUS + '/criterion:AC1').map((s) => s.item)).toEqual(['B3', 'C4'])
    const l = layoutGraph(g, { ...emptyExpansion(), cards: [FOCUS], criteria: [FOCUS + '/criterion:AC1'] })
    const focus = l.cards.find((c) => c.id === FOCUS)!
    expect(focus.steps.map((s) => s.node.item)).toEqual(['B3', 'C4'])
    // B1, C1, C2 and C5 are under no criterion: offered apart.
    expect(focus.otherSteps).toBe(4)
    expect(l.wires.some((w) => w.edge.kind === 'covers')).toBe(true)
  })
  it('shows the first nodes of a large level and a "+N more" row', () => {
    const big: Graph = {
      ...g,
      nodes: [
        ...g.nodes,
        ...Array.from({ length: MORE_LIMIT + 5 }, (_, i) => ({ id: FOCUS + '/invariant:X' + i, kind: 'invariant' as const, card: FOCUS, item: 'X' + i, text: 'x' })),
      ],
    }
    const c = layoutGraph(big, { ...emptyExpansion(), cards: [FOCUS] }).cards.find((x) => x.id === FOCUS)!
    expect(c.items.length).toBe(MORE_LIMIT)
    expect(c.more).toBe(7)
    const all = layoutGraph(big, { ...emptyExpansion(), cards: [FOCUS], more: [FOCUS] }).cards.find((x) => x.id === FOCUS)!
    expect(all.more).toBe(0)
  })
})

describe('expansion state', () => {
  it('is kept per focus, and a storage failure gives the default', () => {
    const st = memStorage()
    expect(loadExpansion(FOCUS, st).cards).toEqual([FOCUS])
    saveExpansion(FOCUS, { ...emptyExpansion(), cards: [] }, st)
    expect(loadExpansion(FOCUS, st).cards).toEqual([])
    const broken = { getItem: () => { throw new Error('denied') }, setItem: () => { throw new Error('denied') } } as unknown as Storage
    expect(loadExpansion(FOCUS, broken).cards).toEqual([FOCUS])
    expect(() => saveExpansion(FOCUS, emptyExpansion(), broken)).not.toThrow()
  })
})

let host: HTMLDivElement
let root: Root
beforeEach(() => {
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
  vi.stubGlobal('localStorage', memStorage())
  Element.prototype.scrollIntoView = () => {}
  vi.spyOn(api, 'graph').mockResolvedValue({ graph: g, rel: doc.rel })
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

const render = async (el: React.ReactElement) => {
  await act(async () => root.render(el))
}
const button = (text: string) => [...host.querySelectorAll('button')].find((b) => b.textContent?.includes(text))!

describe('the graph page', () => {
  it('lists what needs attention, in the Trace view’s words', async () => {
    await render(<GraphPage doc={doc} />)
    const panel = host.querySelector('[aria-labelledby="gopen-title"]')!
    expect(panel.querySelector('h2')!.textContent).toContain('3 places need attention')
    expect(panel.querySelector('h2')!.getAttribute('aria-live')).toBe('polite')
    expect([...panel.querySelectorAll('li')].map((li) => li.textContent)).toEqual(['goal G3 is unclaimed', 'step C2: code only', 'step C5: code only'])
  })
  it('opens AC1 into B3 and C4, and C4 into the Trace view', async () => {
    await render(<GraphPage doc={doc} />)
    expect(host.querySelector('.gstep')).toBeNull()
    await act(async () => button('AC1').click())
    expect([...host.querySelectorAll('.gstep .mono')].map((s) => s.textContent)).toEqual(['B3', 'C4'])
    await act(async () => button('C4').click())
    const open = [...host.querySelectorAll('.gdetail a')].find((a) => a.textContent === 'Open in Trace')!
    expect(open.getAttribute('href')).toBe('#/trace/return-creates-hold.md/1?node=C4')
  })
  it('reaches every card and item from the keyboard, in reading order', async () => {
    await render(<GraphPage doc={doc} />)
    const heads = [...host.querySelectorAll<HTMLButtonElement>('.gcard-head')]
    expect(heads.map((b) => b.querySelector('.gcard-title')!.textContent)).toEqual([
      'library@v2',
      'library.reservations@v1',
      'DEV-100 Holds and notification on return',
      'DEV-123 Return creates a hold and notifies the queue head',
    ])
    expect(heads[2].getAttribute('aria-expanded')).toBe('false')
    await act(async () => heads[2].click())
    expect(host.querySelectorAll<HTMLButtonElement>('.gcard-head')[2].getAttribute('aria-expanded')).toBe('true')
    expect(button('G3')).toBeTruthy()
  })
  it('reveals an open item: the step under no criterion is shown and selected', async () => {
    await render(<GraphPage doc={doc} />)
    await act(async () => button('step C5').click())
    expect([...host.querySelectorAll('.gstep .mono')].map((s) => s.textContent)).toContain('C5')
    expect(host.querySelector('.gstep.selected .mono')!.textContent).toBe('C5')
  })
})

describe('the document bar', () => {
  it('has Document, Trace and Graph', async () => {
    await render(<DocBar doc={doc} scope="lending" view="graph" traceIndex={0} />)
    const tabs = [...host.querySelectorAll('[aria-label="Views"] > *')]
    expect(tabs.map((t) => t.textContent)).toEqual(['Document', 'Trace', 'Graph'])
    const graph = tabs[2] as HTMLAnchorElement
    expect(graph.getAttribute('href')).toBe('#/t/lending/graph/3e9b7a60-1f24-4d8e-b0c5-7a2d91e4f803')
    expect(graph.className).toBe('active')
  })
})
