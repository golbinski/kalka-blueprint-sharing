// Issues in the page (spec 10.5, 11.1): grouping the Blueprints list and
// the rail by parent issue or status, offered only with a plugin's
// capability; archived documents; the issue chip and the export control.
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, type BlueprintInfo, type Doc, type IssuesView, type IssueView, type Status } from './api'
import { DocBar, DocRail } from './DocNav'
import { groupOptions, groupRows, levels, loadGroupBy, loadShowArchived, saveGroupBy, saveShowArchived, usable } from './groups'
import { Blueprints } from './Views'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const TEAM = 'parcel-ops'
const row = (rel: string, more: Partial<BlueprintInfo> = {}): BlueprintInfo => ({
  path: '/f/parcel-ops/' + rel,
  rel,
  title: rel.replace(/\.md$/, ''),
  entries: 0,
  errors: 0,
  modified: '2026-11-01T10:00:00Z',
  ...more,
})

const epic = { issue: 'DEV-90', title: 'Failed-delivery handling', type: 'Epic', url: 'https://jira.example/browse/DEV-90' }
const initiative = { issue: 'DEV-12', title: 'Last-mile reliability', type: 'Initiative', url: 'https://jira.example/browse/DEV-12' }
const view = (key: string, meta?: Partial<NonNullable<IssueView['meta']>>): IssueView => ({
  issue: 'jira:' + key,
  scheme: 'jira',
  payload: key,
  caps: { meta: true, hierarchy: true, export: { attachment: true, comment: true } },
  meta: meta ? { issue: key, ok: true, ...meta } : undefined,
})
const issues: IssuesView = {
  caps: { meta: true, hierarchy: true, export: true },
  issues: {
    'jira:DEV-123': view('DEV-123', { title: 'Redelivery', status: { name: 'In Progress', category: 'in_progress' }, parents: [epic, initiative] }),
    'jira:DEV-140': view('DEV-140', { title: 'Locker pickup', status: { name: 'Done', category: 'done' }, parents: [epic, initiative] }),
    'jira:DEV-150': view('DEV-150', { title: 'Address change', status: { name: 'To Do', category: 'todo' }, parents: [] }),
    'jira:DEV-999': { ...view('DEV-999'), meta: { issue: 'DEV-999', ok: false, error: 'not found or no permission' } },
  },
}
// Most recent activity first, as the list sorts.
const list = [
  row('redelivery.md', { issue: 'jira:DEV-123', activity: '2026-11-09T10:00:00Z' }),
  row('notes.md', { activity: '2026-11-08T10:00:00Z' }),
  row('lockers.md', { issue: 'jira:DEV-140', activity: '2026-11-07T10:00:00Z' }),
  row('gone.md', { issue: 'jira:DEV-999', activity: '2026-11-06T10:00:00Z' }),
  row('address.md', { issue: 'jira:DEV-150', activity: '2026-11-05T10:00:00Z' }),
  row('old-plan.md', { issue: 'jira:DEV-123', archived: true, activity: '2026-11-04T10:00:00Z' }),
]

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
const throwing = {
  getItem: () => {
    throw new Error('denied')
  },
  setItem: () => {
    throw new Error('denied')
  },
  removeItem: () => {
    throw new Error('denied')
  },
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

describe('groupRows', () => {
  const names = (gs: { label: string; rows: BlueprintInfo[] }[]) => gs.map((g) => [g.label, g.rows.map((r) => r.rel)])
  it('groups by the nearest parent, with no parent, unavailable and no issue last', () => {
    const gs = groupRows(list.slice(0, 5), { by: 'parent', level: 1 }, issues.issues, 'Epic')
    expect(names(gs)).toEqual([
      ['DEV-90 · Failed-delivery handling', ['redelivery.md', 'lockers.md']],
      ['No epic', ['address.md']],
      ['Issue not available', ['gone.md']],
      ['No issue', ['notes.md']],
    ])
    expect(gs[0].url).toBe(epic.url)
  })
  it('groups by a higher level', () => {
    const gs = groupRows(list.slice(0, 5), { by: 'parent', level: 2 }, issues.issues)
    expect(gs[0].label).toBe('DEV-12 · Last-mile reliability')
  })
  it('groups by status category in a fixed order, keeping the order inside a group', () => {
    const gs = groupRows(list.slice(0, 5), { by: 'status', level: 1 }, issues.issues)
    expect(names(gs).map((g) => g[0])).toEqual(['To do', 'In progress', 'Done', 'Issue not available', 'No issue'])
  })
  it('treats an issue not fetched yet as unavailable', () => {
    const gs = groupRows([row('a.md', { issue: 'jira:DEV-7' })], { by: 'status', level: 1 }, {})
    expect(gs.map((g) => g.kind)).toEqual(['unavailable'])
  })
  it('names the levels by their most common issue type', () => {
    expect(levels(issues.issues)).toEqual([
      { level: 1, label: 'Epic' },
      { level: 2, label: 'Initiative' },
    ])
  })
})

describe('capabilities', () => {
  it('offers only what an installed plugin declares', () => {
    expect(groupOptions(undefined)).toEqual(['none'])
    expect(groupOptions({ meta: true, hierarchy: false, export: false })).toEqual(['none', 'status'])
    expect(groupOptions(issues.caps)).toEqual(['none', 'parent', 'status'])
    // A remembered choice the plugins no longer allow falls back to none.
    expect(usable({ by: 'parent', level: 2 }, { meta: true, hierarchy: false, export: false }).by).toBe('none')
  })
})

describe('the remembered choices', () => {
  it('round-trip per scope', () => {
    saveGroupBy(TEAM, { by: 'parent', level: 2 })
    saveShowArchived(TEAM, true)
    expect(loadGroupBy(TEAM)).toEqual({ by: 'parent', level: 2 })
    expect(loadGroupBy('personal').by).toBe('none')
    expect(loadShowArchived(TEAM)).toBe(true)
    expect(loadShowArchived('personal')).toBe(false)
    localStorage.setItem('kalka.group-by.' + TEAM, '{"by":"rm -rf","level":-1}')
    expect(loadGroupBy(TEAM).by).toBe('none')
  })
  it('fall back to defaults when storage throws', () => {
    expect(() => saveGroupBy(TEAM, { by: 'status', level: 1 }, throwing)).not.toThrow()
    expect(() => saveShowArchived(TEAM, true, throwing)).not.toThrow()
    expect(loadGroupBy(TEAM, throwing).by).toBe('none')
    expect(loadShowArchived(TEAM, throwing)).toBe(false)
  })
})

const status = { email: 'ola@parcels.example', folder: '/f' } as Status

describe('the Blueprints list', () => {
  const titles = () => [...host.querySelectorAll('tbody tr:not(.group-row) td:first-child > a')].map((a) => a.textContent)
  const headers = () => [...host.querySelectorAll('tr.group-row th')].map((th) => th.textContent)

  it('has no Group by without a plugin that declares it', async () => {
    await render(<Blueprints list={list.slice(0, 5)} status={status} scope={TEAM} reload={() => {}} />)
    expect(host.querySelector('select[aria-label="Group by"]')).toBeNull()
    // The raw key is still shown.
    expect(host.querySelector('.badge.issue')!.textContent).toBe('jira:DEV-123')
  })
  it('groups by parent and status, with headers, and remembers the choice', async () => {
    await render(<Blueprints list={list} status={status} scope={TEAM} reload={() => {}} issues={issues} />)
    const sel = host.querySelector<HTMLSelectElement>('select[aria-label="Group by"]')!
    expect([...sel.options].map((o) => o.value)).toEqual(['none', 'parent', 'status'])
    await act(async () => {
      sel.value = 'parent'
      sel.dispatchEvent(new Event('change', { bubbles: true }))
    })
    expect(headers()).toEqual(['DEV-90 · Failed-delivery handling · 2', 'No epic · 1', 'Issue not available · 1', 'No issue · 1'])
    expect(host.querySelector<HTMLAnchorElement>('tr.group-row a')!.href).toBe(epic.url)
    expect(titles()).toEqual(['redelivery', 'lockers', 'address', 'gone', 'notes'])
    expect(loadGroupBy(TEAM)).toEqual({ by: 'parent', level: 1 })
    // The level picker offers the levels above.
    expect([...host.querySelectorAll('select[aria-label="Parent level"] option')].map((o) => o.textContent)).toEqual(['Epic', 'Initiative'])
  })
  it('hides archived blueprints until asked, and remembers it', async () => {
    await render(<Blueprints list={list} status={status} scope={TEAM} reload={() => {}} issues={issues} />)
    expect(titles()).not.toContain('old-plan')
    const box = [...host.querySelectorAll('label')].find((l) => l.textContent?.startsWith('Show archived'))!
    expect(box.textContent).toBe('Show archived (1)')
    await act(async () => box.querySelector('input')!.click())
    expect(titles()).toContain('old-plan')
    expect(loadShowArchived(TEAM)).toBe(true)
  })
})

describe('the rail', () => {
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

  it('follows the list grouping with headers, and leaves out archived ones', async () => {
    saveGroupBy(TEAM, { by: 'status', level: 1 })
    await render(<DocRail doc={doc('redelivery.md')} scope={TEAM} where="parcel-ops" list={list} view="doc" reloadKey={0} issues={issues} />)
    const items = [...host.querySelectorAll('.doc-rail-list li')].map((li) => (li.classList.contains('doc-rail-group') ? '# ' : '') + li.textContent?.replace('needs you', ''))
    expect(items).toEqual(['# To do', 'address', '# In progress', 'redelivery', '# Done', 'lockers', '# Issue not available', 'gone', '# No issue', 'notes'])
  })
  it('lists an archived document while it is open', async () => {
    await render(<DocRail doc={doc('old-plan.md')} scope={TEAM} where="parcel-ops" list={list} view="doc" reloadKey={0} issues={issues} />)
    expect(host.querySelector('.doc-rail-list a[aria-current="page"]')!.textContent).toContain('old-plan')
  })
})

describe('the document bar', () => {
  const doc = (more: Partial<Doc>): Doc =>
    ({ path: '/f/parcel-ops/r.md', rel: 'r.md', title: 'Redelivery', role: 'owner', scope: TEAM, sections: [], headings: [], entries: [], problems: [], refs: {}, traces: [], ...more }) as unknown as Doc

  it('shows the raw key without a plugin', async () => {
    const iv: IssueView = { issue: 'jira:DEV-123', scheme: 'jira', payload: 'DEV-123' }
    await render(<DocBar doc={doc({ issue: iv })} scope={TEAM} view="doc" traceIndex={0} reload={() => {}} />)
    expect(host.querySelector('.issue-chip')!.textContent).toBe('DEV-123')
    expect(host.querySelector('.issue-chip a')).toBeNull()
    expect(host.querySelector('.docbar-export')).toBeNull()
  })
  it('shows title, status, parents and the export control', async () => {
    const iv = { ...issues.issues['jira:DEV-123'], stale: true, as_of: '2026-11-19T10:00:00Z' }
    iv.meta = { ...iv.meta!, url: 'https://jira.example/browse/DEV-123' }
    await render(
      <DocBar doc={doc({ issue: iv, export: { can: false, why: 'only a released version is exported' } })} scope={TEAM} view="doc" traceIndex={0} reload={() => {}} />,
    )
    const chip = host.querySelector('.issue-chip')!
    expect(chip.querySelector('a')!.getAttribute('href')).toBe('https://jira.example/browse/DEV-123')
    expect(chip.querySelector('.issue-title')!.textContent).toBe('Redelivery')
    expect(chip.querySelector('.issue-status.in_progress')!.textContent).toBe('In Progress')
    expect(chip.querySelector('.issue-asof')!.textContent).toMatch(/^as of /)
    expect([...host.querySelectorAll('.issue-parents li')].map((l) => l.textContent)).toEqual(['DEV-12', 'DEV-90'])
    const btn = host.querySelector<HTMLButtonElement>('.docbar-export')!
    expect(btn.disabled).toBe(true)
    expect(btn.title).toBe('only a released version is exported')
    expect(btn.dataset.action).toBe('doc.export')
  })
})
