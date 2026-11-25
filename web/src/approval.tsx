// Approval (spec 6.18): the owner's release of the approved version, the
// end of a plan's review. The state comes from the daemon, computed from
// the review log; the page words it, lists the open items before
// approving as information, and never computes the state itself.
import { useState } from 'react'
import { act } from './actions'
import { api, type Approval, type ApproveResult, type Doc } from './api'
import { ErrorConfirm } from './Diagnostics'
import { confirmable } from './diag'
import { ownNodes } from './Goals'
import { Modal } from './Modal'
import { plural } from './util'

/** approvalText words a state: draft, approved v3, changed since approved v3. */
export function approvalText(a?: { state: Approval['state']; version?: number }): string {
  if (!a) return ''
  if (a.state === 'approved') return `approved v${a.version}`
  if (a.state === 'changed') return `changed since approved v${a.version}`
  return 'draft'
}

const TITLE: Record<Approval['state'], string> = {
  draft: 'Not approved yet: the review is open',
  approved: 'Its owner approved this plan; the latest release has the approved text',
  changed: 'A release after the approval changed the plan; its owner has not approved the change yet',
}

// Tones of a badge in the lists and the header, and of a card's small badge.
const TONE: Record<Approval['state'], string> = { draft: '', approved: 'ok', changed: 'warn' }
const SMALL_TONE: Record<Approval['state'], string> = { draft: 'outline', approved: 'ok', changed: 'div' }

/** ApprovalBadge shows a document's approval state; nothing where there is none. */
export function ApprovalBadge({ a, small }: { a?: { state: Approval['state']; version?: number }; small?: boolean }) {
  if (!a) return null
  return (
    <span className={(small ? 'tbadge ' + SMALL_TONE[a.state] : 'badge ' + TONE[a.state]) + ' approval ' + a.state} title={TITLE[a.state]}>
      {approvalText(a)}
    </span>
  )
}

/** openItems are the document's open structural items, listed before
 * approving as information: unclaimed goals, criteria no trace step
 * covers, unguarded or unverified invariants, and errors. Nothing here
 * blocks an approval, and approving changes none of them. */
export function openItems(doc: Doc, errors = true): string[] {
  const out: string[] = []
  for (const n of ownNodes(doc)) {
    if (n.kind === 'goal' && n.state === 'unclaimed') out.push(`goal ${n.id} is unclaimed`)
    if (n.kind === 'criterion' && (n.covered_by ?? []).length === 0) out.push(`criterion ${n.id}: no trace step covers it`)
  }
  for (const r of doc.invariants ?? []) {
    if (r.gaps.includes('unguarded')) out.push(`invariant ${r.ref}: no trace step guards it`)
    if (r.gaps.includes('unverified')) out.push(`invariant ${r.ref}: nothing says how it is verified`)
  }
  if (errors) for (const p of doc.problems) if (p.severity === 'error') out.push(`${p.rule}: ${p.message}`)
  return out
}

/** approveRole is who may approve the open document now: its owner, or a
 * team owner taking it over first (spec 7.4); '' when nobody here may or
 * there is nothing to approve. */
export function approveRole(doc: Doc): '' | 'owner' | 'take-over' {
  const t = doc.team
  if (!doc.approval || !t?.held || t.version === 0 || t.disputed || doc.domain || doc.seed || doc.transfer?.moving_to) return ''
  if (doc.approval.state === 'approved' && !t.unreleased) return ''
  if (t.can_act && !t.reassigned_to) return 'owner'
  if (t.team_owner) return 'take-over'
  return ''
}

/** approveVersion is the version an approval approves: the next one when
 * unreleased edits are released with it. */
export function approveVersion(doc: Doc, role: '' | 'owner' | 'take-over'): number {
  const t = doc.team!
  return t.unreleased || role === 'take-over' ? t.version + 1 : t.version
}

/**
 * ApproveDialog is Approve vN (spec 6.18): with unreleased edits, the
 * release dialog (a note, the deviations it takes up, "anyway" over
 * errors) and the open items, behind one confirmation; without, the open
 * items and an optional note. After the approval, when the document names
 * an issue and a plugin can export, it asks whether to attach the
 * approved version there; a failed export leaves the approval as it is.
 */
