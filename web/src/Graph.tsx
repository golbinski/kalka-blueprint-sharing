// The graph canvas (spec 6.18, 11.1): the lens around the open document,
// drawn. Read-only and computed: the graph is kalka graph's, the layout is
// graphLayout's, and only which cards are open is kept, in the page's
// storage, per focus. Everything in order is drawn quietly; what needs a
// person stands out, and the side panel lists it.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { invElementId } from './Aids'
import { api, type Doc, type Graph, type GraphNode, type GoalState, type OpenItem, type Trace } from './api'
import { StateBadge } from './Goals'
import { CARD_W, criteriaLeadingTo, emptyExpansion, layoutGraph, type CardBox, type Expansion, type ItemBox } from './graphLayout'
import { badgeText } from './Trace'
import { atHref, docHref, graphHref, localTime, plural, traceHref } from './util'

const STORE = 'kalka.graph.'

/** loadExpansion is what was open last time on this focus; the default
 * opens the focus to its items. A storage failure gives the default. */
export function loadExpansion(focus: string, storage?: Storage): Expansion {
  try {
    const raw = (storage ?? globalThis.localStorage).getItem(STORE + focus)
    if (raw) {
      const v = JSON.parse(raw) as Partial<Expansion>
      return { ...emptyExpansion(), ...v }
    }
  } catch {
    /* the default */
  }
  return { ...emptyExpansion(), cards: [focus] }
}

export function saveExpansion(focus: string, e: Expansion, storage?: Storage): void {
  try {
    ;(storage ?? globalThis.localStorage).setItem(STORE + focus, JSON.stringify(e))
  } catch {
    /* not kept: the default next time */
  }
}

const toggle = (xs: string[], x: string) => (xs.includes(x) ? xs.filter((y) => y !== x) : [...xs, x])
const domId = (id: string) => 'g-' + id.replace(/[^A-Za-z0-9_-]/g, '-')

/** stepTrace is enough of a trace for badgeText to word a step's divergence. */
function stepTrace(g: Graph, n: GraphNode): Trace {
  const nodes = g.nodes.filter((s) => s.card === n.card && s.kind === 'step' && s.trace === n.trace).map((s) => ({ id: s.item, lane: s.lane }))
  return { lanes: n.lanes ?? [], nodes } as unknown as Trace
}

const MARKER_WORDS: Record<string, string> = {
  uncovered: 'no trace step covers it',
  unguarded: 'no trace step guards it',
  unverified: 'nothing says how it is verified',
  deviation: 'a deviation',
  'block-errors': 'a block has errors and was not read',
  ambiguous: 'ambiguous: two documents claim this domain version',
  inconsistent: 'inconsistent ancestor versions on two paths',
  'not-here': 'not here',
  archived: 'archived',
  closed: 'closed',
}

/** markerWords says what a node's markers mean, divergences in the Trace
 * view's words. */
export function markerWords(g: Graph, n: GraphNode): string[] {
  const out: string[] = []
  for (const m of n.markers ?? []) {
    if (m.startsWith('divergence:')) continue
    out.push(MARKER_WORDS[m] ?? m)
  }
  for (const d of n.divergences ?? []) out.push(badgeText(d, stepTrace(g, n)))
  return out
}

/** openText is an open item in words. */
export function openText(g: Graph, o: OpenItem): string {
  const n = g.nodes.find((x) => x.id === o.node)
  if (n?.kind === 'step') {
    const d = (n.divergences ?? []).find((x) => x.class === o.kind)
    if (d) return `step ${n.item}: ${badgeText(d, stepTrace(g, n))}`
  }
  return o.text
}

const quiet = new Set(['archived', 'closed'])
const loud = (n: GraphNode) => (n.markers ?? []).some((m) => !quiet.has(m))

function cardLabel(n: GraphNode): string {
  if (n.kind === 'domain') return `${n.domain}@v${n.version}`
  const key = n.issue ? n.issue.replace(/^[a-z][a-z0-9-]*:/, '') : ''
  return key ? `${key} ${n.title ?? ''}` : (n.title ?? '')
}

function cardHref(n: GraphNode): string {
  if (!n.rel) return ''
  return n.kept ? docHref(n.rel, n.version) : docHref(n.rel)
}

