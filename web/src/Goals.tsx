// The plan graph in the document view (spec 6.18, 11.1): a goals block
// drawn as goals and acceptance criteria with their computed states, each
// goal with the tree of child goals and criteria that claim it.
import { CodeWithNote, Linked } from './Aids'
import type { Doc, GoalNode, GoalState } from './api'
import { docHref, plural } from './util'

const STATE: Record<GoalState, { label: string; title: string; tone: string }> = {
  unclaimed: { label: 'unclaimed', title: 'No child goal and no criterion realizes it yet', tone: 'div' },
  claimed: { label: 'claimed', title: 'Realized by child goals or criteria, not all of them delivered', tone: 'outline' },
  ready: { label: 'ready', title: 'Everything claiming it is delivered: the owner can close the document', tone: 'ok' },
  delivered: { label: 'delivered', title: 'Ready, and its document is closed', tone: 'ok' },
  open: { label: 'open', title: 'Its document is not closed yet', tone: 'outline' },
}

export function StateBadge({ state }: { state: GoalState }) {
  const s = STATE[state]
  return (
    <span className={'tbadge goal-state ' + s.tone} title={s.title}>
      {s.label}
    </span>
  )
}

/** ownNodes are the document's own goals and criteria in the plan view's
 * tree, at the top level and under its own goals. */
export function ownNodes(doc: Doc): GoalNode[] {
  const out: GoalNode[] = []
  const walk = (ns: GoalNode[]) => {
    for (const n of ns) {
      if (!n.own) continue
      out.push(n)
      walk(n.claimants ?? [])
    }
  }
  walk(doc.plan?.goals ?? [])
  return out
}

/** Claimants is the tree of what claims a goal, from other documents. */
function Claimants({ doc, nodes }: { doc: Doc; nodes: GoalNode[] }) {
  const rels = new Map((doc.plan?.children ?? []).map((c) => [c.kalka_id, c.rel]))
  if (nodes.length === 0) return null
  return (
    <ul className="plain goal-claimants">
      {nodes.map((n) => {
        const rel = n.doc ? rels.get(n.doc) : undefined
        const where = (n.issue ? n.issue.replace(/^[a-z]+:/, '') + ' ' : '') + (n.title ?? '')
        return (
          <li key={(n.doc ?? '') + n.kind + n.id}>
            <StateBadge state={n.state} /> <span className="mono">{n.id}</span> <Linked text={n.text} />{' '}
            {!n.own && (
              <span className="muted small">
                · {rel ? <a href={docHref(rel)}>{where}</a> : where}
              </span>
            )}
            <Claimants doc={doc} nodes={(n.claimants ?? []).filter((c) => !c.own || !n.own)} />
          </li>
        )
      })}
    </ul>
  )
}

/** GoalsBlock draws a goals block of the body: each goal and criterion a
 * comment anchor, with its state; a block with errors stays code. */
export function GoalsBlock({
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
  const items = ownNodes(doc).filter((n) => (n.line ?? 0) > line && (n.line ?? 0) < end)
  if (items.length === 0) return <CodeWithNote source={source} />
  return (
    <div className="goals-block">
      {items.map((n) => {
        const anchor = n.section ? `${n.section}#${n.id}` : ''
        const notes = anchor ? doc.entries.filter((e) => e.anchor === anchor && e.kind !== 'release' && e.kind !== 'budded').length : 0
        const others = (n.claimants ?? []).filter((c) => !c.own)
        return (
          <div key={n.kind + n.id} className={'goal-card ' + n.kind} id={'goal-' + n.id}>
            <div className="row center">
              <span className="muted small">{n.kind === 'goal' ? 'Goal' : 'Criterion'}</span>
              <span className="mono b">{n.id}</span>
              <StateBadge state={n.state} />
              {(n.realizes ?? []).length > 0 && <span className="muted small">realizes {n.realizes!.join(', ')}</span>}
              <span className="grow" />
              {notes > 0 && <span className="muted small">{plural(notes, 'comment')}</span>}
              {onComment && anchor && (
                <button type="button" className="link small" onClick={() => onComment(anchor)} aria-label={`Comment on ${n.kind} ${n.id}`}>
                  + Comment
                </button>
              )}
            </div>
            <div className="goal-text">
              <Linked text={n.text} />
            </div>
            {(n.steps ?? []).length > 0 && (
              <ul className="plain goal-steps">
                {n.steps!.map((s) => (
                  <li key={s.line}>
                    <b>{s.keyword}</b> <Linked text={s.text} />
                  </li>
                ))}
              </ul>
            )}
            {n.kind === 'criterion' && (
              <div className="small muted">
                {(n.covered_by ?? []).length > 0 ? <>Covered by trace step {n.covered_by!.map((c) => c.node).join(', ')}</> : 'No trace step covers it'}
              </div>
            )}
            {n.kind === 'goal' && others.length > 0 && <Claimants doc={doc} nodes={others} />}
          </div>
        )
      })}
    </div>
  )
}
