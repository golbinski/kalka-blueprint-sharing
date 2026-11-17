// Review aids (spec 6.15, 6.16, 11.1): glossary terms and invariant ids
// linked in the page, the Invariants and Terms panels, the typed blocks
// drawn in the body, and what a domain release would mean for held
// blueprints.
import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import { api, type Affected, type Doc, type Entry, type InvariantRow } from './api'
import { Matcher } from './glossary'
import { linkPlain, termCounts, type Linker } from './markdown'
import { plural } from './util'

/** The document's linker, for the parts of the page that render text. */
export const LinkContext = createContext<Linker | undefined>(undefined)

export function useLink(): Linker | undefined {
  return useContext(LinkContext)
}

/** linkerOf links the glossary the daemon resolved and the invariants of the matrix. */
export function linkerOf(doc: Doc): Linker {
  const invariants = new Map<string, string>()
  for (const r of doc.invariants) invariants.set(r.ref, r.statement ?? '')
  const terms = doc.glossary.state === 'held' && doc.glossary.terms.length > 0 ? new Matcher(doc.glossary.terms) : undefined
  return { terms, invariants }
}

/** Linked renders plain text with terms and invariant ids linked. */
export function Linked({ text, className }: { text: string; className?: string }) {
  const link = useLink()
  // linkPlain escapes the text and what it inserts.
  return <span className={className} dangerouslySetInnerHTML={{ __html: linkPlain(text, link) }} />
}

/** invAnchor is the comment anchor of one of the document's own invariants. */
export const invAnchor = (r: Pick<InvariantRow, 'section' | 'id'>) => (r.section ? `${r.section}#${r.id}` : '')

/** invElementId is the id of an invariant's card in the body. */
export const invElementId = (ref: string) => 'inv-' + ref.replace(/[^A-Za-z0-9_-]/g, '-')

/** showInvariant scrolls to an invariant's card, or its row in the panel. */
export function showInvariant(ref: string) {
  const el = document.getElementById(invElementId(ref)) ?? document.getElementById('row-' + invElementId(ref))
  if (!el) return
  const fold = el.closest('details')
  if (fold && !fold.open) fold.open = true
  el.scrollIntoView({ block: 'center', behavior: 'smooth' })
  el.classList.add('flash')
  setTimeout(() => el.classList.remove('flash'), 2000)
}

const notesOn = (entries: Entry[], anchor: string) =>
  anchor ? entries.filter((e) => e.anchor === anchor && e.kind !== 'release' && e.kind !== 'budded').length : 0

function Gaps({ r }: { r: InvariantRow }) {
  return (
    <>
      {r.gaps.includes('unguarded') && (
        <span className="tbadge div" title="No trace step lists it under guards (I030)">
          not guarded
        </span>
      )}
      {r.gaps.includes('unverified') && (
        <span className="tbadge div" title="It has no verified_by (I031)">
          not verified
        </span>
      )}
      {!r.resolved && (
        <span className="tbadge div" title="The referenced domain version does not have it, or is not here">
          not found
        </span>
      )}
    </>
  )
}

const STATUS: Record<string, string> = { new: 'new', changed: 'changed', removed: 'removed' }