/** itemHref opens an item in the document or the trace view. */
function itemHref(card: GraphNode | undefined, n: GraphNode): { href: string; label: string } | null {
  if (!card?.rel) return null
  if (n.kind === 'step' && n.trace) return { href: traceHref(card.rel, n.trace - 1) + '?node=' + encodeURIComponent(n.item ?? ''), label: 'Open in Trace' }
  const at = n.kind === 'goal' || n.kind === 'criterion' ? 'goal-' + n.item : n.kind === 'invariant' ? invElementId(n.item ?? '') : ''
  const version = card.kept ? card.version : undefined
  return { href: at ? atHref(card.rel, at, version) : docHref(card.rel, version), label: 'Open in Document' }
}

function Card({
  c,
  exp,
  selected,
  onToggle,
  onSelect,
  onExpand,
}: {
  c: CardBox
  exp: Expansion
  selected: string
  onToggle: (id: string) => void
  onSelect: (id: string) => void
  onExpand: (e: Expansion) => void
}) {
  const n = c.node
  const cls = ['gcard', 'band-' + n.band, n.focus && 'focus', n.placeholder && 'placeholder', (n.markers ?? []).includes('archived') && 'archived', selected === n.id && 'selected']
    .filter(Boolean)
    .join(' ')
  const chips: string[] = []
  if ((n.open ?? 0) > 0) chips.push(plural(n.open!, 'open item'))
  if ((n.errors ?? 0) > 0) chips.push(plural(n.errors!, 'error'))
  if ((n.warnings ?? 0) > 0) chips.push(plural(n.warnings!, 'warning'))
  const talk = [
    (n.prose ?? 0) > 0 && plural(n.prose!, 'comment') + ' in prose',
    (n.general ?? 0) > 0 && plural(n.general!, 'general comment'),
    (n.orphaned ?? 0) > 0 && plural(n.orphaned!, 'comment') + ' on removed sections',
  ].filter(Boolean) as string[]
  return (
    <div className={cls} style={{ left: c.x, top: c.y, width: CARD_W, height: c.frameH }} id={domId(n.id)}>
      <button
        type="button"
        className="gcard-head"
        aria-expanded={n.placeholder ? undefined : c.expanded}
        onClick={() => {
          onSelect(n.id)
          if (!n.placeholder) onToggle(n.id)
        }}
      >
        <span className="gcard-kind muted small">
          {n.kind === 'domain' ? 'Domain' : n.child ? 'Child' : n.focus ? 'This document' : 'Plan node'}
          {n.owner && <> · {n.owner.split('@')[0]}</>}
          {(n.new ?? 0) > 0 && <span className="gnew">{n.new} new</span>}
        </span>
        <span className="gcard-title">{n.placeholder ? (n.kind === 'domain' ? `${n.domain}@v${n.version}` : n.title) : cardLabel(n)}</span>
        <span className="gcard-chips small">
          {n.placeholder && <span className="tbadge div">not here</span>}
          {(n.markers ?? []).includes('closed') && <span className="tbadge ok">closed</span>}
          {(n.markers ?? []).includes('archived') && <span className="tbadge outline">archived</span>}
          {(n.markers ?? []).includes('ambiguous') && <span className="tbadge div">ambiguous</span>}
          {(n.markers ?? []).includes('inconsistent') && <span className="tbadge div">inconsistent versions</span>}
          {(n.markers ?? []).includes('block-errors') && <span className="tbadge danger">block errors</span>}
          {chips.map((s) => (
            <span key={s} className={'tbadge ' + (s.includes('error') ? 'danger' : 'div')}>
              {s}
            </span>
          ))}
        </span>
        {talk.length > 0 && <span className="gcard-talk muted small">{talk.join(' · ')}</span>}
      </button>
      {c.items.map((it) => (
        <Item key={it.id} it={it} exp={exp} selected={selected} onSelect={onSelect} onExpand={onExpand} />
      ))}
      {c.more > 0 && (
        <button type="button" className="gitem gmore link" style={{ top: c.items.length > 0 ? c.items[c.items.length - 1].y + 30 : 84 }} onClick={() => onExpand({ ...exp, more: [...exp.more, n.id] })}>
          +{c.more} more
        </button>
      )}
      {c.expanded && c.otherSteps > 0 && (
        <button type="button" className="link small gother" style={{ top: c.otherY }} aria-expanded={exp.steps.includes(n.id)} onClick={() => onExpand({ ...exp, steps: toggle(exp.steps, n.id) })}>
          {exp.steps.includes(n.id) ? 'Hide' : 'Show'} {plural(c.otherSteps, 'step')} under no criterion
        </button>
      )}
    </div>
  )
}

