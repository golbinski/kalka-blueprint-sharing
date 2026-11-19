// Navigation inside a document (spec 11.1): the document bar with the
// breadcrumb and the Document / Trace tabs, and the rail listing the
// documents of the section the open one belongs to.
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { api, type BlueprintInfo, type Doc, type DomainRow, type SeedRow, type WaitingItem } from './api'
import { loadCollapsed, NARROW, railHref, saveCollapsed, type DocView } from './rail'
import { blueprintRows, SECTION_LABEL, sectionOf, type Section } from './section'
import { docHref, scopeHref, traceHref, type ScopedView } from './util'

/** SectionsNav is the top bar's sections; active is activeSection's answer. */
export function SectionsNav({ scope, personal, active, needs }: { scope: string; personal: boolean; active: string; needs: number }) {
  const link = (view: ScopedView, label: string) => (
    <a href={scopeHref(scope, view)} className={active === view ? 'active' : ''} aria-current={active === view ? 'page' : undefined}>
      {label}
    </a>
  )
  return (
    <nav className="nav" aria-label="Sections">
      <a href="#/needs" className={active === 'needs' ? 'active' : ''} aria-current={active === 'needs' ? 'page' : undefined}>
        Needs you{needs > 0 && <span className="count">{needs}</span>}
      </a>
      {link('list', 'Blueprints')}
      {link('seeds', 'Seeds')}
      {!personal && link('domains', 'Domains')}
      {!personal && link('team', 'Team')}
    </nav>
  )
}

/** DocBar is the bar under the top bar while a document is open. */
export function DocBar({ doc, scope, view, traceIndex }: { doc: Doc; scope: string; view: DocView; traceIndex: number }) {
  const section = sectionOf(doc)
  return (
    <nav className="docbar" aria-label="Document">
      <ol className="crumbs">
        <li>
          <a href={scopeHref(scope, section)}>{SECTION_LABEL[section]}</a>
        </li>
        <li aria-current="page" title={doc.title}>
          {doc.title}
        </li>
      </ol>
      <span className="grow" />
      <div className="tabs" role="group" aria-label="Views">
        <a className={view === 'doc' || view === 'canvas' ? 'active' : ''} href={docHref(doc.rel)}>
          Document
        </a>
        {doc.traces.length > 0 ? (
          <a className={view === 'trace' ? 'active' : ''} href={traceHref(doc.rel, view === 'trace' ? traceIndex : 0)}>
            Trace
          </a>
        ) : (
          <span className="disabled" title="This blueprint has no trace blocks">
            Trace
          </span>
        )}
      </div>
    </nav>
  )
}

/** A row of the rail. */
export interface RailRow {
  rel: string
  title: string
  traces?: number
  /** Unread comments by others, or something waiting on you. */
  needs: boolean
}

// railRows are a section's documents in the order of its list page, with
// the needs-you marker from the list rows (unread) and the waiting items.
export function railRows(
  section: Section,
  scope: string,
  list: BlueprintInfo[],
  waiting: WaitingItem[],
  seeds: SeedRow[] | null,
  domains: DomainRow[] | null,
): RailRow[] {
  const byRel = new Map(list.map((b) => [b.rel, b]))
  const waits = new Set(waiting.flatMap((w) => (w.path ? [w.path] : [])))
  const row = (rel: string, title?: string): RailRow => {
    const b = byRel.get(rel)
    return { rel, title: title || b?.title || rel, traces: b?.traces, needs: (b?.new ?? 0) > 0 || (!!b && waits.has(b.path)) }
  }
  if (section === 'seeds') return (seeds ?? []).map((s) => row(s.rel, s.title))
  if (section === 'domains') return (domains ?? []).map((d) => row(d.rel, d.title))
  return blueprintRows(list, scope).map((b) => row(b.rel, b.title))
}

// useCollapsed: the remembered choice on a wide window; on a narrow one
// the rail starts collapsed and opening it there is not remembered, so
// the wide window keeps its own choice.
function useCollapsed(): [boolean, () => void] {
  const [wide, setWide] = useState(() => window.innerWidth >= NARROW)
  const [stored, setStored] = useState(loadCollapsed)
  const [narrowOpen, setNarrowOpen] = useState(false)
  useEffect(() => {
    const f = () => {
      const w = window.innerWidth >= NARROW
      setWide(w)
      if (!w) setNarrowOpen(false)
    }
    window.addEventListener('resize', f)
    return () => window.removeEventListener('resize', f)
  }, [])
  const collapsed = wide ? stored : !narrowOpen
  const toggle = () => {
    if (wide) {
      saveCollapsed(!stored)
      setStored(!stored)
    } else setNarrowOpen(!narrowOpen)
  }
  return [collapsed, toggle]
}

/** DocRail lists the documents of the open document's section. */
export function DocRail({ doc, scope, where, list, view, reloadKey }: { doc: Doc; scope: string; where: string; list: BlueprintInfo[]; view: DocView; reloadKey: number }) {
  const section = sectionOf(doc)
  const [collapsed, toggle] = useCollapsed()
  const [seeds, setSeeds] = useState<SeedRow[] | null>(null)
  const [domains, setDomains] = useState<DomainRow[] | null>(null)
  const [waiting, setWaiting] = useState<WaitingItem[]>([])
  useEffect(() => {
    let live = true
    if (section === 'seeds') api.seeds(scope).then((l) => live && setSeeds(l), () => {})
    if (section === 'domains') api.domains(scope).then((l) => live && setDomains(l), () => {})
    api.waiting(scope).then((l) => live && setWaiting(l), () => {})
    return () => {
      live = false
    }
  }, [section, scope, reloadKey, list])
  const rows = railRows(section, scope, list, waiting, seeds, domains)
  const current = useRef<HTMLAnchorElement>(null)
  // Keep the open document in sight when the list is long.
  useLayoutEffect(() => {
    current.current?.scrollIntoView?.({ block: 'nearest' })
  }, [doc.rel, collapsed, rows.length])
  const label = `${SECTION_LABEL[section]} in ${where}`
  return (
    <nav className={'doc-rail' + (collapsed ? ' collapsed' : '')} aria-label={label}>
      <div className="doc-rail-head">
        {!collapsed && (
          <a className="doc-rail-title" href={scopeHref(scope, section)}>
            {SECTION_LABEL[section]}
          </a>
        )}
        <button
          type="button"
          className="icon doc-rail-toggle"
          aria-expanded={!collapsed}
          aria-controls="doc-rail-list"
          aria-label={collapsed ? `Show ${label}` : `Hide ${label}`}
          title={collapsed ? 'Show the list' : 'Hide the list'}
          onClick={toggle}
        >
          {collapsed ? '»' : '«'}
        </button>
      </div>
      {!collapsed && (
        <ul id="doc-rail-list" className="plain doc-rail-list">
          {rows.map((r) => {
            const open = r.rel === doc.rel
            return (
              <li key={r.rel}>
                <a ref={open ? current : undefined} href={railHref(r, view)} aria-current={open ? 'page' : undefined} className={open ? 'open' : ''}>
                  <span className="doc-rail-name">{r.title}</span>
                  {r.needs && (
                    <span className="doc-rail-needs" title="Needs you">
                      <span className="sr-only">needs you</span>
                    </span>
                  )}
                </a>
              </li>
            )
          })}
          {rows.length === 0 && <li className="muted small doc-rail-empty">Nothing else here.</li>}
        </ul>
      )}
    </nav>
  )
}