/** InvariantsPanel is the invariants matrix (spec 11.1). */
export function InvariantsPanel({ doc }: { doc: Doc }) {
  const rows = doc.invariants
  if (rows.length === 0) return null
  const gaps = rows.filter((r) => r.gaps.length > 0 || !r.resolved).length
  return (
    <details className="fold invariants" open={gaps > 0}>
      <summary>
        <b>Invariants</b>{' '}
        <span className="muted small">
          · {plural(rows.length, 'invariant')}
          {gaps > 0 ? ` · ${gaps} with gaps` : ''}
        </span>
      </summary>
      <table className="inv-table">
        <thead>
          <tr>
            <th>Invariant</th>
            <th>Guarded by</th>
            <th>Verified by</th>
            <th>{doc.domain ? 'Referenced by' : 'Comments'}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.ref} id={'row-' + invElementId(r.ref)} className={r.status === 'removed' ? 'removed' : undefined}>
              <td>
                <button type="button" className="link mono" onClick={() => showInvariant(r.ref)}>
                  {r.ref}
                </button>{' '}
                {r.status && <span className="tbadge outline">{STATUS[r.status]}</span>} <Gaps r={r} />
                <div>{r.statement ? <Linked text={r.statement} /> : <span className="muted">not found in {doc.glossary.ref ?? 'the domain'}</span>}</div>
              </td>
              <td className="mono small">{r.guarded_by.length ? r.guarded_by.map((g) => g.node).join(', ') : <span className="muted">none</span>}</td>
              <td className="small">{r.verified_by.length ? r.verified_by.join('; ') : <span className="muted">none</span>}</td>
              <td className="small">
                {doc.domain
                  ? (r.referenced_by ?? []).join(', ') || <span className="muted">none here</span>
                  : notesOn(doc.entries, invAnchor(r)) || <span className="muted">none</span>}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </details>
  )
}

/** TermsPanel lists the glossary terms the document uses, with counts. */
export function TermsPanel({ doc }: { doc: Doc }) {
  const link = useLink()
  const g = doc.glossary
  const counts = useMemo(() => {
    if (!link?.terms) return new Map<number, number>()
    const texts = doc.sections.map((s) => s.markdown)
    for (const t of doc.traces) for (const n of t.nodes ?? []) texts.push(n.text)
    for (const r of doc.invariants) if (!r.domain && r.statement) texts.push(r.statement)
    return termCounts(texts, link.terms)
  }, [doc, link])
  if (g.state === 'not-kept')
    return (
      <p className="muted small terms-note" role="note">
        Glossary {g.ref} not kept here: terms are not linked or checked.
      </p>
    )
  if (g.state !== 'held' || counts.size === 0) return null
  const used = [...counts.entries()].sort((a, b) => b[1] - a[1] || g.terms[a[0]].term.localeCompare(g.terms[b[0]].term))
  const aliases = doc.problems.filter((p) => p.rule === 'G020').length
  return (
    <details className="fold terms">
      <summary>
        <b>Terms</b>{' '}
        <span className="muted small">
          · {plural(used.length, 'term')} from {g.ref}
          {aliases > 0 ? ` · ${plural(aliases, 'alias', 'aliases')} used` : ''}
        </span>
      </summary>
      <ul className="plain term-list">
        {used.map(([i, n]) => {
          const t = g.terms[i]
          return (
            <li key={i}>
              <b>{t.term}</b> <span className="muted small">×{n}</span> — {t.definition}
              {t.context && <span className="muted small"> ({t.context})</span>}
            </li>
          )
        })}
      </ul>
    </details>
  )
}

/** InvariantBlock draws an invariants block of the body as cards, each a
 * comment anchor; a block with errors stays code. */
export function InvariantBlock({
  doc,
  line,
  end,
  source,
  onComment,
}: {
  doc: Doc
  line: number
  end: number
  source: string
  onComment?: (anchor: string) => void
}) {
  const rows = doc.invariants.filter((r) => !r.domain && r.line > line && r.line < end)
  const affects = doc.invariants.filter((r) => r.domain && r.line > line && r.line < end)
  if (rows.length === 0 && affects.length === 0) return <CodeWithNote source={source} />
  return (
    <div className="inv-block">
      {affects.length > 0 && (
        <div className="small muted">
          Affects{' '}
          {affects.map((r, i) => (
            <span key={r.ref}>
              {i > 0 && ', '}
              <span className="inv-ref mono" tabIndex={0} role="link" data-inv={r.ref} data-def={r.statement || 'not found in the domain version'}>
                {r.ref}
              </span>
            </span>
          ))}
        </div>
      )}
      {rows.map((r) => {
        const anchor = invAnchor(r)
        const n = notesOn(doc.entries, anchor)
        return (
          <div key={r.id} className={'inv-card' + (r.status === 'removed' ? ' removed' : '')} id={invElementId(r.ref)}>
            <div className="row center">
              <span className="mono b">{r.id}</span>
              {r.status && <span className="tbadge outline">{STATUS[r.status]}</span>}
              <Gaps r={r} />
              <span className="grow" />
              {n > 0 && <span className="muted small">{plural(n, 'comment')}</span>}
              {onComment && anchor && (
                <button type="button" className="link small" onClick={() => onComment(anchor)} aria-label={`Comment on invariant ${r.id}`}>
                  + Comment
                </button>
              )}
            </div>
            <div className="inv-statement">
              <Linked text={r.statement ?? ''} />
            </div>
            {r.why && (
              <div className="small">
                <b>Why:</b> {r.why}
              </div>
            )}
            <div className="small">
              <b>Verified by:</b> {r.verified_by.length ? r.verified_by.join('; ') : <span className="muted">nothing yet</span>}
              {' · '}
              <b>Guarded by:</b> {r.guarded_by.length ? r.guarded_by.map((g) => g.node).join(', ') : <span className="muted">no trace step</span>}
            </div>
          </div>
        )
      })}
    </div>
  )
}