function Item({
  it,
  exp,
  selected,
  onSelect,
  onExpand,
}: {
  it: ItemBox
  exp: Expansion
  selected: string
  onSelect: (id: string) => void
  onExpand: (e: Expansion) => void
}) {
  const n = it.node
  const criterion = n.kind === 'criterion'
  const open = criterion && exp.criteria.includes(n.id)
  return (
    <button
      type="button"
      id={domId(n.id)}
      className={'gitem kind-' + n.kind + (loud(n) || n.state === 'unclaimed' ? ' attention' : '') + (selected === n.id ? ' selected' : '')}
      style={{ top: it.y, left: it.x, width: it.w, height: it.h }}
      aria-expanded={criterion ? open : undefined}
      title={n.text}
      onClick={() => {
        onSelect(n.id)
        if (criterion) onExpand({ ...exp, criteria: toggle(exp.criteria, n.id) })
      }}
    >
      <span className="mono b">{n.kind === 'term' ? '' : n.item}</span>
      <span className="gitem-text">{n.kind === 'term' ? <b>{n.item}</b> : n.text}</span>
      {(n.threads ?? 0) > 0 && <span className="gcount muted small">{n.threads}</span>}
      {(n.new ?? 0) > 0 && <span className="gdot" title="New since you last looked" />}
      {(n.kind === 'goal' || n.kind === 'criterion') && n.state && n.state !== 'open' && <StateBadge state={n.state as GoalState} />}
      {(n.markers ?? []).filter((m) => m in MARKER_WORDS).map((m) => (
        <span key={m} className="tbadge div small" title={MARKER_WORDS[m]}>
          {m}
        </span>
      ))}
    </button>
  )
}

function Step({ g, it, selected, onSelect }: { g: Graph; it: ItemBox; selected: string; onSelect: (id: string) => void }) {
  const n = it.node
  const words = markerWords(g, n)
  return (
    <button
      type="button"
      id={domId(n.id)}
      className={'gstep' + (words.length > 0 ? ' attention' : '') + (selected === n.id ? ' selected' : '')}
      style={{ left: it.x, top: it.y, width: it.w, height: it.h }}
      title={n.text}
      onClick={() => onSelect(n.id)}
    >
      <span className="gstep-head">
        <span className="mono b">{n.item}</span>
        <span className="muted small">{n.lane}</span>
        {(n.threads ?? 0) > 0 && <span className="gcount muted small">{n.threads}</span>}
        {words.map((w) => (
          <span key={w} className="tbadge div small">
            {w}
          </span>
        ))}
      </span>
      <span className="gstep-text small">{n.text}</span>
      {n.ref && <span className="gref mono small">{n.ref}</span>}
    </button>
  )
}