export function ApproveDialog({ doc, takeOver, onClose, onDone }: { doc: Doc; takeOver: boolean; onClose: () => void; onDone: () => void }) {
  const t = doc.team!
  const edits = !!t.unreleased
  const next = approveVersion(doc, takeOver ? 'take-over' : 'owner')
  const devs = doc.entries.filter((e) => e.valid && e.kind === 'deviation' && !e.reply_to && !e.incorporated_in)
  const items = openItems(doc, !edits)
  const errs = edits ? confirmable(doc).length : 0
  const [note, setNote] = useState('')
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [done, setDone] = useState<ApproveResult | null>(null)
  const [attach, setAttach] = useState<'' | 'done' | 'failed'>('')
  const heading = (slug: string) => doc.headings.find((h) => h.slug === slug)?.text ?? slug
  const issue = doc.issue
  const canAttach = !!issue && !!issue.caps?.export
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault()
    setBusy(true)
    setErr('')
    try {
      const r = await api.approve(doc.rel, note, [...picked], takeOver)
      if (!canAttach || !r.changed) {
        onDone()
        return
      }
      setDone(r)
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy(false)
  }
  if (done) {
    return (
      <Modal title={`Approved version ${done.version}`} busy={busy} onClose={onDone}>
        {attach === 'done' ? (
          <p>Approved v{done.version} is attached to {issue!.payload}.</p>
        ) : (
          <p>
            Attach approved v{done.version} to {issue!.payload}?{' '}
            <span className="muted small">The export sends the approved version with your own credentials for the tracker and records it in the review log.</span>
          </p>
        )}
        {attach === 'failed' && <p className="error-text">The export failed: {err}. The approval stands; you can export later from the document bar.</p>}
        <div className="row end">
          {attach !== 'done' && (
            <>
              <button type="button" onClick={onDone} disabled={busy}>
                Not now
              </button>
              <button
                type="button"
                className="primary"
                {...act('doc.export')}
                disabled={busy}
                onClick={async () => {
                  setBusy(true)
                  setErr('')
                  try {
                    await api.exportDoc(done.path)
                    setAttach('done')
                  } catch (e) {
                    setErr((e as Error).message)
                    setAttach('failed')
                  }
                  setBusy(false)
                }}
              >
                {busy ? 'Attaching…' : 'Attach'}
              </button>
            </>
          )}
          {attach === 'done' && (
            <button type="button" className="primary" onClick={onDone}>
              Done
            </button>
          )}
        </div>
      </Modal>
    )
  }
  return (
    <Modal title={`Approve version ${next}`} busy={busy} onClose={onClose}>
      <form onSubmit={submit} className="seed-form">
        <p className="muted">
          {takeOver
            ? `You become its owner first, which is recorded in the team history: the plan as you have it is released as version ${next} in your name, and approved.`
            : edits
              ? `Your edits are released as version ${next} for every member of ${t.team}, and version ${next} is approved.`
              : `Version ${next}, as the team has it, is approved.`}{' '}
          The approval holds until a release changes the plan's text; it says the review is done, not that the work is.
        </p>
        <label>
          {edits ? 'What changed' : 'Note'} <span className="muted">(optional)</span>
          <textarea rows={2} value={note} onChange={(e) => setNote(e.target.value)} />
        </label>
        {edits && devs.length > 0 && (
          <fieldset className="incorporate">
            <legend>Deviations this version takes up</legend>
            {devs.map((e) => (
              <label key={e.id} className="check">
                <input
                  type="checkbox"
                  checked={picked.has(e.id)}
                  onChange={(ev) => {
                    const n = new Set(picked)
                    if (ev.target.checked) n.add(e.id)
                    else n.delete(e.id)
                    setPicked(n)
                  }}
                />
                <span>
                  <b>{e.author}</b> on “{e.anchor ? heading(e.anchor) : 'the whole plan'}”: {e.body.length > 120 ? e.body.slice(0, 120) + '…' : e.body}
                </span>
              </label>
            ))}
          </fieldset>
        )}
        <div className="approve-open">
          {items.length > 0 ? (
            <>
              <p className="small">
                {plural(items.length, 'open item')}, for your information; they do not stop the approval, and it changes none of them:
              </p>
              <ul className="small">
                {items.map((s) => (
                  <li key={s}>{s}</li>
                ))}
              </ul>
            </>
          ) : (
            <p className="muted small">No open items.</p>
          )}
        </div>
        {edits && <ErrorConfirm doc={doc} verb="Release" />}
        {err && <p className="error-text">{err}</p>}
        <div className="row">
          <button type="submit" className="primary" {...act('doc.approve')} disabled={busy}>
            {takeOver ? 'Take over and approve' : `Approve v${next}`}
            {errs > 0 ? ' anyway' : ''}
          </button>
          <button type="button" onClick={onClose}>
            Cancel
          </button>
        </div>
      </form>
    </Modal>
  )
}
