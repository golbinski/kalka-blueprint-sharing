// The document header (spec 11.1): one card with the blueprint's state in
// one sentence, what follows from it, at most one primary action, and the
// rest under More. Changing the owner, deleting for the team and taking a
// blueprint over open as a confirmation inside the card.
import { useState } from 'react'
import { ErrorConfirm } from './Diagnostics'
import { confirmable } from './diag'
import { act } from './actions'
import { api, type Doc } from './api'
import { docState } from './docstate'
import { Menu, type MenuItem } from './Menu'
import { MemberSelect, useMembers } from './Members'
import { Modal } from './Modal'
import { attestedText } from './scope'
import { TransferDialog } from './Transfer'
import { docHref, plural, warningOf } from './util'

type Panel = '' | 'owner' | 'delete' | 'take-release' | 'take-delete'

export function DocHeader({ doc, reload, onSeed, onPublish }: { doc: Doc; reload: () => void; onSeed?: () => void; onPublish?: () => void }) {
  const [panel, setPanel] = useState<Panel>('')
  const [transfer, setTransfer] = useState<'' | 'copy' | 'move'>('')
  const [releasing, setReleasing] = useState(false)
  const [note, setNote] = useState('')
  const [to, setTo] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [warn, setWarn] = useState('')
  const t = doc.team
  const dm = doc.domain
  const members = useMembers(panel === 'owner' ? t?.team : undefined)
  const st = docState(doc)
  const run = async (f: () => Promise<unknown>) => {
    setBusy(true)
    setErr('')
    setWarn('')
    try {
      setWarn(warningOf(await f()))
      setNote('')
      setTo('')
      setPanel('')
      reload()
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy(false)
  }

  const live = !!t?.held && t.version > 0 && !dm?.viewing
  const takeOver = live && !!t?.team_owner && !t?.can_act
  const handoff = !!t?.can_act
  const errors = doc.problems.some((p) => p.blocks)
  const [askErr, setAskErr] = useState(false)
  const more: MenuItem[] = []
  if (live && (t!.can_act || t!.team_owner) && st.primary !== 'choose') {
    more.push({
      label: 'Change owner…',
      sub: handoff ? 'hand it to a teammate' : 'as team owner; the current owner is not asked',
      onSelect: () => setPanel('owner'),
    })
  }
  if (onSeed && !dm?.viewing && !errors) more.push({ label: 'Split off a seed…', sub: 'park an idea without changing the plan', onSelect: onSeed })
  if (live && (t!.can_act || t!.team_owner) && !t!.disputed && !doc.transfer?.moving_to) {
    more.push({ label: 'Copy to another team…', sep: true, onSelect: () => setTransfer('copy') })
    more.push({ label: 'Move to another team…', onSelect: () => setTransfer('move') })
  }
  if (takeOver) more.push({ label: 'Take over and release…', sub: 'you become its owner, then release it', sep: true, onSelect: () => setPanel('take-release') })
  if (live && t!.can_act) more.push({ label: 'Delete for the team…', danger: true, sep: true, onSelect: () => setPanel('delete') })
  if (takeOver) more.push({ label: 'Take over and delete…', danger: true, onSelect: () => setPanel('take-delete') })

  const versions = dm ? [...dm.versions].sort((a, b) => b - a) : []
  return (
    <section className={'doc-head' + (st.tone ? ' ' + st.tone : '')} aria-label="Blueprint state">
      {askErr && <ErrorConfirm doc={doc} verb="Release" />}
      <div className="doc-head-main">
        <div className="grow">
          <div className="doc-head-state">{st.head}</div>
          <div className="doc-head-sub">
            {st.sub}
            {dm?.viewing ? (
              <>
                {' '}
                <a href={docHref(doc.rel)}>Back to the current version</a>
              </>
            ) : null}
          </div>
        </div>
        {st.primary === 'release' && (
          <form
            className="row"
            onSubmit={(e) => {
              e.preventDefault()
              if (confirmable(doc).length > 0 && !askErr) {
                setAskErr(true)
                return
              }
              run(() => api.release(doc.rel, note, []))
            }}
          >
            <input className="release-note" placeholder="What changed (optional)" aria-label="Release note" value={note} disabled={busy} onChange={(e) => setNote(e.target.value)} />
            <button type="submit" className="primary" {...act('doc.release')} disabled={busy}>
              Release v{t!.version + 1}
              {askErr ? ' anyway' : ''}
            </button>
            {askErr && (
              <button type="button" onClick={() => setAskErr(false)}>
                Cancel
              </button>
            )}
          </form>
        )}
        {st.primary === 'domain-release' && (
          <button type="button" className="primary" onClick={() => setReleasing(true)}>
            Release v{(dm?.version ?? 0) + 1}…
          </button>
        )}
        {st.primary === 'publish' && onPublish && (
          <button type="button" className="primary" onClick={onPublish}>
            Publish to team…
          </button>
        )}
        {st.primary === 'choose' && panel !== 'owner' && (
          <button type="button" className="primary" onClick={() => setPanel('owner')}>
            Choose owner…
          </button>
        )}
        {versions.length > 0 && (
          <select
            className="version-switch"
            aria-label="Version"
            value={dm?.viewing ?? 0}
            onChange={(e) => {
              location.hash = docHref(doc.rel, Number(e.target.value) || undefined)
            }}
          >
            <option value={0}>current</option>
            {versions.map((v) => (
              <option key={v} value={v}>
                v{v}
              </option>
            ))}
          </select>
        )}
        <Menu label="More ▾" items={more} className="more" />
      </div>

      {panel === 'owner' && t && (
        <form
          className="doc-head-panel row"
          onSubmit={(e) => {
            e.preventDefault()
            run(() => (handoff ? api.handoff(doc.rel, to) : api.reassign(doc.rel, to)))
          }}
        >
          <span className="small">
            {handoff
              ? 'They become the owner at once and release the next version. You can still comment.'
              : 'As a team owner you choose the next owner; the current owner is not asked.'}
          </span>
          <MemberSelect members={members} value={to} onChange={setTo} exclude={t.releaser && !t.disputed ? [t.releaser] : []} label="New owner" />
          {handoff ? (
            <button type="submit" className="primary" {...act('doc.handoff')} disabled={busy || !to}>
              Make owner
            </button>
          ) : (
            <button type="submit" className="primary" {...act('doc.reassign')} disabled={busy || !to}>
              Make owner
            </button>
          )}
          <button type="button" className="link" onClick={() => setPanel('')}>
            Cancel
          </button>
        </form>
      )}
      {panel === 'delete' && (
        <div className="doc-head-panel row">
          <span className="small">Delete for every member of {t?.team}? The last copy goes to the team folder's trash.</span>
          <button type="button" className="danger" {...act('doc.delete')} disabled={busy} onClick={() => run(() => api.deleteForTeam(doc.rel))}>
            Delete for the team
          </button>
          <button type="button" onClick={() => setPanel('')}>
            Keep it
          </button>
        </div>
      )}
      {(panel === 'take-release' || panel === 'take-delete') && (
        <div className="doc-head-panel row">
          <span className="small">
            You become its owner first, then {panel === 'take-release' ? 'release the plan as you have it.' : 'delete it for every member.'} This is
            recorded in the team's history.
          </span>
          {panel === 'take-release' ? (
            <button type="button" className="primary" {...act('doc.release')} disabled={busy} onClick={() => run(() => api.release(doc.rel, '', [], true))}>
              Take over and release
            </button>
          ) : (
            <button type="button" className="danger" {...act('doc.delete')} disabled={busy} onClick={() => run(() => api.deleteForTeam(doc.rel, true))}>
              Take over and delete
            </button>
          )}
          <button type="button" onClick={() => setPanel('')}>
            Cancel
          </button>
        </div>
      )}

      {t?.domain_clash && !dm?.viewing && (
        <div className="doc-head-line warn-text">
          Another blueprint, {t.domain_clash}, claims the same domain. References to it are ambiguous until a team owner deletes one.
        </div>
      )}
      {doc.transfer?.from && <div className="doc-head-line">{attestedText(doc.transfer)}</div>}
      {(t?.local_only ?? 0) > 0 && (
        <div className="doc-head-line">
          {plural(t!.local_only!, 'comment is', 'comments are')} only in your copy: not signed, or by someone outside the team.
        </div>
      )}
      {err && <div className="error-text">{err}</div>}
      {warn && <div className="warn-box">{warn}</div>}

      {transfer && <TransferDialog doc={doc} mode={transfer} onClose={() => setTransfer('')} onDone={reload} />}
      {releasing && (
        <ReleaseDialog
          doc={doc}
          onClose={() => setReleasing(false)}
          onDone={() => {
            setReleasing(false)
            reload()
          }}
        />
      )}
    </section>
  )
}

// ReleaseDialog releases the next domain version (spec 6.12): a summary
// and the open comments it takes up, which then fold as taken up.
function ReleaseDialog({ doc, onClose, onDone }: { doc: Doc; onClose: () => void; onDone: () => void }) {
  const open = doc.entries.filter((e) => e.valid && e.kind !== 'release' && !e.reply_to && !e.incorporated_in)
  const [picked, setPicked] = useState<Set<string>>(new Set())
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const next = (doc.domain?.version ?? 0) + 1
  const heading = (slug: string) => doc.headings.find((h) => h.slug === slug)?.text ?? slug
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault()
    setBusy(true)
    setErr('')
    try {
      await api.release(doc.rel, note, [...picked])
      onDone()
    } catch (e) {
      setErr((e as Error).message)
      setBusy(false)
    }
  }
  return (
    <Modal title={`Release version ${next}`} busy={busy} onClose={onClose}>
      <form onSubmit={submit} className="seed-form">
        <p className="muted">
          The current text becomes version {next} for every member of {doc.domain?.team}. Blueprints written against an older version
          keep pointing to it.
        </p>
        <label>
          Summary <span className="muted">(optional)</span>
          <textarea rows={2} value={note} onChange={(e) => setNote(e.target.value)} />
        </label>
        {open.length > 0 ? (
          <fieldset className="incorporate">
            <legend>Comments this version takes up</legend>
            {open.map((e) => (
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
        ) : (
          <p className="muted small">No open comments.</p>
        )}
        <ErrorConfirm doc={doc} verb="Release" />
        {err && <p className="error-text">{err}</p>}
        <div className="row">
          <button type="submit" className="primary" {...act('doc.release')} disabled={busy}>
            Release v{next}
            {confirmable(doc).length > 0 ? ' anyway' : ''}
          </button>
          <button type="button" onClick={onClose}>
            Cancel
          </button>
        </div>
      </form>
    </Modal>
  )
}
