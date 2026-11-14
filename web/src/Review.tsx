import { useEffect, useMemo, useRef, useState } from 'react'
import { api, type Doc, type Entry, type Section } from './api'
import { render, renderParts } from './markdown'
import { Mermaid } from './Mermaid'
import { RefLayer } from './refs'
import { TraceInline } from './Trace'
import { groupEntries } from './entries'
import { docHref, localTime, plural } from './util'

export function Markdown({ src, className }: { src: string; className?: string }) {
  // render() escapes raw HTML and filters links (markdown.ts), so its output
  // is safe to insert.
  const html = useMemo(() => render(src), [src])
  return <div className={'md ' + (className ?? '')} dangerouslySetInnerHTML={{ __html: html }} />
}

// Hint dismissals are per viewer and per document, kept in localStorage
// (which may be missing or throw: then hints simply come back).
const hintKey = (id: string, code: string) => `kalka.hint-dismissed.${id}.${code}`

function hintDismissed(id: string, code: string): boolean {
  try {
    return localStorage.getItem(hintKey(id, code)) === '1'
  } catch {
    return false
  }
}

function setHintDismissed(id: string, code: string, on: boolean) {
  try {
    if (on) localStorage.setItem(hintKey(id, code), '1')
    else localStorage.removeItem(hintKey(id, code))
  } catch {
    /* not persisted */
  }
}

export function HintStrip({ doc }: { doc: Doc }) {
  const id = doc.kalka_id || doc.rel
  const [, bump] = useState(0)
  const [showAll, setShowAll] = useState(false)
  const hints = doc.hints ?? []
  if (hints.length === 0) return null
  const dismissed = hints.filter((h) => hintDismissed(id, h.code))
  const shown = showAll ? hints : hints.filter((h) => !hintDismissed(id, h.code))
  const toggle = (code: string, on: boolean) => {
    setHintDismissed(id, code, on)
    bump((n) => n + 1)
  }
  return (
    <div className="hints" aria-label="Blueprint reference hints">
      {shown.map((h) => {
        const off = hintDismissed(id, h.code)
        return (
          <div key={h.code} className={'hint' + (off ? ' off' : '')}>
            <span className="hint-label">hint</span>
            <span className="hint-text">{h.message}</span>
            <button type="button" className="link" onClick={() => toggle(h.code, !off)}>
              {off ? 'Restore' : 'Dismiss'}
            </button>
          </div>
        )
      })}
      {dismissed.length > 0 && (
        <button type="button" className="link hint-more" onClick={() => setShowAll(!showAll)}>
          {showAll ? 'Hide dismissed hints' : `Show dismissed hints (${dismissed.length})`}
        </button>
      )}
    </div>
  )
}

