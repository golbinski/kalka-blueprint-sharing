// References resolved by the reader's plugins (spec 10): shown generically
// (title, url, status, snippet as text, warnings), never branching on the
// plugin or scheme.
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { Doc, RefView } from './api'
import { highlightLines, languageOf } from './highlight'
import { allowedLink } from './markdown'
import { Modal } from './Modal'

export function RefBox({ info: r, raw }: { info?: RefView; raw: string }) {
  const [big, setBig] = useState(false)
  if (!r?.ok) {
    return (
      <div className="refbox">
        <div className="refbox-head">
          <span className="mono">{raw}</span>
        </div>
        <div className="refbox-foot">
          <span className="muted">{r?.pending ? `Resolving with ${r.plugin}…` : `Not resolved: ${r?.error ?? 'no result'}`}</span>
        </div>
      </div>
    )
  }
  const url = r.url && allowedLink(r.url) && !r.url.startsWith('#') ? r.url : ''
  const snip = r.snippet?.text ? <Snippet text={r.snippet.text} start={r.snippet.start_line || 1} lang={languageOf(r.snippet.lang, r.title, r.url)} /> : null
  return (
    <div className="refbox">
      <div className="refbox-head">
        <span className="mono">{r.title || raw}</span>
        {snip && (
          <button type="button" className="small" onClick={() => setBig(true)}>
            Expand
          </button>
        )}
      </div>
      {snip}
      {big && snip && (
        <Modal title={r.title || raw} wide onClose={() => setBig(false)}>
          {snip}
          <div className="row">
            <span className="grow" />
            <button type="button" onClick={() => setBig(false)}>
              Close
            </button>
          </div>
        </Modal>
      )}
      {(r.warnings ?? []).map((w, i) => (
        <div key={i} className="refbox-warn">
          {w.text}
        </div>
      ))}
      <div className="refbox-foot">
        <span className="muted">{r.status}</span>
        {url && (
          <a href={url} target="_blank" rel="noopener noreferrer">
            Open source
          </a>
        )}
      </div>
      {r.plugin && <div className="refbox-by muted small">Resolved on your machine by {r.plugin}</div>}
    </div>
  )
}

// Snippet is the code of a reference with its line numbers, highlighted when
// its language is known.
function Snippet({ text, start, lang }: { text: string; start: number; lang: string | null }) {
  const lines = useMemo(() => highlightLines(text, lang), [text, lang])
  return (
    <div className="snippet" role="figure" aria-label="Source snippet">
      {lines.map((html, i) => (
        <div key={i} className="snippet-line">
          <span className="ln">{start + i}</span>
          <span className="snippet-text" dangerouslySetInnerHTML={{ __html: html }} />
        </div>
      ))}
    </div>
  )
}

type State = 'ok' | 'stale' | 'pending' | 'error' | 'none'

function stateOf(r?: RefView): State {
  if (!r) return 'none'
  if (r.ok) return r.warnings?.length ? 'stale' : 'ok'
  if (r.pending) return 'pending'
  return r.plugin ? 'error' : 'none'
}

// RefLayer decorates the plugin references rendered inside it (spans from
// markdown.ts) and opens a card with the resolved reference on click. A
// reference no plugin handles stays plain text (spec 10.1).
export function RefLayer({ doc, children, className }: { doc: Doc; children: ReactNode; className?: string }) {
  const box = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState<{ ref: string; x: number; y: number } | null>(null)

  useEffect(() => {
    box.current?.querySelectorAll<HTMLElement>('.ref[data-ref]').forEach((el) => {
      const r = doc.refs[el.dataset.ref ?? '']
      const st = stateOf(r)
      el.dataset.state = st
      if (st === 'none') {
        el.removeAttribute('tabindex')
        el.removeAttribute('role')
        el.title = ''
      } else {
        el.tabIndex = 0
        el.setAttribute('role', 'button')
        el.title = r?.ok ? r.title || r.ref : (r?.error ?? '')
      }
    })
  })

  useEffect(() => {
    if (!open) return
    const close = (e: Event) => {
      if (e instanceof KeyboardEvent && e.key !== 'Escape') return
      if (e.type === 'mousedown' && (e.target as HTMLElement).closest('.refpop, .ref[data-ref]')) return
      setOpen(null)
    }
    document.addEventListener('keydown', close)
    document.addEventListener('mousedown', close)
    const scroll = (e: Event) => {
      if (!(e.target instanceof Element && e.target.closest('.refpop'))) setOpen(null)
    }
    window.addEventListener('resize', close)
    window.addEventListener('scroll', scroll, true)
    return () => {
      document.removeEventListener('keydown', close)
      document.removeEventListener('mousedown', close)
      window.removeEventListener('resize', close)
      window.removeEventListener('scroll', scroll, true)
    }
  }, [open])

  const activate = (target: EventTarget | null) => {
    const el = (target as HTMLElement | null)?.closest<HTMLElement>('.ref[data-ref]')
    if (!el || el.dataset.state === 'none') return false
    const ref = el.dataset.ref ?? ''
    const rect = el.getBoundingClientRect()
    setOpen((o) => (o?.ref === ref ? null : { ref, x: rect.left, y: Math.min(rect.bottom + 6, window.innerHeight - 120) }))
    return true
  }

  const width = Math.min(480, window.innerWidth - 32)
  return (
    <div
      ref={box}
      className={className}
      onClick={(e) => activate(e.target)}
      onKeyDown={(e) => {
        if ((e.key === 'Enter' || e.key === ' ') && activate(e.target)) e.preventDefault()
      }}
    >
      {children}
      {open && (
        <div className="refpop" role="dialog" aria-label={`Reference ${open.ref}`} style={{ left: Math.max(16, Math.min(open.x, window.innerWidth - width - 16)), top: open.y, width }}>
          <RefBox info={doc.refs[open.ref]} raw={open.ref} />
        </div>
      )}
    </div>
  )
}