function Detail({ g, doc, id, onFocus }: { g: Graph; doc: Doc; id: string; onFocus: (n: GraphNode) => void }) {
  const n = g.nodes.find((x) => x.id === id)
  if (!n) return null
  const card = n.card ? g.nodes.find((x) => x.id === n.card) : n
  const words = markerWords(g, n)
  if (!n.card) {
    const href = cardHref(n)
    return (
      <section className="gdetail" aria-label="Selected document">
        <div className="muted small">{n.kind === 'domain' ? 'Domain node' : 'Plan node'}</div>
        <h3>{n.placeholder ? (n.kind === 'domain' ? `${n.domain}@v${n.version}` : n.title) : cardLabel(n)}</h3>
        {n.placeholder ? (
          <p className="muted">Not held here: this copy does not have the document, so only what the reference says is known.</p>
        ) : (
          <>
            {n.owner && <div className="small">Owner {n.owner}</div>}
            {words.length > 0 && <div className="small">{words.join(' · ')}</div>}
            <div className="row center gap">
              {href && <a href={href}>Open in Document</a>}
              {!n.focus && n.rel && (
                <button type="button" onClick={() => onFocus(n)}>
                  Focus here
                </button>
              )}
            </div>
          </>
        )}
      </section>
    )
  }
  const link = itemHref(card, n)
  const threads = card?.focus && n.anchor ? doc.entries.filter((e) => e.anchor === n.anchor && e.kind !== 'release' && e.kind !== 'budded') : []
  return (
    <section className="gdetail" aria-label="Selected item">
      <div className="muted small">
        {n.kind} {n.kind === 'step' && n.lane ? `· ${n.lane} lane` : ''} · {card ? cardLabel(card) : ''}
      </div>
      <h3>
        {n.kind !== 'term' && <span className="mono">{n.item} </span>}
        {n.kind === 'term' ? n.item : n.text}
      </h3>
      {n.kind === 'term' && <p>{n.text}</p>}
      {(n.kind === 'goal' || n.kind === 'criterion') && n.state && (
        <div>
          <StateBadge state={n.state as GoalState} />
        </div>
      )}
      {n.kind === 'invariant' && n.state && (
        <div>
          <span className="tbadge outline">{n.state}</span>
        </div>
      )}
      {words.length > 0 && <div className="small">{words.join(' · ')}</div>}
      {n.ref && <div className="mono small">{n.ref}</div>}
      {(n.threads ?? 0) > 0 && <div className="small">{plural(n.threads!, 'thread')}</div>}
      {threads.length > 0 && (
        <ul className="plain gthreads">
          {threads.map((e) => (
            <li key={e.id} className={'small kind-' + e.kind}>
              <b>{e.author.split('@')[0]}</b> · {localTime(e.timestamp)}
              {e.kind === 'deviation' && <span className="tbadge div">deviation</span>}
              <div>{e.body.length > 240 ? e.body.slice(0, 240) + '…' : e.body}</div>
            </li>
          ))}
        </ul>
      )}
      {link && <a href={link.href}>{link.label}</a>}
    </section>
  )
}

/** OpenList is the side panel's list of what needs a person, grouped by
 * document: the accessible equivalent of the overlay. */
