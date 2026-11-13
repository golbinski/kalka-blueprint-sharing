// Trace view (spec 11.1, 6.10): a trace block as lanes side by side, the
// divergences the daemon computed, and a side panel with node details.
// Layout is computed in traceLayout.ts; nothing is saved.
import { useEffect, useMemo, useRef, useState } from 'react'
import type { Divergence, Doc, Entry, RefView, Trace, TraceNode } from './api'
import { Composer, EntryCard } from './Review'
import { RefBox, RefLayer } from './refs'
import { CARD_H, CARD_W, layout, type Layout } from './traceLayout'
import { plural, traceHref } from './util'

const NEUTRAL = new Set(['unverified', 'stale'])

// graphDivs are the divergences that make a node orange.
function graphDivs(t: Trace, id: string): Divergence[] {
  return t.divergences.filter((d) => d.node === id && !NEUTRAL.has(d.class))
}

export function badgeText(d: Divergence): string {
  switch (d.class) {
    case 'merged':
    case 'expanded':
      return `${d.class} · ${(d.nodes ?? []).join(' + ')}`
    case 'scattered':
    case 'reordered':
      return `${d.class} in ${d.lane}`
    case 'stale':
      return d.detail && d.detail.length <= 32 ? d.detail : 'stale'
    case 'unverified':
      return d.detail === 'resolving…' ? 'resolving…' : 'unverified'
    default:
      return d.class
  }
}

// refLabel is how a node shows its reference: the plugin's title when it
// resolved, else the payload.
function refLabel(ref: string, r?: RefView): string {
  if (r?.ok && r.title) return r.title
  return r?.payload || ref
}

// nodeEntries are the top-level entries anchored to a node of this block.
export function nodeEntries(doc: Doc, t: Trace, id?: string): Entry[] {
  if (!t.section) return []
  const ids = new Set((t.nodes ?? []).map((n) => n.id))
  return doc.entries.filter((e) => e.section === t.section && e.node && ids.has(e.node) && (!id || e.node === id) && !e.reply_to)
}

function NodeCard({
  t,
  p,
  doc,
  selected,
  onSelect,
}: {
  t: Trace
  p: Layout['placed'] extends Map<string, infer V> ? V : never
  doc: Doc
  selected: boolean
  onSelect?: (id: string) => void
}) {
  const n = p.node
  const divs = graphDivs(t, n.id)
  const neutral = t.divergences.filter((d) => d.node === n.id && NEUTRAL.has(d.class))
  const ref = n.ref ? doc.refs[n.ref] : undefined
  const comments = nodeEntries(doc, t, n.id).length
  const cls = ['tnode', divs.length ? 'div' : '', selected ? 'selected' : '', onSelect ? '' : 'static'].join(' ')
  const style = { left: p.x, top: p.y, width: CARD_W, height: p.h }
  const body = (
    <span className="tnode-in">
      <span className="tnode-title">
        <span className="tid">{n.id}</span> <b>{n.text}</b>
      </span>
      {n.ref && (
        <span className={'tref' + (ref?.ok ? '' : ' unresolved')} title={ref?.ok ? n.ref : `${n.ref}: ${ref?.error ?? 'not resolved'}`}>
          {refLabel(n.ref, ref)}
        </span>
      )}
      <span className="tbadges">
        {divs.map((d, i) => (
          <span key={i} className="tbadge div">
            {badgeText(d)}
          </span>
        ))}
        {neutral.map((d, i) => (
          <span key={'n' + i} className="tbadge outline" title={d.detail}>
            {badgeText(d)}
          </span>
        ))}
        {comments > 0 && <span className="tbadge count">{plural(comments, 'comment')}</span>}
      </span>
    </span>
  )
  if (!onSelect)
    return (
      <div className={cls} style={style}>
        {body}
      </div>
    )
  return (
    <button type="button" className={cls} style={style} onClick={() => onSelect(n.id)} aria-pressed={selected}>
      {body}
    </button>
  )
}