// SeedStrip heads a seed (spec 6.11): where it came from, Promote for
// its owner, and the backlink offer when the parent missed it.
function SeedStrip({ doc, reload }: { doc: Doc; reload: () => void }) {
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  if (!doc.seed) return null
  const s = doc.seed
  const act = async (f: () => Promise<unknown>) => {
    setBusy(true)
    setErr('')
    try {
      await f()
      reload()
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy(false)
  }
  return (
    <div className="hints seed-strip">
      <div className="hint">
        <span className="hint-label">seed</span>
        <span className="hint-text">
          {s.parent_rel ? (
            <>
              Split off from <a href={docHref(s.parent_rel)}>{s.parent_title || s.parent_rel}</a>.
            </>
          ) : s.parent ? (
            <>Split off from a blueprint not in your folder.</>
          ) : (
            <>A seed: an idea kept small until someone takes it up.</>
          )}
        </span>
        {doc.role === 'owner' && (
          <button type="button" className="link" disabled={busy} onClick={() => act(() => api.promote(doc.rel))}>
            Promote to draft
          </button>
        )}
      </div>
      {s.backlink_missing && (
        <div className="hint">
          <span className="hint-text">The parent has no link to this seed yet (its file could not be written).</span>
          <button type="button" className="link" disabled={busy} onClick={() => act(() => api.backlink(doc.rel))}>
            Add the backlink
          </button>
        </div>
      )}
      {err && <div className="error-text">{err}</div>}
    </div>
  )
}

export interface Draft {
  anchor: string
  re?: string
  quote?: string
}

const releaseRe = /^v(\d+) · plan:([0-9a-f]{64}) · owner:(\S+)( · deleted)?$/

// ReleaseBody renders a release entry (spec 20.4) in words; a body that is
// not a release is shown as written.
function ReleaseBody({ body }: { body: string }) {
  const [first, ...rest] = body.split('\n')
  const m = releaseRe.exec(first)
  if (!m) return <Markdown src={body} className="entry-body" />
  const note = rest.filter((l) => !l.startsWith('incorporates: ')).join('\n').trim()
  const inc = rest.find((l) => l.startsWith('incorporates: '))
  return (
    <div className="entry-body">
      {m[4] ? (
        <b>Deleted for the team at version {m[1]}</b>
      ) : (
        <>
          Released <b>version {m[1]}</b>, owner {m[3]}
        </>
      )}
      <span className="mono small muted" title={`plan hash ${m[2]}`}>
        {' '}
        · plan {m[2].slice(0, 8)}
      </span>
      {inc && <div className="muted small">{inc}</div>}
      {note && <Markdown src={note} />}
    </div>
  )
}

function Badges({ e }: { e: Entry }) {
  return (
    <>
      {e.orphaned && (
        <span className="badge warn" title={e.node ? `§${e.anchor}: the plan has no trace step ${e.node} under that heading any more` : `§${e.anchor} matches no heading in the current plan`}>
          orphaned
        </span>
      )}
      {e.outdated && !e.orphaned && e.kind !== 'handoff' && (
        <span className="badge" title={`written against rev ${e.rev}; the plan has changed since`}>
          outdated
        </span>
      )}
      {!e.valid && (
        <span className="badge warn" title="the entry id does not match its content">
          damaged
        </span>
      )}
      {e.valid && e.sig === 'unsigned' && (
        <span className="badge" title="no signature: written by hand or by an older kalka. It stays in local files and is not shared with a team.">
          unsigned
        </span>
      )}
      {e.valid && e.sig === 'invalid' && (
        <span className="badge warn" title={`the signature does not match the key held for ${e.author}; the entry has no effect`}>
          signature invalid
        </span>
      )}
      {e.handoff === 'unverified' && (
        <span className="badge warn" title={e.handoff_note}>
          unverified handoff
        </span>
      )}
    </>
  )
}

export function EntryCard({
  e,
  replies,
  byId,
  onReply,
  onSeed,
}: {
  e: Entry
  replies: Map<string, Entry[]>
  byId: Map<string, Entry>
  onReply: (e: Entry) => void
  onSeed?: (e: Entry) => void
}) {
  const kids = replies.get(e.id) ?? []
  return (
    <div className={'entry kind-' + e.kind} id={'e-' + e.id}>
      <div className="entry-head">
        <span className={'kind kind-' + e.kind}>{e.kind}</span>
        <span className="author">{e.author}</span>
        {e.node && (
          <span className="mono small muted" title={`on trace step ${e.node}`}>
            #{e.node}
          </span>
        )}
        <time dateTime={e.timestamp} title={e.timestamp}>
          {localTime(e.timestamp)}
        </time>
        <Badges e={e} />
      </div>
      {e.kind === 'budded' && e.budded_seed ? (
        <div className="entry-body">
          Split off a seed:{' '}
          {e.budded_rel ? (
            <a className="seed-link" href={docHref(e.budded_rel)}>
              {e.budded_title || e.budded_seed}
            </a>
          ) : (
            <span className="seed-link" title={`kalka_id ${e.budded_seed}; not in your folder`}>
              {e.budded_title || e.budded_seed}
            </span>
          )}
        </div>
      ) : e.kind === 'release' ? (
        <ReleaseBody body={e.body} />
      ) : e.kind === 'handoff' ? (
        <div className="entry-body">
          Ownership passed to <b>{e.handoff_target ?? '?'}</b>
          {e.handoff === 'verified' ? <span className="muted"> · signature verified</span> : null}
          {e.handoff_note && <div className="muted small">{e.handoff_note}</div>}
        </div>
      ) : (
        <Markdown src={e.body} className="entry-body" />
      )}
      {e.reply_to && !byId.has(e.reply_to) && <div className="muted small">reply to an entry that is not in this file ({e.reply_to})</div>}
      {e.unparsed && (
        <details className="unparsed">
          <summary>text in the log that is not an entry</summary>
          <pre>{e.unparsed}</pre>
        </details>
      )}
      <div className="entry-actions">
        <button type="button" className="link" onClick={() => onReply(e)}>
          Reply
        </button>
        {onSeed && e.kind === 'deviation' && e.valid && (
          <button type="button" className="link" onClick={() => onSeed(e)}>
            Create seed
          </button>
        )}
      </div>
      {kids.length > 0 && (
        <div className="replies">
          {kids.map((k) => (
            <EntryCard key={k.id} e={k} replies={replies} byId={byId} onReply={onReply} onSeed={onSeed} />
          ))}
        </div>
      )}
    </div>
  )
}

export function Composer({
  doc,
  draft,
  onCancel,
  onSaved,
  lockAnchor,
}: {
  doc: Doc
  draft: Draft
  onCancel: () => void
  onSaved: () => void
  // lockAnchor shows the anchor instead of offering a choice (trace steps).
  lockAnchor?: boolean
}) {
  const [kind, setKind] = useState('comment')
  const [anchor, setAnchor] = useState(draft.anchor)
  const [quote, setQuote] = useState(draft.quote ?? '')
  const [body, setBody] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const text = useRef<HTMLTextAreaElement>(null)
  useEffect(() => text.current?.focus(), [])
  const target = draft.re ? doc.entries.find((e) => e.id === draft.re) : undefined

  const save = async () => {
    if (!body.trim()) return
    setBusy(true)
    setErr('')
    try {
      await api.addEntry({ path: doc.rel, kind, anchor, re: draft.re, quote, body })
      onSaved()
    } catch (e) {
      setErr((e as Error).message)
      setBusy(false)
    }
  }
  return (
    <form
      className="composer"
      onSubmit={(e) => {
        e.preventDefault()
        save()
      }}
    >
      {target && (
        <div className="muted small">
          Replying to {target.author}, {localTime(target.timestamp)}
        </div>
      )}
      <div className="row">
        <select value={kind} onChange={(e) => setKind(e.target.value)} aria-label="Kind">
          <option value="comment">Comment</option>
          <option value="deviation">Deviation</option>
        </select>
        {lockAnchor ? (
          <span className="mono small muted anchor-fixed">§{anchor}</span>
        ) : (
          <select value={anchor} onChange={(e) => setAnchor(e.target.value)} aria-label="Section">
            <option value="">General (whole plan)</option>
            {anchor.includes('#') && <option value={anchor}>§{anchor}</option>}
            {doc.headings.map((h) => (
              <option key={h.slug} value={h.slug}>
                {'  '.repeat(Math.max(0, h.level - 1))}§{h.slug}
              </option>
            ))}
          </select>
        )}
      </div>
      {quote && (
        <div className="quote-edit">
          <blockquote>{quote}</blockquote>
          <button type="button" className="link" onClick={() => setQuote('')}>
            Remove quote
          </button>
        </div>
      )}
      <textarea
        ref={text}
        rows={4}
        value={body}
        onChange={(e) => setBody(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) save()
          if (e.key === 'Escape') onCancel()
        }}
        placeholder={kind === 'deviation' ? 'What was done differently from the plan, and why' : 'Your comment (Markdown)'}
        aria-label="Entry text"
      />
      <div className="muted small">Headings in your text become level-4 headings. Ctrl/⌘+Enter to add.</div>
      {err && <div className="error-text">{err}</div>}
      <div className="row end">
        <button type="button" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
        <button type="submit" className="primary" disabled={busy || !body.trim()}>
          Add {kind}
        </button>
      </div>
    </form>
  )
}