function OpenList({ g, onReveal }: { g: Graph; onReveal: (o: OpenItem) => void }) {
  const groups: { card: GraphNode; items: OpenItem[] }[] = []
  for (const o of g.open) {
    let grp = groups.find((x) => x.card.id === o.card)
    if (!grp) {
      const card = g.nodes.find((x) => x.id === o.card)
      if (!card) continue
      grp = { card, items: [] }
      groups.push(grp)
    }
    grp.items.push(o)
  }
  return (
    <section aria-labelledby="gopen-title">
      <h2 id="gopen-title" className="gopen-title" aria-live="polite">
        {g.open.length === 0 ? 'Nothing needs attention' : `${plural(g.open.length, 'place needs', 'places need')} attention`}
        <span className="muted small normal"> · in the documents held here</span>
      </h2>
      {groups.map((grp) => (
        <div key={grp.card.id} className="gopen-group">
          <div className="small b">{cardLabel(grp.card)}</div>
          <ul className="plain">
            {grp.items.map((o) => (
              <li key={o.node + o.kind}>
                <button type="button" className="link" onClick={() => onReveal(o)}>
                  {openText(g, o)}
                </button>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </section>
  )
}

function wireClass(kind: string, dashed?: boolean, marker?: string): string {
  const base = kind === 'parent' || kind === 'domain-parent' || kind === 'pin' ? 'gw-struct' : kind === 'realizes' || kind === 'covers' || kind === 'step-realizes' ? 'gw-realizes' : 'gw-link'
  return [base, 'gw-' + kind, dashed && 'gw-dashed', marker && 'gw-' + marker].filter(Boolean).join(' ')
}

export function GraphPage({ doc }: { doc: Doc }) {
  const [g, setG] = useState<Graph | null>(null)
  const [err, setErr] = useState('')
  const [exp, setExp] = useState<Expansion>(emptyExpansion)
  const [selected, setSelected] = useState('')
  const [reveal, setReveal] = useState('')
  const focusRef = useRef('')

  useEffect(() => {
    let live = true
    api.graph({ path: doc.rel }).then(
      (r) => {
        if (!live) return
        setG(r.graph)
        setErr('')
        if (focusRef.current !== r.graph.focus) {
          focusRef.current = r.graph.focus
          setExp(loadExpansion(r.graph.focus))
          setSelected('')
        }
      },
      (e: Error) => live && setErr(e.message),
    )
    return () => {
      live = false
    }
  }, [doc])

  const update = useCallback((e: Expansion) => {
    setExp(e)
    if (focusRef.current) saveExpansion(focusRef.current, e)
  }, [])

  const layout = useMemo(() => (g ? layoutGraph(g, exp) : null), [g, exp])

  // Scroll to and show what an open item named, once it is drawn.
  useEffect(() => {
    if (!reveal) return
    const el = document.getElementById(domId(reveal))
    if (el) {
      el.scrollIntoView({ block: 'center', inline: 'center' })
      el.focus()
      setReveal('')
    }
  }, [reveal, layout])

  if (err) return <div className="page"><div className="error-box">{err}</div></div>
  if (!g || !layout) return <div className="page muted">Loading the graph…</div>

  const onReveal = (o: OpenItem) => {
    const n = g.nodes.find((x) => x.id === o.node)
    const next = { ...exp, cards: exp.cards.includes(o.card) ? exp.cards : [...exp.cards, o.card] }
    if (n?.kind === 'step') {
      const crit = criteriaLeadingTo(g, n.id)
      if (crit.length > 0) next.criteria = exp.criteria.includes(crit[0]) ? exp.criteria : [...exp.criteria, crit[0]]
      else if (!next.steps.includes(o.card)) next.steps = [...next.steps, o.card]
    }
    update(next)
    setSelected(o.node)
    setReveal(o.node)
  }
  const onFocus = (n: GraphNode) => {
    location.hash = graphHref({ rel: n.rel ?? '', kalka_id: n.kalka_id, scope: n.scope }).slice(1)
  }
  const bands = { domain: layout.cards.some((c) => c.node.band === 'domain'), plan: layout.cards.some((c) => c.node.band === 'plan') }

  return (
    <div className="trace-page graph-page">
      <div className="trace-main">
        <div className="trace-head">
          <div>
            <div className="trace-title">Graph</div>
            <div className="muted small">
              {g.alone
                ? 'This document is not part of a graph: it has no parent, no domain, no goals and no children.'
                : 'The plan and domain paths of this document and its children. Select a card to open it; a criterion opens the trace steps that cover it.'}
            </div>
          </div>
          <div className="legend">
            <span>
              <span className="legend-line struct" /> tree and pins
            </span>
            <span>
              <span className="legend-line realizes" /> realizes and covers
            </span>
            <span>
              <span className="legend-line link" /> affects, guards, refines
            </span>
          </div>
        </div>
        <div className="graph-area">
          <div className="graph-bands small muted" style={{ width: layout.width }}>
            {bands.domain && <span>Domain tree</span>}
            {bands.plan && <span className="grow" />}
            {bands.plan && <span>Plan tree</span>}
          </div>
          <div className="graph-canvas" style={{ width: layout.width + 24, height: layout.height + 24 }}>
            <svg className="tedges" width={layout.width + 24} height={layout.height + 24} aria-hidden="true">
              {layout.wires.map((w, i) => (
                <path key={i} className={wireClass(w.edge.kind, w.edge.dashed, w.edge.marker)} d={w.d} />
              ))}
            </svg>
            {layout.wires
              .filter((w) => w.edge.marker === 'parent-newer' || w.edge.marker === 'cycle')
              .map((w, i) => (
                <span key={'l' + i} className={'gw-label tbadge ' + (w.edge.marker === 'cycle' ? 'danger' : 'div')} style={{ left: w.lx, top: w.ly }}>
                  {w.edge.marker === 'cycle' ? 'cycle' : 'parent newer'}
                </span>
              ))}
            {layout.cards.map((c) => (
              <div key={c.id}>
                <Card c={c} exp={exp} selected={selected} onSelect={setSelected} onExpand={update} onToggle={(id) => update({ ...exp, cards: toggle(exp.cards, id) })} />
                {c.groups.map((grp) => (
                  <div key={grp.under || 'other'} className="ggroup muted small" style={{ left: c.x + CARD_W + 24, top: c.y + grp.y }}>
                    {grp.label}
                  </div>
                ))}
                {c.steps.map((s) => (
                  <Step key={s.id} g={g} it={{ ...s, x: c.x + s.x, y: c.y + s.y }} selected={selected} onSelect={setSelected} />
                ))}
              </div>
            ))}
          </div>
        </div>
      </div>
      <aside className="trace-panel graph-panel">
        {selected && <Detail g={g} doc={doc} id={selected} onFocus={onFocus} />}
        <OpenList g={g} onReveal={onReveal} />
      </aside>
    </div>
  )
}