// TraceDiagram draws one valid block. scale shrinks it; onSelect makes the
// nodes buttons.
export function TraceDiagram({
  t,
  doc,
  scale = 1,
  selected,
  onSelect,
}: {
  t: Trace
  doc: Doc
  scale?: number
  selected?: string
  onSelect?: (id: string) => void
}) {
  const l = useMemo(() => layout(t), [t])
  return (
    <div className="tdiagram-outer" style={{ width: l.width * scale, height: l.height * scale }}>
      <div className="tdiagram" style={{ width: l.width, height: l.height, transform: `scale(${scale})` }}>
        {l.lanes.map((ln) => (
          <div key={ln.name} className="tlane" style={{ left: ln.x, width: 440, height: l.height }}>
            <div className="tlane-name">{ln.name.toUpperCase()}</div>
          </div>
        ))}
        <svg className="tedges" width={l.width} height={l.height} aria-hidden="true">
          <defs>
            <marker id="tarrow" orient="auto" markerWidth="8" markerHeight="8" refX="4" refY="4">
              <path d="M0 0 L8 4 L0 8 Z" className="tarrow-head" />
            </marker>
          </defs>
          {l.edges.map((e, i) => (
            <path key={i} d={e.d} className={e.kind === 'flow' ? 'tflow' : 'trealizes'} markerEnd={e.kind === 'flow' ? 'url(#tarrow)' : undefined} />
          ))}
        </svg>
        {l.edges
          .filter((e) => e.label)
          .map((e, i) => (
            <div key={i} className="tedge-label" style={{ left: e.lx, top: (e.ly ?? 0) - 8 }}>
              {e.label}
            </div>
          ))}
        {l.gaps.map((g, i) => (
          <div key={i} className="tgap" style={{ left: g.x, top: g.y, width: CARD_W, height: CARD_H }}>
            {g.label}
          </div>
        ))}
        {[...l.placed.values()].map((p) => (
          <NodeCard key={p.node.id} t={t} p={p} doc={doc} selected={selected === p.node.id} onSelect={onSelect} />
        ))}
      </div>
    </div>
  )
}

// TraceInvalid shows a block that failed validation as code with the
// errors beside it (spec 6.10).
export function TraceInvalid({ t }: { t: Trace }) {
  return (
    <div className="tinvalid">
      <pre>
        <code>{t.source}</code>
      </pre>
      <div className="tinvalid-errors">
        <b>This trace block is not valid</b>
        <ul>
          {(t.errors ?? []).map((e, i) => (
            <li key={i}>
              line {e.line}
              {e.node ? `, node ${e.node}` : ''}: {e.message}
            </li>
          ))}
        </ul>
        <span className="muted small">It is shown as code. It does not block syncing.</span>
      </div>
    </div>
  )
}

function counts(t: Trace, doc: Doc): string {
  const nodes = t.nodes ?? []
  const links = nodes.reduce((a, n) => a + (n.realizes?.length ?? 0), 0)
  const stale = t.divergences.filter((d) => d.class === 'stale').length
  const parts = [plural(nodes.length, 'step'), plural(links, 'realization link'), plural(t.graph, 'divergence')]
  if (stale) parts.push(plural(stale, 'stale reference'))
  const c = nodeEntries(doc, t).length
  if (c) parts.push(plural(c, 'comment'))
  return parts.join(' · ')
}

// TraceInline is the reduced block in the linear review.
export function TraceInline({ t, doc }: { t: Trace; doc: Doc }) {
  const box = useRef<HTMLDivElement>(null)
  const [scale, setScale] = useState(0.55)
  useEffect(() => {
    const el = box.current
    if (!el || !t.nodes) return
    const w = layout(t).width
    const fit = () => setScale(Math.max(0.35, Math.min(0.6, el.clientWidth / w)))
    fit()
    const ro = new ResizeObserver(fit)
    ro.observe(el)
    return () => ro.disconnect()
  }, [t])
  if (!t.nodes) return <TraceInvalid t={t} />
  const href = traceHref(doc.rel, t.index)
  return (
    <figure className="tinline">
      <figcaption>
        <span className="muted small">
          Trace · {counts(t, doc)}
        </span>
        <a href={href}>Open trace view →</a>
      </figcaption>
      <div className="tinline-scroll" ref={box}>
        <TraceDiagram t={t} doc={doc} scale={scale} onSelect={(id) => (location.hash = href + '?node=' + encodeURIComponent(id))} />
      </div>
    </figure>
  )
}

function node(t: Trace, id: string): TraceNode | undefined {
  return t.nodes?.find((n) => n.id === id)
}

function nodeName(t: Trace, id: string): string {
  const n = node(t, id)
  return n ? `${id} ${n.text}` : id
}