// SectionBody renders a section with its trace and mermaid blocks drawn,
// not shown as code.
function SectionBody({ s, doc }: { s: Section; doc: Doc }) {
  const parts = useMemo(() => renderParts(s.markdown, s.line), [s.markdown, s.line])
  return (
    <>
      {parts.map((p, i) => {
        if (p.kind === 'html') return <div key={i} className="md" dangerouslySetInnerHTML={{ __html: p.html }} />
        if (p.kind === 'mermaid') return <Mermaid key={i} source={p.source} />
        const t = doc.traces.find((x) => x.line === p.line)
        if (!t)
          return (
            <pre key={i} className="md-pre">
              <code>{p.source}</code>
            </pre>
          )
        return <TraceInline key={i} t={t} doc={doc} />
      })}
    </>
  )
}

// Review is the linear review view (spec 11.1).
export function Review({ doc, reload, onSeed }: { doc: Doc; reload: () => void; onSeed?: (e: Entry) => void }) {
  const [draft, setDraft] = useState<Draft | null>(null)
  const [selection, setSelection] = useState<{ slug: string; text: string } | null>(null)

  const { bySlug, general, orphaned, replies, byId } = useMemo(() => groupEntries(doc), [doc])

  // Scroll to a section or entry named in the location, once.
  useEffect(() => {
    const id = new URLSearchParams(location.hash.split('?')[1] ?? '').get('at')
    const el = id ? document.getElementById(id) : null
    if (!el) return
    el.scrollIntoView({ block: 'center' })
    // Show which one was meant (from the canvas, say).
    el.classList.add('flash')
    const t = setTimeout(() => el.classList.remove('flash'), 2000)
    return () => clearTimeout(t)
  }, [])

  const captureSelection = (slug: string) => {
    const s = window.getSelection()
    const t = s?.toString().trim() ?? ''
    setSelection(t ? { slug, text: t.slice(0, 2000) } : null)
  }

  const open = (anchor: string, quote?: string) => setDraft({ anchor, quote })
  const onReply = (e: Entry) => setDraft({ anchor: e.anchor, re: e.id })
  const saved = () => {
    setDraft(null)
    setSelection(null)
    reload()
  }

  const composerFor = (anchor: string) =>
    draft && draft.anchor === anchor ? <Composer doc={doc} draft={draft} onCancel={() => setDraft(null)} onSaved={saved} /> : null

  const sectionView = (s: Section, i: number) => {
    const slug = s.heading?.slug ?? ''
    const notes = s.heading ? bySlug.get(slug) ?? [] : []
    const sel = selection && selection.slug === slug ? selection : null
    return (
      <section className="sec" key={i} id={s.heading ? 's-' + slug : undefined}>
        <div className="sec-body" onMouseUp={() => s.heading && captureSelection(slug)}>
          <SectionBody s={s} doc={doc} />
        </div>
        <aside className="sec-notes">
          {s.heading && (
            <>
              {notes.map((e) => (
                <EntryCard key={e.id} e={e} replies={replies} byId={byId} onReply={onReply} onSeed={onSeed} />
              ))}
              {composerFor(slug) ??
                (sel ? (
                  <button type="button" className="add sel" onClick={() => open(slug, sel.text)}>
                    Comment on the selected text
                  </button>
                ) : (
                  <button type="button" className="add" onClick={() => open(slug)}>
                    + Comment on §{slug}
                  </button>
                ))}
            </>
          )}
        </aside>
      </section>
    )
  }

  return (
    <RefLayer doc={doc} className="review">
      <SeedStrip doc={doc} reload={reload} />
      <HintStrip doc={doc} />
      {doc.sections.length === 0 && <p className="muted">This file has no body text.</p>}
      {doc.sections.map(sectionView)}
      <section className="sec general" id="general">
        <div className="sec-body">
          <h2 className="log-title">General</h2>
          <p className="muted">Entries about the whole plan. {plural(doc.entries.length, 'entry', 'entries')} in the review log.</p>
          {doc.log_lead && (
            <details className="unparsed">
              <summary>text at the start of the review log that is not an entry</summary>
              <pre>{doc.log_lead}</pre>
            </details>
          )}
        </div>
        <aside className="sec-notes">
          {general.map((e) => (
            <EntryCard key={e.id} e={e} replies={replies} byId={byId} onReply={onReply} onSeed={onSeed} />
          ))}
          {composerFor('') ?? (
            <button type="button" className="add" onClick={() => open('')}>
              + General comment
            </button>
          )}
        </aside>
      </section>
      {orphaned.length > 0 && (
        <section className="sec" id="orphaned">
          <div className="sec-body">
            <h2 className="log-title">Orphaned</h2>
            <p className="muted">These entries point at headings that are no longer in the plan.</p>
          </div>
          <aside className="sec-notes">
            {orphaned.map((e) => (
              <EntryCard key={e.id} e={e} replies={replies} byId={byId} onReply={onReply} onSeed={onSeed} />
            ))}
          </aside>
        </section>
      )}
    </RefLayer>
  )
}
