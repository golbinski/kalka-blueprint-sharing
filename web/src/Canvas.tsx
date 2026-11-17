// Canvas review (spec 11.1): the linear view's sections and entries as a
// read-only graph. Selecting a card opens the linear view scrolled to it.
// The layout is computed (canvasLayout.ts) and nothing is saved.
import { useEffect, useMemo, useRef, useState } from 'react'
import type { Doc } from './api'
import { layout, type NoteCard, type SecCard } from './canvasLayout'
import { RefLayer } from './refs'
import { docHref, localTime, plural, traceHref } from './util'

const ZOOMS = [0.3, 0.4, 0.5, 0.6, 0.75, 0.9, 1, 1.25]

// atHref names a place in the linear view: an entry, a section or a group.
export function atHref(rel: string, id: string): string {
  return docHref(rel) + '?at=' + encodeURIComponent(id)
}

function secTarget(c: SecCard): string | null {
  if (c.kind === 'general') return 'general'
  if (c.kind === 'orphaned') return 'orphaned'
  return c.slug ? 's-' + c.slug : null
}

function Section({ doc, c }: { doc: Doc; c: SecCard }) {
  const target = secTarget(c)
  const style = { left: c.x, top: c.y, width: c.w, height: c.h }
  const body = (
    <>
      <div className="cv-sec-title">
        {c.level && c.level > 1 && <span className="mono muted small">§{c.slug} </span>}
        {c.title}
      </div>
      {c.lines > 0 && (
        <div className="cv-sec-text" style={{ WebkitLineClamp: c.lines }}>
          {c.excerpt}
        </div>
      )}
      <div className="cv-foot muted small">
        {[c.entries > 0 ? plural(c.entries, 'comment') : 'no comments', c.traces > 0 && plural(c.traces, 'trace block')].filter(Boolean).join(' · ')}
      </div>
    </>
  )
  const cls = 'cv-card cv-sec cv-' + c.kind
  if (!target) return <div className={cls} style={style}>{body}</div>
  return (
    <a className={cls} style={style} href={atHref(doc.rel, target)} title="Open in the document">
      {body}
    </a>
  )
}

function Note({ doc, n }: { doc: Doc; n: NoteCard }) {
  const e = n.entry
  const flags = [e.orphaned && 'section removed', e.outdated && !e.orphaned && 'on an earlier version', !e.valid && 'damaged', e.valid && e.sig === 'invalid' && 'signature invalid']
    .filter(Boolean)
    .join(' · ')
  return (
    <a
      className={'cv-card cv-note kind-' + e.kind}
      style={{ left: n.x, top: n.y, width: n.w, height: n.h }}
      href={atHref(doc.rel, 'e-' + e.id)}
      title="Show this comment in the document"
    >
      <div className="cv-note-head">
        <span className={'kind kind-' + e.kind}>{e.kind}</span>
        <span className="author">{e.author}</span>
        <time dateTime={e.timestamp}>{localTime(e.timestamp)}</time>
        {flags && <span className="cv-flag">{flags}</span>}
      </div>
      <div className="cv-note-text" style={{ WebkitLineClamp: n.lines }}>
        {n.text}
      </div>
    </a>
  )
}

export function CanvasPage({ doc }: { doc: Doc }) {
  const l = useMemo(() => layout(doc), [doc])
  const [scale, setScale] = useState(1)
  const area = useRef<HTMLDivElement>(null)
  const fit = () => {
    if (!area.current) return
    const s = Math.min(1, (area.current.clientWidth - 16) / l.width)
    setScale(Math.max(0.3, Math.round(s * 100) / 100))
  }
  useEffect(fit, [doc.rel]) // eslint-disable-line react-hooks/exhaustive-deps
  const zoom = (dir: 1 | -1) => {
    const i = ZOOMS.findIndex((z) => z >= scale - 0.001)
    const j = Math.min(ZOOMS.length - 1, Math.max(0, (i < 0 ? ZOOMS.length - 1 : i) + dir))
    setScale(ZOOMS[j])
  }
  const bodySections = l.sections.filter((s) => s.kind === 'section').length
  return (
    <div className="canvas-page">
      <h1 className="sr-only">Map of “{doc.title}”</h1>
      <div className="trace-head">
        <div>
          <div className="trace-title">Canvas</div>
          <div className="muted small">
            {plural(bodySections, 'section')} · {plural(doc.entries.length, 'comment')} · {plural(new Set(l.refs.map((r) => r.ref)).size, 'reference')}
            {' · '}select a card to open it in the document
          </div>
        </div>
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
              <span className="legend-dash">- - -</span> reference
            </span>
            <span>
              <b>—</b> comment on a section
            </span>
            <span>
              <span className="legend-div">orange</span> deviation
            </span>
          </div>
        </div>
      </div>
      <RefLayer doc={doc} className="canvas-area">
        <div ref={area}>
          <div className="tdiagram-outer" style={{ width: l.width * scale, height: l.height * scale }}>
            <div className="tdiagram" style={{ width: l.width, height: l.height, transform: `scale(${scale})` }} aria-label="Canvas of the blueprint">
              <svg className="tedges" width={l.width} height={l.height} aria-hidden="true">
                {l.edges.map((e, i) => (
                  <path key={i} d={e.d} className={'cv-edge cv-edge-' + e.kind} />
                ))}
              </svg>
              {l.refs.map((r) => {
                const v = doc.refs[r.ref]
                return (
                  <div key={r.key} className="cv-card cv-ref" style={{ left: r.x, top: r.y, width: r.w, height: r.h }}>
                    <span className="ref" data-ref={r.ref}>
                      {v?.ok && v.title ? v.title : r.ref}
                    </span>
                    <div className="muted small cv-ref-state">
                      {v?.ok ? v.status || 'resolved' : v?.pending ? 'resolving…' : v?.plugin ? 'not resolved' : 'no plugin'}
                    </div>
                  </div>
                )
              })}
              {l.sections.map((c) => (
                <Section key={c.key} doc={doc} c={c} />
              ))}
              {l.notes.map((n) => (
                <Note key={n.entry.id} doc={doc} n={n} />
              ))}
            </div>
          </div>
        </div>
      </RefLayer>
      {doc.traces.length > 0 && (
        <p className="muted small">
          Trace blocks are drawn in the <a href={traceHref(doc.rel, 0)}>Trace</a> view.
        </p>
      )}
    </div>
  )
}