/** GlossaryBlock draws a domain document's glossary block as a list of definitions. */
export function GlossaryBlock({ doc, line, end, source }: { doc: Doc; line: number; end: number; source: string }) {
  const terms = doc.domain ? doc.glossary.terms.filter((t) => t.line > line && t.line < end) : []
  if (terms.length === 0) return <CodeWithNote source={source} />
  return (
    <dl className="gloss-block">
      {terms.map((t) => (
        <div key={t.term} className="gloss-entry">
          <dt>
            <b>{t.term}</b>
            {(t.forms ?? []).length > 0 && <span className="muted small"> · {t.forms!.join(', ')}</span>}
            {t.context && <span className="tbadge outline">{t.context}</span>}
          </dt>
          <dd>
            {t.definition}
            {(t.aliases ?? []).length > 0 && <div className="small muted">Not: {t.aliases!.join(', ')}</div>}
            {(t.see ?? []).length > 0 && <div className="small muted">See: {t.see!.join(', ')}</div>}
          </dd>
        </div>
      ))}
    </dl>
  )
}

function CodeWithNote({ source }: { source: string }) {
  return (
    <>
      <pre className="md-pre">
        <code>{source}</code>
      </pre>
      <p className="muted small">Shown as code: kalka cannot read this block. Checks says why.</p>
    </>
  )
}

/** GuardsList is the invariants a trace step guards, with their statements. */
export function GuardsList({ doc, guards }: { doc: Doc; guards?: string[] }) {
  if (!guards?.length) return null
  return (
    <div className="mt2">
      <b>Guards:</b>{' '}
      {guards.map((g, i) => {
        const r = doc.invariants.find((x) => x.ref === g)
        return (
          <span key={g}>
            {i > 0 && ', '}
            <button type="button" className="link mono" title={r?.statement || 'not found'} onClick={() => showInvariant(g)}>
              {g}
            </button>
            {r?.statement && <span className="muted"> ({r.statement})</span>}
          </span>
        )
      })}
    </div>
  )
}

/** ReleaseImpact shows the owner of a domain document what releasing it
 * would mean for the held blueprints referencing it (I040, G030). */
export function ReleaseImpact({ doc }: { doc: Doc }) {
  const [list, setList] = useState<Affected[] | null>(null)
  useEffect(() => {
    let live = true
    api.domainImpact(doc.rel).then(
      (l) => live && setList(l),
      () => live && setList([]),
    )
    return () => {
      live = false
    }
  }, [doc.rel, doc.rev])
  if (!list || list.length === 0) return null
  return (
    <div className="confirm-box" role="note">
      <p>
        {plural(list.length, 'blueprint')} held here would notice this release when they move to it:
      </p>
      <ul className="plain diag-list">
        {list.map((a) => (
          <li key={a.path}>
            <b>{a.title || a.path}</b>
            <ul className="plain">
              {a.problems.map((p, i) => (
                <li key={i} className="small">
                  {p.message} <span className="muted mono">{p.rule}</span>
                </li>
              ))}
            </ul>
          </li>
        ))}
      </ul>
    </div>
  )
}
