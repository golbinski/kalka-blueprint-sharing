// Navigation in a document (spec 11.1): sectionOf, the top bar's highlight,
// the breadcrumb, the document rail and its remembered state.
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, type BlueprintInfo, type Doc, type DomainRow, type SeedRow } from './api'
import { DocBar, DocRail, SectionsNav } from './DocNav'
import { loadCollapsed, RAIL_KEY, railHref, saveCollapsed } from './rail'
import { activeSection, blueprintRows, sectionOf, sectionOfRow } from './section'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const TEAM = 'parcel-ops'

const doc = (rel: string, more: Partial<Doc> = {}): Doc =>
  ({
    path: '/f/parcel-ops/' + rel,
    rel,
    title: rel.replace(/\.md$/, ''),
    role: 'owner',
    self: 'ola@parcels.example',
    rev: '',
    sections: [],
    headings: [],
    entries: [],
    problems: [],
    invariants: [],
    glossary: { state: 'none', terms: [] },
    scope: TEAM,
    traces: [],
    refs: {},
    ...more,
  }) as Doc

const row = (rel: string, more: Partial<BlueprintInfo> = {}): BlueprintInfo => ({
  path: '/f/parcel-ops/' + rel,
  rel,
  title: rel.replace(/\.md$/, ''),
  entries: 0,
  errors: 0,
  modified: '2026-11-01T10:00:00Z',
  ...more,
})

const domainInfo = { name: 'delivery', team: TEAM, versions: [] } as unknown as Doc['domain']
const seedInfo = { parent: 'p' }

// The three kinds as an open document and as the list row the daemon sends.
const kinds = {
  blueprint: { doc: doc('redelivery.md'), row: row('redelivery.md') },
  seed: { doc: doc('lockers.md', { seed: seedInfo }), row: row('lockers.md', { seed: true }) },
  domain: { doc: doc('delivery-domain.md', { domain: domainInfo }), row: row('delivery-domain.md', { domain: 'delivery' }) },
}

describe('sectionOf', () => {
  it('puts a blueprint, a seed and a domain document in their sections', () => {
    expect(sectionOf(kinds.blueprint.doc)).toBe('list')
    expect(sectionOf(kinds.seed.doc)).toBe('seeds')
    expect(sectionOf(kinds.domain.doc)).toBe('domains')
  })
  it('agrees with the list rows, so a document is highlighted where its list holds it', () => {
    for (const k of Object.values(kinds)) expect(sectionOfRow(k.row, TEAM)).toBe(sectionOf(k.doc))
    // The Blueprints list holds the blueprint only; seeds and domains have their own lists.
    expect(blueprintRows(Object.values(kinds).map((k) => k.row), TEAM).map((b) => b.rel)).toEqual(['redelivery.md'])
  })
  it('keeps what has no Domains list in Blueprints: Personal, a duplicate, an invalid domain name', () => {
    expect(sectionOf(doc('d.md', { domain: domainInfo, scope: 'personal' }))).toBe('list')
    expect(sectionOfRow(row('d.md', { domain: 'delivery' }), 'personal')).toBe('list')
    expect(sectionOf(doc('d.md', { domain: domainInfo, duplicate: true }))).toBe('list')
    expect(sectionOfRow(row('d.md', { domain: 'delivery', duplicate: true }), TEAM)).toBe('list')
    // kalka_kind: domain with no valid kalka_domain: the daemon gives no name.
    expect(sectionOf(doc('d.md', { domain: { ...domainInfo!, name: '' } }))).toBe('list')
    expect(sectionOfRow(row('d.md'), TEAM)).toBe('list')
  })
  it('lists a seed in Personal under Seeds too', () => {
    expect(sectionOf(doc('s.md', { seed: seedInfo, scope: 'personal' }))).toBe('seeds')
    expect(blueprintRows([row('s.md', { seed: true })], 'personal')).toEqual([])
  })
})

// An in-memory Storage: newer Node versions put a localStorage of their
// own in front of jsdom's, without its methods unless given a file.
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

let host: HTMLDivElement
let root: Root
const render = async (el: React.ReactNode) => {
  await act(async () => root.render(el))
}

