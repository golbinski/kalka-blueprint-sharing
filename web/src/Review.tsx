import { useEffect, useMemo, useRef, useState } from 'react'
import { act } from './actions'
import { api, type Doc, type Entry, type Section } from './api'
import { render, renderParts } from './markdown'
import { Mermaid } from './Mermaid'
import { RefLayer } from './refs'
import { TraceInline } from './Trace'
import { groupEntries, oldThread, thread, versionAt } from './entries'
import { attestedBadge, shortName } from './scope'
import { DiagnosticsPanel } from './Diagnostics'
import { hintsOf } from './diag'
import { DocHeader } from './DocHeader'
import { GlossaryBlock, InvariantBlock, InvariantsPanel, LinkContext, linkerOf, showInvariant, TermsPanel, useLink } from './Aids'
import { canvasHref, docHref, localTime, plural } from './util'

export function Markdown({ src, className }: { src: string; className?: string }) {
  // render() escapes raw HTML and filters links (markdown.ts), so its output
  // is safe to insert.
  // Comments link invariant ids, not terms (spec 6.16).
  const link = useLink()
  const html = useMemo(() => render(src, link && { invariants: link.invariants }), [src, link])
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
  const hints = hintsOf(doc)
  if (hints.length === 0) return null
  const dismissed = hints.filter((h) => hintDismissed(id, h.rule))
  const shown = showAll ? hints : hints.filter((h) => !hintDismissed(id, h.rule))
  const toggle = (code: string, on: boolean) => {
    setHintDismissed(id, code, on)
    bump((n) => n + 1)
  }
  return (
    <div className="hints" aria-label="Blueprint reference hints">
      {shown.map((h) => {
        const off = hintDismissed(id, h.rule)
        return (
          <div key={h.rule} className={'hint' + (off ? ' off' : '')}>
            <span className="hint-label">hint</span>
            <span className="hint-text">{h.message}</span>
            <button type="button" className="link" onClick={() => toggle(h.rule, !off)}>
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
  const run = async (f: () => Promise<unknown>) => {
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
        {/* In Personal every seed is yours, sent or not (spec 7.1). */}
        {(doc.role === 'owner' || doc.scope === 'personal') && (
          <button type="button" className="link" {...act('seed.promote')} disabled={busy} onClick={() => run(() => api.promote(doc.rel))}>
            Turn into a blueprint
          </button>
        )}
      </div>
      {s.backlink_missing && (
        <div className="hint">
          <span className="hint-text">Its parent does not link to it yet (kalka could not write the parent's file).</span>
          <button type="button" className="link" {...act('seed.backlink')} disabled={busy} onClick={() => run(() => api.backlink(doc.rel))}>
            Link it from its parent
          </button>
        </div>
      )}
      {err && <div className="error-text">{err}</div>}
    </div>
  )
}

// DomainRefStrip shows what a blueprint's `kalka_domain:` reference resolves to
// (spec 6.12): a newer version held here, dismissible; a version not kept
// here; an ambiguous team name. It never blocks anything.
function DomainRefStrip({ doc }: { doc: Doc }) {
  const r = doc.domain_ref
  const [, bump] = useState(0)
  if (!r) return null
  const id = doc.kalka_id || doc.rel
  const newer = (r.latest ?? 0) > r.version
  const code = `domain-newer-v${r.latest}`
  const lines: { key: string; text: React.ReactNode; dismiss?: boolean }[] = []
  if (r.ambiguous) lines.push({ key: 'amb', text: <>Two of your teams are named {r.team}, so kalka cannot tell which {r.ref} is meant.</> })
  if (!r.held) lines.push({ key: 'none', text: <>Written against {r.ref}; that domain is not on this computer.</> })
  if (r.held && r.missing)
    lines.push({ key: 'miss', text: <>Written against version {r.version} of {r.team}/{r.domain}, which is not on this computer.</> })
  if (r.held && newer && !hintDismissed(id, code))
    lines.push({
      key: 'newer',
      dismiss: true,
      text: (
        <>
          The {r.domain} domain has a newer version: <a href={docHref(r.rel!)}>v{r.latest}</a>
          {!r.missing && (
            <>
              {' '}
              (this blueprint was written against <a href={docHref(r.rel!, r.version)}>v{r.version}</a>)
            </>
          )}
          .
        </>
      ),
    })
  if (lines.length === 0) return null
  return (
    <div className="hints" aria-label="Domain reference">
      {lines.map((l) => (
        <div key={l.key} className="hint">
          <span className="hint-label">domain</span>
          <span className="hint-text">{l.text}</span>
          {l.dismiss && (
            <button
              type="button"
              className="link"
              onClick={() => {
                setHintDismissed(id, code, true)
                bump((n) => n + 1)
              }}
            >
              Dismiss
            </button>
          )}
        </div>
      ))}
    </div>
  )
}

export interface Draft {
  anchor: string
  re?: string
  quote?: string
}

// ReleaseBody renders a release entry (spec 20.4) in words; a body that is
// not a release is shown as written.
interface Badge {
  label: string
  why: string
  warn?: boolean
}

// badges are what is unusual about an entry, each with its explanation,
// shown when the badge is clicked.
function badges(e: Entry, version: number): Badge[] {
  const out: Badge[] = []
  if (e.orphaned)
    out.push({
      label: 'section removed',
      warn: true,
      why: e.node ? `The plan has no trace step ${e.node} under that heading any more.` : 'The section it was written on is no longer in the plan.',
    })
  if (e.outdated && !e.orphaned)
    out.push({
      label: version > 0 ? `on v${version}` : 'on an earlier version',
      why: 'Written on an earlier text of the plan; the plan has changed since.',
    })
  if (!e.valid) out.push({ label: 'damaged', warn: true, why: 'Its text was changed after it was written, so kalka ignores it.' })
  if (e.valid && e.sig === 'unsigned')
    out.push({ label: 'not signed', why: 'Written by hand, not by kalka. It stays in your copy and is not shared with the team.' })
  if (e.valid && e.sig === 'invalid')
    out.push({ label: 'signature invalid: ignored', warn: true, why: `The signature does not match ${e.author}'s key, so it has no effect.` })
  if (e.valid && e.team_state === 'local-only' && e.sig !== 'unsigned' && e.sig !== 'invalid')
    out.push({ label: 'only in your copy', why: 'Signed by someone who is not in the team, so it is not shared with the team.' })
  if (e.valid && e.sig === 'attested') out.push({ label: 'carried over', why: cap(attestedBadge(e.author)) + '.' })
  if (e.incorporated_in) out.push({ label: `taken up in v${e.incorporated_in}`, why: `The release of version ${e.incorporated_in} took this up.` })
  if (e.valid && e.team_state === 'removed-member')
    out.push({ label: 'former member', why: `${e.author} is no longer in the team. They wrote this while they were, so it stays.` })
  return out
}

const cap = (s: string) => (s ? s[0].toUpperCase() + s.slice(1) : s)

// VersionLine is a release in the review log: history, not conversation.
function VersionLine({ e }: { e: Entry }) {
  const r = e.release
  if (!r) return <Markdown src={e.body} className="entry-body" />
  return (
    <div className="version-line" id={'e-' + e.id} title={`plan ${r.plan}`}>
      <b>{r.deleted ? `Deleted for the team at v${r.version}` : `v${r.version}`}</b> · {localTime(e.timestamp)} · {shortName(r.owner)}
      {r.note && <> · {r.note.replace(/\s+/g, ' ')}</>}
      {!!r.incorporates && <span className="muted"> · takes up {plural(r.incorporates, 'comment')}</span>}
    </div>
  )
}

// ExportLine is an export of a released version to its issue (spec 10.5).
function ExportLine({ e }: { e: Entry }) {
  const x = e.exported!
  return (
    <div className="version-line" id={'e-' + e.id}>
      <b>Exported v{x.version}</b> to{' '}
      {x.url ? (
        <a href={x.url} target="_blank" rel="noreferrer">
          {x.issue}
        </a>
      ) : (
        x.issue
      )}{' '}
      · {localTime(e.timestamp)} · {shortName(e.author)}
    </div>
  )
}

// SeedLine is a seed split off from an entry, under the entry.
function SeedLine({ e }: { e: Entry }) {
  return (
    <div className="seed-line" id={'e-' + e.id}>
      ↳ {shortName(e.author)} split off a seed:{' '}
      {e.budded_rel ? (
        <a className="seed-link" href={docHref(e.budded_rel)}>
          {e.budded_title || e.budded_seed}
        </a>
      ) : (
        <span className="seed-link" title="not in your folder">
          {e.budded_title || e.budded_seed}
        </span>
      )}
    </div>
  )
}

export function EntryCard({
  e,
  replies,
  byId,
  onReply,
  onSeed,
  all,
}: {
  e: Entry
  replies: Map<string, Entry[]>
  byId: Map<string, Entry>
  onReply?: (e: Entry) => void
  onSeed?: (e: Entry) => void
  /** Every entry of the document, to tell which version one was written on. */
  all?: Entry[]
}) {
  const kids = replies.get(e.id) ?? []
  const [unfolded, setUnfolded] = useState(false)
  const [why, setWhy] = useState('')
  if (e.kind === 'release') return <VersionLine e={e} />
  if (e.kind === 'exported' && e.exported) return <ExportLine e={e} />
  if (e.kind === 'budded' && e.budded_seed) return <SeedLine e={e} />
  if (e.incorporated_in && !unfolded) {
    return (
      <div className={'entry folded kind-' + e.kind} id={'e-' + e.id}>
        <button type="button" className="link" onClick={() => setUnfolded(true)}>
          {e.kind === 'deviation' ? 'Deviation' : 'Comment'} by {shortName(e.author)} · taken up in v{e.incorporated_in}
          {kids.length > 0 ? ` · ${plural(kids.length, 'reply', 'replies')}` : ''}
        </button>
      </div>
    )
  }
  const bs = badges(e, e.outdated && all ? versionAt(all, e.timestamp) : 0)
  const seeds = kids.filter((k) => k.kind === 'budded' && k.budded_seed)
  const talk = kids.filter((k) => !seeds.includes(k))
  return (
    <div className={'entry kind-' + e.kind} id={'e-' + e.id}>
      <div className="entry-head">
        {e.kind !== 'comment' && <span className={'kind kind-' + e.kind}>{e.kind}</span>}
        <span className="author" title={e.author}>
          {shortName(e.author)}
        </span>
        {e.node && (
          <span className="mono small muted" title={`on trace step ${e.node}`}>
            #{e.node}
          </span>
        )}
        <time dateTime={e.timestamp} title={e.timestamp}>
          {localTime(e.timestamp)}
        </time>
        {(e.ext ?? []).map((x) => (
          <span key={x} className="mono small muted" title="a header field this kalka does not use">
            {x}
          </span>
        ))}
        {bs.map((b) => (
          <button
            key={b.label}
            type="button"
            className={'badge' + (b.warn ? ' warn' : '')}
            aria-expanded={why === b.why}
            onClick={() => setWhy(why === b.why ? '' : b.why)}
          >
            {b.label}
          </button>
        ))}
      </div>
      {why && <div className="badge-why">{why}</div>}
      <Markdown src={e.body} className="entry-body" />
      {e.reply_to && !byId.has(e.reply_to) && <div className="muted small">A reply to a comment that is not in this copy.</div>}
      {e.unparsed && (
        <details className="unparsed">
          <summary>text in the comments that kalka cannot read</summary>
          <pre>{e.unparsed}</pre>
        </details>
      )}
      {seeds.map((k) => (
        <SeedLine key={k.id} e={k} />
      ))}
      <div className="entry-actions">
        {onReply && (
          <button type="button" className="link" onClick={() => onReply(e)}>
            Reply
          </button>
        )}
        {onSeed && e.kind === 'deviation' && e.valid && (
          <button type="button" className="link" onClick={() => onSeed(e)}>
            Split off a seed
          </button>
        )}
      </div>
      {talk.length > 0 && (
        <div className="replies">
          {talk.map((k) => (
            <EntryCard key={k.id} e={k} replies={replies} byId={byId} onReply={onReply} onSeed={onSeed} all={all} />
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
          Replying to {shortName(target.author)}, {localTime(target.timestamp)}
        </div>
      )}
      <div className="row">
        <select value={kind} onChange={(e) => setKind(e.target.value)} aria-label="Kind">
          <option value="comment">Comment</option>
          <option value="deviation">Deviation</option>
        </select>
        {lockAnchor ? (
          <span className="small muted anchor-fixed">on step {anchor.split('#')[1] ?? anchor}</span>
        ) : (
          <select value={anchor} onChange={(e) => setAnchor(e.target.value)} aria-label="Section">
            <option value="">The whole blueprint</option>
            {anchor.includes('#') && (
              <option value={anchor}>
                {doc.invariants.some((r) => !r.domain && `${r.section}#${r.id}` === anchor) ? 'invariant' : 'step'} {anchor.split('#')[1]}
              </option>
            )}
            {doc.headings.map((h) => (
              <option key={h.slug} value={h.slug}>
                {'\u00a0\u00a0'.repeat(Math.max(0, h.level - 1))}
                {h.text}
              </option>
            ))}
          </select>
        )}
      </div>
      <div className="muted small">
        {kind === 'deviation'
          ? 'A deviation records what was done differently from the plan, and why. It stands out for the owner.'
          : 'A comment asks, suggests or agrees. Choose Deviation to record that the work went differently from the plan.'}
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
      <div className="muted small">Markdown works. Ctrl/⌘+Enter to add.</div>
      {err && <div className="error-text">{err}</div>}
      <div className="row end">
        <button type="button" onClick={onCancel} disabled={busy}>
          Cancel
        </button>
        <button type="submit" className="primary" {...act('entry.add')} disabled={busy || !body.trim()}>
          Add {kind}
        </button>
      </div>
    </form>
  )
}

// SectionBody renders a section with its trace and mermaid blocks drawn,
// not shown as code.
function SectionBody({ s, doc, onComment }: { s: Section; doc: Doc; onComment?: (anchor: string) => void }) {
  const link = useLink()
  const parts = useMemo(() => renderParts(s.markdown, s.line, link), [s.markdown, s.line, link])
  return (
    <>
      {parts.map((p, i) => {
        if (p.kind === 'html') return <div key={i} className="md" dangerouslySetInnerHTML={{ __html: p.html }} />
        if (p.kind === 'mermaid') return <Mermaid key={i} source={p.source} />
        if (p.kind === 'invariants') return <InvariantBlock key={i} doc={doc} line={p.line} end={p.end} source={p.source} onComment={onComment} />
        if (p.kind === 'glossary') return <GlossaryBlock key={i} doc={doc} line={p.line} end={p.end} source={p.source} />
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
export function Review({
  doc,
  reload,
  onSeed,
  onPublish,
}: {
  doc: Doc
  reload: () => void
  onSeed?: (e?: Entry) => void
  onPublish?: () => void
}) {
  const [draft, setDraft] = useState<Draft | null>(null)
  const [selection, setSelection] = useState<{ slug: string; text: string; x: number; y: number } | null>(null)
  const [unfold, setUnfold] = useState<Set<string>>(new Set())

  const { bySlug, general, orphaned, replies, byId } = useMemo(() => groupEntries(doc), [doc])
  const link = useMemo(() => linkerOf(doc), [doc])
  // A linked invariant id goes to its card, by click or keyboard.
  const followInvariant = (e: React.MouseEvent | React.KeyboardEvent) => {
    if ('key' in e && e.key !== 'Enter') return
    const el = (e.target as HTMLElement).closest('[data-inv]')
    if (el) showInvariant(el.getAttribute('data-inv') ?? '')
  }

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
    if (!t || !s || s.rangeCount === 0) {
      setSelection(null)
      return
    }
    const r = s.getRangeAt(0).getBoundingClientRect()
    setSelection({ slug, text: t.slice(0, 2000), x: r.left + r.width / 2, y: r.top })
  }
  // The popover is placed where the selection was; it goes when the page moves.
  useEffect(() => {
    if (!selection) return
    const off = () => setSelection(null)
    window.addEventListener('scroll', off, true)
    window.addEventListener('resize', off)
    return () => {
      window.removeEventListener('scroll', off, true)
      window.removeEventListener('resize', off)
    }
  }, [selection])

  const open = (anchor: string, quote?: string) => setDraft({ anchor, quote })
  // A kept domain version is read only (spec 6.12): comments go to the
  // current document.
  const readOnly = !!doc.domain?.viewing
  const onReply = readOnly ? undefined : (e: Entry) => setDraft({ anchor: e.anchor, re: e.id })
  const saved = () => {
    setDraft(null)
    setSelection(null)
    reload()
  }

  // A section's composer also takes comments on its trace steps and invariants.
  const composerFor = (anchor: string) =>
    draft && (draft.anchor === anchor || (anchor !== '' && draft.anchor.startsWith(anchor + '#'))) ? <Composer doc={doc} draft={draft} onCancel={() => setDraft(null)} onSaved={saved} /> : null

  const sectionView = (s: Section, i: number) => {
    const slug = s.heading?.slug ?? ''
    const all = s.heading ? bySlug.get(slug) ?? [] : []
    const old = unfold.has(slug) ? [] : all.filter((e) => oldThread(e, replies))
    const notes = all.filter((e) => !old.includes(e))
    return (
      <section className="sec" key={i} id={s.heading ? 's-' + slug : undefined}>
        <div className="sec-body" onMouseUp={() => s.heading && captureSelection(slug)}>
          <SectionBody s={s} doc={doc} onComment={readOnly ? undefined : (a) => open(a)} />
        </div>
        <aside className="sec-notes">
          {s.heading && (
            <>
              {old.length > 0 && (
                <button type="button" className="link old-threads" onClick={() => setUnfold(new Set([...unfold, slug]))}>
                  {plural(old.length, 'comment')} on an earlier version
                </button>
              )}
              {notes.map((e) => (
                <EntryCard key={e.id} e={e} replies={replies} byId={byId} onReply={onReply} onSeed={onSeed} all={doc.entries} />
              ))}
              {!readOnly &&
                (composerFor(slug) ?? (
                  <button type="button" className="add" onClick={() => open(slug)} aria-label={`Comment on “${s.heading.text}”`}>
                    + Comment
                  </button>
                ))}
            </>
          )}
        </aside>
      </section>
    )
  }

  return (
    <LinkContext.Provider value={link}>
    <div className="review-wrap" onClick={followInvariant} onKeyDown={followInvariant}>
      <Outline doc={doc} bySlug={bySlug} replies={replies} general={general.length} orphaned={orphaned.length} />
    <RefLayer doc={doc} className="review">
      <DocHeader doc={doc} reload={reload} onSeed={onSeed && (() => onSeed())} onPublish={onPublish} />
      <DomainRefStrip doc={doc} />
      <SeedStrip doc={doc} reload={reload} />
      <HintStrip doc={doc} />
      <DiagnosticsPanel doc={doc} />
      <InvariantsPanel doc={doc} />
      <TermsPanel doc={doc} />
      {doc.sections.length === 0 && <p className="muted">This blueprint has no text yet.</p>}
      {selection && !readOnly && !draft && (
        <button
          type="button"
          className="sel-pop primary"
          style={{ left: selection.x, top: Math.max(8, selection.y - 44) }}
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => open(selection.slug, selection.text)}
        >
          Comment
        </button>
      )}
      {doc.sections.map(sectionView)}
      <section className="sec general" id="general">
        <div className="sec-body">
          <h2 className="log-title">General</h2>
          <p className="muted">Comments about the whole blueprint, and its versions.</p>
          {doc.log_lead && (
            <details className="unparsed">
              <summary>text at the start of the comments that kalka cannot read</summary>
              <pre>{doc.log_lead}</pre>
            </details>
          )}
        </div>
        <aside className="sec-notes">
          {general.map((e) => (
            <EntryCard key={e.id} e={e} replies={replies} byId={byId} onReply={onReply} onSeed={onSeed} all={doc.entries} />
          ))}
          {!readOnly &&
            (composerFor('') ?? (
              <button type="button" className="add" onClick={() => open('')}>
                + General comment
              </button>
            ))}
        </aside>
      </section>
      {orphaned.length > 0 && (
        <section className="sec" id="orphaned">
          <div className="sec-body">
            <h2 className="log-title">On removed sections</h2>
            <p className="muted">These were written on sections that are no longer in the plan.</p>
          </div>
          <aside className="sec-notes">
            {orphaned.map((e) => (
              <EntryCard key={e.id} e={e} replies={replies} byId={byId} onReply={onReply} onSeed={onSeed} all={doc.entries} />
            ))}
          </aside>
        </section>
      )}
    </RefLayer>
    </div>
    </LinkContext.Provider>
  )
}

// Outline is the rail beside the document (spec 11.1): its sections with
// how many comments each has and a dot where there is a deviation, the
// general comments, and the map of the document (the canvas).
function Outline({
  doc,
  bySlug,
  replies,
  general,
  orphaned,
}: {
  doc: Doc
  bySlug: Map<string, Entry[]>
  replies: Map<string, Entry[]>
  general: number
  orphaned: number
}) {
  const go = (id: string) => (e: React.MouseEvent) => {
    e.preventDefault()
    document.getElementById(id)?.scrollIntoView({ block: 'start', behavior: 'smooth' })
  }
  const rows = doc.sections.filter((s) => s.heading && s.heading.level <= 3)
  if (rows.length < 2) return null
  return (
    <nav className="doc-outline" aria-label="Outline">
      <div className="doc-outline-title">Outline</div>
      <ul>
        {rows.map((s) => {
          const h = s.heading!
          const all = (bySlug.get(h.slug) ?? []).flatMap((e) => thread(e, replies).map((x) => x.e))
          const notes = all.filter((e) => e.kind !== 'release' && e.kind !== 'budded')
          const dev = notes.some((e) => e.kind === 'deviation')
          return (
            <li key={h.slug} className={'lv' + h.level}>
              <a href={'#s-' + h.slug} onClick={go('s-' + h.slug)}>
                <span className="doc-outline-text">{h.text}</span>
                {dev && <span className="doc-outline-dev" title="has a deviation" aria-label="has a deviation" />}
                {notes.length > 0 && <span className="doc-outline-count">{notes.length}</span>}
              </a>
            </li>
          )
        })}
        <li className="lv2">
          <a href="#general" onClick={go('general')}>
            <span className="doc-outline-text">General</span>
            {general > 0 && <span className="doc-outline-count">{general}</span>}
          </a>
        </li>
        {orphaned > 0 && (
          <li className="lv2">
            <a href="#orphaned" onClick={go('orphaned')}>
              <span className="doc-outline-text">On removed sections</span>
              <span className="doc-outline-count">{orphaned}</span>
            </a>
          </li>
        )}
      </ul>
      <a className="small" href={canvasHref(doc.rel)}>
        Open as map
      </a>
    </nav>
  )
}