// DivCard explains one divergence in words, using the block's own lane
// names (implementation plan M7: nothing hard-coded about "business").
function divCard(t: Trace, d: Divergence): { head: string; title: string; text: string } {
  const n = node(t, d.node)
  const lanes = t.lanes ?? []
  const li = lanes.indexOf(n?.lane ?? '')
  const why = n?.why ? ` Why: ${n.why}.` : ''
  const id = d.node
  switch (d.class) {
    case 'implementation-only':
      return { head: `IMPLEMENTATION-ONLY · ${id}`, title: n?.text ?? id, text: `No ${lanes.slice(0, li).join(' or ')} step describes this.${why}` }
    case 'unrealized':
      return { head: `UNREALIZED · ${id}`, title: n?.text ?? id, text: `No ${lanes.slice(li + 1).join(' or ')} step realizes this.${why}` }
    case 'merged':
      return {
        head: `MERGED · ${id} REALIZES ${(d.nodes ?? []).join(' + ')}`,
        title: n?.text ?? id,
        text: `${(d.nodes ?? []).map((x) => nodeName(t, x)).join(' and ')} are one ${n?.lane} step: they cannot change independently.${why}`,
      }
    case 'expanded':
      return {
        head: `EXPANDED · ${id} REALIZED BY ${(d.nodes ?? []).join(' + ')}`,
        title: n?.text ?? id,
        text: `${plural((d.nodes ?? []).length, 'step')} carry out this one: ${(d.nodes ?? []).map((x) => nodeName(t, x)).join(', ')}.`,
      }
    case 'scattered':
      return {
        head: `SCATTERED · ${id}`,
        title: n?.text ?? id,
        text: `The ${d.lane} steps for this are not next to each other in the flow: ${(d.nodes ?? []).join(', ')}.`,
      }
    case 'reordered': {
      const [b, early, late] = d.nodes ?? []
      return {
        head: `REORDERED · ${id} AND ${b}`,
        title: `${n?.text ?? id} / ${node(t, b)?.text ?? b}`,
        text: `${id} comes before ${b} in ${n?.lane}, but in ${d.lane} ${early} (for ${b}) runs before ${late} (for ${id}).`,
      }
    }
    case 'stale':
      return { head: `STALE REFERENCE · ${id}`, title: n?.text ?? id, text: d.detail ?? '' }
    default:
      return { head: `${d.class.toUpperCase()} · ${id}`, title: n?.text ?? id, text: d.detail ?? '' }
  }
}

function Summary({ t, doc, onSelect }: { t: Trace; doc: Doc; onSelect: (id: string) => void }) {
  const lanes = t.lanes ?? []
  const first = lanes[0]
  const last = lanes[lanes.length - 1]
  const shown = t.divergences.filter((d) => d.class !== 'unverified')
  const unverified = t.divergences.filter((d) => d.class === 'unverified')
  const lastNodes = (t.nodes ?? []).filter((n) => n.lane === last)
  const reasons = new Map<string, string[]>()
  for (const d of unverified) reasons.set(d.detail ?? '', [...(reasons.get(d.detail ?? '') ?? []), d.node])
  const by = [...new Set(Object.values(doc.refs).filter((r) => r.ok && r.plugin).map((r) => r.plugin))]
  return (
    <>
      <div>
        <div className="panel-title">Where the {['two', 'three', 'four', 'five'][lanes.length - 2] ?? lanes.length} views differ</div>
        <div className="muted small">Computed from the trace, not written by the author</div>
      </div>
      {shown.length === 0 && <div className="muted">The lanes line up: no divergences.</div>}
      {shown.map((d, i) => {
        const c = divCard(t, d)
        return (
          <button key={i} type="button" className={'divcard' + (NEUTRAL.has(d.class) ? ' neutral' : '')} onClick={() => onSelect(d.node)}>
            <span className="divcard-head">{c.head}</span>
            <span className="divcard-title">{c.title}</span>
            <span className="divcard-text">{c.text}</span>
          </button>
        )
      })}
      {unverified.length > 0 && (
        <div className="divcard neutral static">
          <span className="divcard-head">UNVERIFIED · {unverified.map((d) => d.node).join(', ')}</span>
          {[...reasons].map(([why, ids]) => (
            <span key={why} className="divcard-text">
              {ids.join(', ')}: {why}
            </span>
          ))}
        </div>
      )}
      <div className="hr" />
      <dl className="summary">
        <div>
          <dt>{cap(last)} references resolved</dt>
          <dd>
            {lastNodes.length - unverified.length} of {lastNodes.length}
          </dd>
        </div>
        <div>
          <dt>
            {cap(first)} steps with no {last}
          </dt>
          <dd>{(t.nodes ?? []).filter((n) => n.lane === first && t.divergences.some((d) => d.class === 'unrealized' && d.node === n.id)).length}</dd>
        </div>
        <div>
          <dt>Steps out of order</dt>
          <dd>{t.divergences.filter((d) => d.class === 'reordered').length}</dd>
        </div>
        <div>
          <dt>Comments on this trace</dt>
          <dd>{nodeEntries(doc, t).length}</dd>
        </div>
      </dl>
      <div className="grow" />
      <div className="muted small">
        {by.length > 0 ? (
          <>
            References resolved on your machine by <span className="mono">{by.join(', ')}</span> with your own credentials.
          </>
        ) : (
          'No plugin on this machine resolved the references. They are shown as written.'
        )}
      </div>
    </>
  )
}