beforeEach(() => {
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
  vi.stubGlobal('localStorage', memStorage())
  window.innerWidth = 1200
  vi.spyOn(api, 'waiting').mockResolvedValue([])
  vi.spyOn(api, 'seeds').mockResolvedValue([])
  vi.spyOn(api, 'domains').mockResolvedValue([])
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe('a domain document', () => {
  // The old back link said "‹ Blueprints" for a domain document and led to the wrong list.
  it('has a breadcrumb back to Domains', async () => {
    await render(<DocBar doc={kinds.domain.doc} scope={TEAM} view="doc" traceIndex={0} />)
    const crumb = host.querySelector<HTMLAnchorElement>('nav[aria-label="Document"] .crumbs a')!
    expect(crumb.textContent).toBe('Domains')
    expect(crumb.getAttribute('href')).toBe('#/t/parcel-ops/domains')
    expect(host.querySelector('[aria-current="page"]')!.textContent).toBe('delivery-domain')
  })
  it('keeps Domains highlighted in the top bar', async () => {
    const active = activeSection('doc', kinds.domain.doc)
    expect(active).toBe('domains')
    await render(<SectionsNav scope={TEAM} personal={false} active={active} needs={0} />)
    const current = [...host.querySelectorAll('nav[aria-label="Sections"] a[aria-current="page"]')]
    expect(current.map((a) => a.textContent)).toEqual(['Domains'])
  })
  it('also from its trace, and highlights nothing while the document loads', () => {
    expect(activeSection('trace', kinds.domain.doc)).toBe('domains')
    expect(activeSection('doc', null)).toBe('')
    expect(activeSection('seeds', null)).toBe('seeds')
  })
})

describe('the document rail', () => {
  const list = [
    row('redelivery.md', { activity: '2026-11-03T10:00:00Z', traces: 2 }),
    row('address-change.md', { activity: '2026-11-05T10:00:00Z', new: 2 }),
    row('lockers.md', { seed: true }),
    row('delivery-domain.md', { domain: 'delivery' }),
  ]
  const links = () => [...host.querySelectorAll<HTMLAnchorElement>('.doc-rail-list a')]

  it('lists the section of the open document, in its list order, and marks the open one', async () => {
    await render(<DocRail doc={doc('redelivery.md')} scope={TEAM} where="parcel-ops" list={list} view="doc" reloadKey={0} />)
    expect(host.querySelector('nav')!.getAttribute('aria-label')).toBe('Blueprints in parcel-ops')
    // Most recent activity first, as on the Blueprints page; no seed, no domain document.
    expect(links().map((a) => a.textContent?.replace('needs you', ''))).toEqual(['address-change', 'redelivery'])
    expect(links().filter((a) => a.getAttribute('aria-current') === 'page').map((a) => a.textContent)).toEqual(['redelivery'])
    // address-change has comments not seen yet.
    expect(links()[0].querySelector('.doc-rail-needs')).not.toBeNull()
    expect(links()[1].querySelector('.doc-rail-needs')).toBeNull()
  })
  it('lists seeds for a seed, in the Seeds page order, marking what is waiting', async () => {
    const seeds = [
      { rel: 'lockers.md', title: 'Lockers', kalka_id: 'a' },
      { rel: 'night-drop.md', title: 'Night drop', kalka_id: 'b' },
    ] as SeedRow[]
    vi.spyOn(api, 'seeds').mockResolvedValue(seeds)
    vi.spyOn(api, 'waiting').mockResolvedValue([{ kind: 'handed', team: TEAM, team_name: TEAM, at: '', target: '', title: '', message: '', path: '/f/parcel-ops/night-drop.md' }])
    const withSeed = [...list, row('night-drop.md', { seed: true })]
    await render(<DocRail doc={kinds.seed.doc} scope={TEAM} where="parcel-ops" list={withSeed} view="doc" reloadKey={0} />)
    expect(links().map((a) => a.textContent?.replace('needs you', ''))).toEqual(['Lockers', 'Night drop'])
    expect(links()[1].querySelector('.doc-rail-needs')).not.toBeNull()
  })
  it('lists domain documents for a domain document', async () => {
    vi.spyOn(api, 'domains').mockResolvedValue([{ rel: 'delivery-domain.md', title: 'Delivery', domain: 'delivery' }] as DomainRow[])
    await render(<DocRail doc={kinds.domain.doc} scope={TEAM} where="parcel-ops" list={list} view="doc" reloadKey={0} />)
    expect(host.querySelector('nav')!.getAttribute('aria-label')).toBe('Domains in parcel-ops')
    expect(links().map((a) => a.textContent)).toEqual(['Delivery'])
  })
  it('opens the next document in the trace view when it has traces, else its document', async () => {
    expect(railHref({ rel: 'redelivery.md', traces: 2 }, 'trace')).toBe('#/trace/redelivery.md/1')
    expect(railHref({ rel: 'address-change.md' }, 'trace')).toBe('#/doc/address-change.md')
    expect(railHref({ rel: 'redelivery.md', traces: 2 }, 'doc')).toBe('#/doc/redelivery.md')
    expect(railHref({ rel: 'redelivery.md', traces: 2 }, 'graph')).toBe('#/doc/redelivery.md')
    await render(<DocRail doc={doc('redelivery.md', { traces: [{}] as Doc['traces'] })} scope={TEAM} where="parcel-ops" list={list} view="trace" reloadKey={0} />)
    expect(links().map((a) => a.getAttribute('href'))).toEqual(['#/doc/address-change.md', '#/trace/redelivery.md/1'])
  })
  it('collapses, says so to assistive technology, and remembers it', async () => {
    await render(<DocRail doc={doc('redelivery.md')} scope={TEAM} where="parcel-ops" list={list} view="doc" reloadKey={0} />)
    const toggle = host.querySelector<HTMLButtonElement>('.doc-rail-toggle')!
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
    expect(toggle.getAttribute('aria-label')).toBe('Hide Blueprints in parcel-ops')
    await act(async () => toggle.click())
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    expect(links()).toEqual([])
    expect(localStorage.getItem(RAIL_KEY)).toBe('1')
    // A new page starts collapsed.
    act(() => root.unmount())
    root = createRoot(host)
    await render(<DocRail doc={doc('redelivery.md')} scope={TEAM} where="parcel-ops" list={list} view="doc" reloadKey={0} />)
    expect(host.querySelector('.doc-rail-toggle')!.getAttribute('aria-expanded')).toBe('false')
  })
  it('starts collapsed on a narrow window, without changing the remembered choice', async () => {
    window.innerWidth = 800
    await render(<DocRail doc={doc('redelivery.md')} scope={TEAM} where="parcel-ops" list={list} view="doc" reloadKey={0} />)
    const toggle = host.querySelector<HTMLButtonElement>('.doc-rail-toggle')!
    expect(toggle.getAttribute('aria-expanded')).toBe('false')
    await act(async () => toggle.click())
    expect(toggle.getAttribute('aria-expanded')).toBe('true')
    expect(localStorage.getItem(RAIL_KEY)).toBeNull()
  })
})

describe('the remembered rail state', () => {
  it('round-trips through storage', () => {
    const m = new Map<string, string>()
    const st = { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v), removeItem: (k: string) => void m.delete(k) }
    expect(loadCollapsed(st)).toBe(false)
    saveCollapsed(true, st)
    expect(loadCollapsed(st)).toBe(true)
    saveCollapsed(false, st)
    expect(loadCollapsed(st)).toBe(false)
  })
  it('falls back to open when storage throws', () => {
    const broken = {
      getItem: () => {
        throw new Error('SecurityError')
      },
      setItem: () => {
        throw new Error('QuotaExceededError')
      },
      removeItem: () => {
        throw new Error('SecurityError')
      },
    }
    expect(loadCollapsed(broken)).toBe(false)
    expect(() => saveCollapsed(true, broken)).not.toThrow()
  })
  it('falls back to open when reading localStorage itself throws', () => {
    const spy = vi.spyOn(globalThis, 'localStorage', 'get').mockImplementation(() => {
      throw new Error('SecurityError')
    })
    expect(loadCollapsed()).toBe(false)
    expect(() => saveCollapsed(true)).not.toThrow()
    spy.mockRestore()
  })
})
