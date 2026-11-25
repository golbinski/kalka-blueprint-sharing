// The issue a blueprint addresses (spec 10.5, 11.1): a chip with the key,
// title and status in the document bar, the parent chain beside it, and
// the export of a released version. What the tracker says comes from the
// reader's own plugin; with none, only the key from the frontmatter.
import { useState } from 'react'
import { act } from './actions'
import { api, type Doc, type ExportResult, type IssueView } from './api'
import { Modal } from './Modal'
import { STATUS_LABEL } from './groups'
import { localTime } from './util'

/** IssueChip shows the issue's key, title and status, linked. */
export function IssueChip({ issue }: { issue: IssueView }) {
  const m = issue.meta?.ok ? issue.meta : undefined
  const why = issue.pending && !m ? 'Fetching the issue…' : issue.error
  const asOf = m && issue.stale && issue.as_of ? `as of ${localTime(issue.as_of)}` : ''
  const label = (
    <>
      <span className="issue-key">{issue.payload}</span>
      {m?.title && <span className="issue-title">{m.title}</span>}
    </>
  )
  return (
    <span className="issue-chip" title={[m?.title, why, asOf].filter(Boolean).join(' · ') || issue.issue}>
      {m?.url ? (
        <a href={m.url} target="_blank" rel="noreferrer" aria-label={`Issue ${issue.payload}${m.title ? ': ' + m.title : ''}`}>
          {label}
        </a>
      ) : (
        <span>{label}</span>
      )}
      {m?.status && (
        <span className={'issue-status ' + m.status.category} title={STATUS_LABEL[m.status.category]}>
          {m.status.name}
        </span>
      )}
      {asOf && <span className="issue-asof">{asOf}</span>}
    </span>
  )
}

/** IssueParents is the parent chain, farthest first, as a small breadcrumb. */
export function IssueParents({ issue }: { issue: IssueView }) {
  const ps = issue.meta?.ok ? (issue.meta.parents ?? []) : []
  if (ps.length === 0) return null
  return (
    <ol className="issue-parents" aria-label="Parent issues">
      {[...ps].reverse().map((p) => (
        <li key={p.issue} title={[p.type, p.title].filter(Boolean).join(': ')}>
          {p.url ? (
            <a href={p.url} target="_blank" rel="noreferrer">
              {p.issue}
            </a>
          ) : (
            p.issue
          )}
        </li>
      ))}
    </ol>
  )
}

/** canArchive: the owner, or a team owner taking it over (spec 7.4). */
export function archiveRole(doc: Doc): '' | 'owner' | 'take-over' {
  if (doc.domain) return ''
  if (doc.scope === 'personal' || doc.role === 'local') return 'owner'
  if (doc.team?.held && doc.team.can_act) return 'owner'
  if (doc.team?.held && doc.team.team_owner) return 'take-over'
  return doc.role === 'owner' ? 'owner' : ''
}

/** ExportButton opens the export dialog; disabled with the reason when it cannot. */
export function ExportButton({ doc, reload }: { doc: Doc; reload: () => void }) {
  const [open, setOpen] = useState(false)
  const st = doc.export
  if (!doc.issue || !st || !doc.issue.caps?.export) return null
  return (
    <>
      <button
        type="button"
        className="docbar-export"
        {...act('doc.export')}
        disabled={!st.can}
        title={st.can ? `Export version ${st.version} to ${doc.issue.payload}` : st.why}
        onClick={() => setOpen(true)}
      >
        Export…
      </button>
      {open && <ExportDialog doc={doc} onClose={() => setOpen(false)} reload={reload} />}
    </>
  )
}

function ExportDialog({ doc, onClose, reload }: { doc: Doc; onClose: () => void; reload: () => void }) {
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [done, setDone] = useState<ExportResult | null>(null)
  const issue = doc.issue!
  const st = doc.export!
  const what = [
    issue.caps?.export?.attachment && 'attaches the whole file',
    issue.caps?.export?.comment && 'writes a comment with the plan and the accepted deviations',
  ]
    .filter(Boolean)
    .join(' and ')
  const run = async (f: () => Promise<void>) => {
    setBusy(true)
    setErr('')
    try {
      await f()
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy(false)
  }
  return (
    <Modal title={done ? `Exported to ${issue.payload}` : `Export version ${st.version} to ${issue.payload}`} busy={busy} onClose={onClose}>
      {!done ? (
        <>
          <p>
            kalka {what || 'sends the released version'} on {issue.meta?.title ? `“${issue.meta.title}”` : issue.payload}, with your own credentials for the
            tracker, and records the export in the review log for the team.
          </p>
          {st.last && (
            <p className="muted small">
              Version {st.last.version} was exported {st.last_at ? 'on ' + localTime(st.last_at) : ''}
              {st.last_by ? ' by ' + st.last_by : ''}; this replaces that attachment and updates the same comment.
            </p>
          )}
          {st.approval && (
            <p className={st.approval.startsWith('approved') ? 'small' : 'small warn-text'}>
              {st.approval.startsWith('approved')
                ? `Version ${st.version} is the approved version (${st.approval}).`
                : `Version ${st.version} is not approved: ${st.approval}.`}
            </p>
          )}
          <p className="muted small">Only the released version goes; edits after it stay in kalka. Nothing in the issue comes back into the blueprint.</p>
          {err && <p className="error-text">{err}</p>}
          <div className="row end">
            <button type="button" onClick={onClose} disabled={busy}>
              Cancel
            </button>
            <button
              type="button"
              className="primary"
              {...act('doc.export')}
              disabled={busy}
              onClick={() =>
                run(async () => {
                  setDone(await api.exportDoc(doc.rel))
                  reload()
                })
              }
            >
              {busy ? 'Exporting…' : 'Export'}
            </button>
          </div>
        </>
      ) : (
        <>
          <p>
            Version {done.version} is on {done.issue}
            {done.updated ? ', replacing the earlier export' : ''}.{' '}
            {done.url && (
              <a href={done.url} target="_blank" rel="noreferrer">
                Open the issue
              </a>
            )}
          </p>
          {done.approval && <p className="muted small">{done.approved ? 'It is the approved version.' : `It is not approved: ${done.approval}.`}</p>}
          <p className="muted small">The plan stays here, as the reference the work is done against and the context later plans are read in.</p>
          {err && <p className="error-text">{err}</p>}
          <div className="row end">
            <button type="button" className="primary" onClick={onClose} disabled={busy}>
              Done
            </button>
          </div>
        </>
      )}
    </Modal>
  )
}