function cap(s: string): string {
  return s.charAt(0).toUpperCase() + s.slice(1)
}

function NodePanel({ t, doc, id, onClose, reload }: { t: Trace; doc: Doc; id: string; onClose: () => void; reload: () => void }) {
  const n = node(t, id)
  const [replyTo, setReplyTo] = useState<Entry | null>(null)
  const [saves, setSaves] = useState(0)
  const byId = useMemo(() => new Map(doc.entries.map((e) => [e.id, e])), [doc])
  const replies = useMemo(() => {
    const m = new Map<string, Entry[]>()
    for (const e of doc.entries) if (e.reply_to) m.set(e.reply_to, [...(m.get(e.reply_to) ?? []), e])
    return m
  }, [doc])
  if (!n) return null
  const lanes = t.lanes ?? []
  const divs = t.divergences.filter((d) => d.node === id)
  const realizedBy = (t.nodes ?? []).filter((m) => m.realizes?.includes(id))
  const notes = nodeEntries(doc, t, id)
  const anchor = t.section ? `${t.section}#${id}` : ''
  const li = lanes.indexOf(n.lane)
  return (
    <>
      <div className="row center">
        <span className="mono muted">
          {id} · {n.lane}
        </span>
        {divs.map((d, i) => (
          <span key={i} className={'tbadge ' + (NEUTRAL.has(d.class) ? 'outline' : 'div')} title={d.detail}>
            {badgeText(d)}
          </span>
        ))}
        <span className="grow" />
        <button type="button" className="icon" aria-label="Close panel" onClick={onClose}>
          ×
        </button>
      </div>
      <div>
        <div className="panel-title big">{n.text}</div>
        {n.why && (
          <div className="mt6">
            <b>Why:</b> {n.why}
          </div>
        )}
        {li > 0 && (
          <div className="mt2">
            <b>Realizes:</b>{' '}
            {n.realizes?.length ? n.realizes.map((r) => nodeName(t, r)).join(', ') : `nothing in the ${lanes.slice(0, li).join(' or ')} lane${li > 1 ? 's' : ''}.`}
          </div>
        )}
        {li < lanes.length - 1 && (
          <div className="mt2">
            <b>Realized by:</b> {realizedBy.length ? realizedBy.map((m) => nodeName(t, m.id)).join(', ') : 'nothing yet.'}
          </div>
        )}
      </div>
      {n.ref && <RefBox info={doc.refs[n.ref]} raw={n.ref} />}
      <div className="hr" />
      <div className="small b">{notes.length ? plural(notes.length, 'comment') + ' on this step' : 'No comments on this step yet'}</div>
      {notes.map((e) => (
        <EntryCard key={e.id} e={e} replies={replies} byId={byId} onReply={setReplyTo} />
      ))}
      {anchor ? (
        <Composer
          key={`${anchor}/${replyTo?.id ?? ''}/${saves}`}
          doc={doc}
          draft={{ anchor, re: replyTo?.id }}
          lockAnchor
          onCancel={() => setReplyTo(null)}
          onSaved={() => {
            setReplyTo(null)
            setSaves((n) => n + 1)
            reload()
          }}
        />
      ) : (
        <div className="muted small">This block has no heading above it, so its steps cannot be commented on.</div>
      )}
    </>
  )
}

const ZOOMS = [0.4, 0.5, 0.6, 0.75, 0.9, 1, 1.25, 1.5]

// TracePage is the full trace view of block `index`.
export function TracePage({ doc, index, nodeId, reload }: { doc: Doc; index: number; nodeId?: string; reload: () => void }) {
  const t = doc.traces[index]
  const [mode, setMode] = useState<'diagram' | 'source'>('diagram')
  const [scale, setScale] = useState(1)
  const [selected, setSelected] = useState<string | undefined>(nodeId)
  const area = useRef<HTMLDivElement>(null)
  useEffect(() => setSelected(nodeId), [nodeId, index])

  const fit = () => {
    if (!t?.nodes || !area.current) return
    const l = layout(t)
    const s = Math.min(1, (area.current.clientWidth - 32) / l.width)
    setScale(Math.max(0.3, Math.round(s * 100) / 100))
  }
  useEffect(fit, [index]) // eslint-disable-line react-hooks/exhaustive-deps

  if (!t) {
    return (
      <div className="page">
        <p className="muted">This blueprint has no trace block {index + 1}.</p>
      </div>
    )
  }
  const zoom = (dir: 1 | -1) => {
    const i = ZOOMS.findIndex((z) => z >= scale - 0.001)
    const j = Math.min(ZOOMS.length - 1, Math.max(0, (i < 0 ? ZOOMS.length - 1 : i) + dir))
    setScale(ZOOMS[j])
  }
  const n = doc.traces.length
  return (
    <div className="trace-page">
      <div className="trace-main">
        <div className="trace-head">
          <div>
            <div className="trace-title">
              {t.title ?? 'Trace'} <span className="muted normal">· trace {index + 1} of {n}</span>
              {n > 1 && (
                <span className="trace-nav">
                  <a href={index > 0 ? traceHref(doc.rel, index - 1) : undefined} aria-disabled={index === 0} aria-label="Previous trace">
                    ‹
                  </a>
                  <a href={index < n - 1 ? traceHref(doc.rel, index + 1) : undefined} aria-disabled={index === n - 1} aria-label="Next trace">
                    ›
                  </a>
                </span>
              )}
            </div>
            <div className="muted small">{t.nodes ? counts(t, doc) : 'not a valid trace'}</div>
          </div>
          <div className="seg" role="group" aria-label="Show">
            <button type="button" className={mode === 'diagram' ? 'on' : ''} onClick={() => setMode('diagram')} disabled={!t.nodes}>
              Diagram
            </button>
            <button type="button" className={mode === 'source' ? 'on' : ''} onClick={() => setMode('source')}>
              Source
            </button>
          </div>
        </div>
        <div className="trace-area" ref={area}>
          {!t.nodes ? (
            <TraceInvalid t={t} />
          ) : mode === 'source' ? (
            <pre className="trace-source">
              <code>{t.source}</code>
            </pre>
          ) : (
            <TraceDiagram t={t} doc={doc} scale={scale} selected={selected} onSelect={(id) => setSelected(id === selected ? undefined : id)} />
          )}
        </div>
        {t.nodes && mode === 'diagram' && (
          <div className="trace-foot">
            <div className="seg">
              <button type="button" aria-label="Zoom in" onClick={() => zoom(1)}>
                +
              </button>
              <button type="button" aria-label="Zoom out" onClick={() => zoom(-1)}>
                −
              </button>
              <button type="button" onClick={fit}>
                Fit
              </button>
            </div>
            <div className="legend">
              <span>
                <b>→</b> flow within a lane
              </span>
              <span>
                <span className="legend-dash">- - -</span> realizes
              </span>
              <span>
                <span className="legend-div">orange</span> divergence
              </span>
            </div>
          </div>
        )}
      </div>
      {t.nodes && (
        <RefLayer doc={doc} className="trace-panel">
          {selected && node(t, selected) ? (
            <NodePanel t={t} doc={doc} id={selected} onClose={() => setSelected(undefined)} reload={reload} />
          ) : (
            <Summary t={t} doc={doc} onSelect={setSelected} />
          )}
        </RefLayer>
      )}
    </div>
  )
}
