// Transfers between teams (spec 20.7): Copy to team… and Move to team… on
// a document, the transfers of a team with Accept, Decline and Withdraw,
// and where an attested history came from.
import { useEffect, useState } from 'react'
import { act } from './actions'
import { Modal } from './Modal'
import { api, type Doc, type Team, type TransferView } from './api'
import { useMembers, MemberSelect } from './Members'
import { attestedText, transferEntries } from './scope'
import { docHref, localTime } from './util'

export function TransferDialog({ doc, mode, onClose, onDone }: { doc: Doc; mode: 'copy' | 'move'; onClose: () => void; onDone: () => void }) {
  const [teams, setTeams] = useState<Team[] | null>(null)
  const [team, setTeam] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [done, setDone] = useState('')
  const takeOver = !doc.team?.can_act && !!doc.team?.team_owner
  useEffect(() => {
    api.teams().then(
      (l) => {
        const to = l.filter((t) => t.me !== 'none' && t.name !== doc.team?.team)
        setTeams(to)
        if (to.length > 0) setTeam(to[0].team)
      },
      (e: Error) => setErr(e.message),
    )
  }, [doc.team?.team])
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setErr('')
    try {
      await api.startTransfer(doc.rel, team, mode, mode === 'move' && takeOver)
      setDone(teams?.find((t) => t.team === team)?.name ?? team)
      onDone()
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy(false)
  }
  const verb = mode === 'copy' ? 'Copy' : 'Move'
  return (
    <Modal title={`${verb} to team`} busy={busy} onClose={onClose}>
      {done ? (
        <>
          <p>
            The offer is with {done}. Its owners accept or decline it; you hear which.{' '}
            {mode === 'move' && 'Until then the document stays here, and comments added now will not travel.'}
          </p>
          <div className="row">
            <button type="button" className="primary" onClick={onClose}>
              Done
            </button>
          </div>
        </>
      ) : teams && teams.length === 0 ? (
        <>
          <p>You are a member of no other team. Create or join one from the switcher first.</p>
          <div className="row">
            <button type="button" onClick={onClose}>
              Close
            </button>
          </div>
        </>
      ) : (
        <form onSubmit={submit} className="seed-form">
          <p className="muted">
            {mode === 'copy'
              ? `The other team gets its own copy of “${doc.title}” at its last release, with the review entries attested by you. This one stays as it is.`
              : `“${doc.title}” leaves ${doc.team?.team} once the other team accepts: it is deleted here for every member, and lives on there with its review history attested by you.`}
            {takeOver && ' You are not its owner: as a team owner you take it over first, by a reassignment to you.'}
          </p>
          {teams && (
            <label>
              To
              <select value={team} onChange={(e) => setTeam(e.target.value)}>
                {teams.map((t) => (
                  <option key={t.team} value={t.team}>
                    {t.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          {err && <p className="error-text">{err}</p>}
          <div className="row">
            {mode === 'copy' ? (
              <button type="submit" className="primary" {...act('doc.copy')} disabled={busy || !team}>
                Offer a copy
              </button>
            ) : (
              <button type="submit" className="primary" {...act('doc.move')} disabled={busy || !team}>
                {takeOver ? 'Take over and offer the move' : 'Offer the move'}
              </button>
            )}
            <button type="button" onClick={onClose}>
              Cancel
            </button>
          </div>
        </form>
      )}
    </Modal>
  )
}

/** TransferStrip heads a document that came by a transfer, or is being
 * moved away (spec 11.1, 20.7). */
export function TransferStrip({ doc }: { doc: Doc }) {
  const t = doc.transfer
  if (!t) return null
  return (
    <div className="hints">
      {t.moving_to && (
        <div className="hint warn">
          <span className="hint-label">moving</span>
          <span className="hint-text">Being moved to {t.moving_to}; comments added now will not travel.</span>
        </div>
      )}
      {t.from && (
        <div className="hint">
          <span className="hint-label">attested</span>
          <span className="hint-text">{attestedText(t)}</span>
        </div>
      )}
    </div>
  )
}

/** AcceptButton accepts a transfer, as the user or naming another member
 * of the team as the owner of version 1. */
export function AcceptButton({ id, team, onDone, onError }: { id: string; team: string; onDone: () => void; onError: (m: string) => void }) {
  const [asked, setAsked] = useState(false)
  const [owner, setOwner] = useState('')
  const [busy, setBusy] = useState(false)
  const members = useMembers(asked ? team : undefined)
  const go = async () => {
    setBusy(true)
    try {
      await api.decideTransfer(id, 'accept', owner)
      onDone()
    } catch (e) {
      onError((e as Error).message)
    }
    setBusy(false)
  }
  return (
    <>
      {asked && <MemberSelect members={members} value={owner} onChange={setOwner} label="Owner of version 1" />}
      <button type="button" className="primary" {...act('transfer.accept')} disabled={busy} onClick={go}>
        {owner ? 'Accept for them' : 'Accept'}
      </button>
      {!asked && (
        <button type="button" className="link" onClick={() => setAsked(true)}>
          Someone else owns it…
        </button>
      )}
    </>
  )
}

/** DeclineButton declines a transfer after one confirming click: the
 * offering team hears of it, and it cannot be undone. */
export function DeclineButton({ id, who, onDone, onError }: { id: string; who?: string; onDone: () => void; onError: (m: string) => void }) {
  const [sure, setSure] = useState(false)
  if (!sure) {
    return (
      <button type="button" onClick={() => setSure(true)}>
        Decline
      </button>
    )
  }
  return (
    <>
      <span className="small">Decline {who ? `${who}'s` : 'the'} offer? They will be told.</span>
      <button type="button" className="danger" {...act('transfer.decline')} onClick={() => api.decideTransfer(id, 'decline').then(onDone, (e: Error) => onError(e.message))}>
        Decline
      </button>
      <button type="button" className="link" onClick={() => setSure(false)}>
        Keep it
      </button>
    </>
  )
}

/** Transfers is the Team view's list (spec 11.1): what was offered to the
 * team, with Accept and Decline for its owners and Withdraw for yours. */
export function Transfers({ team, reloadKey }: { team: string; reloadKey: number }) {
  const [rows, setRows] = useState<TransferView[] | null>(null)
  const [err, setErr] = useState('')
  const [tick, setTick] = useState(0)
  useEffect(() => {
    api.transfers(team).then(setRows, (e: Error) => setErr(e.message))
  }, [team, reloadKey, tick])
  const reload = () => setTick((n) => n + 1)
  if (!rows || (rows.length === 0 && !err)) return null
  return (
    <>
      <h3>Transfers</h3>
      {err && <div className="error-box">{err}</div>}
      <ul className="plain">
        {rows.map((t) => (
          <li key={t.id}>
            <b>{t.title || t.id}</b> <span className="muted small">{t.mode === 'move' ? 'moved' : 'copied'} from {t.from}</span> ·{' '}
            {t.mine ? 'by you' : `by ${t.actor}`}, {localTime(t.at)} · {transferEntries(t.entries)} ·{' '}
            {t.status === 'pending' ? (
              <>
                waiting{' '}
                {t.can_decide && (
                  <>
                    <AcceptButton id={t.id} team={t.team} onDone={reload} onError={setErr} /> <DeclineButton id={t.id} who={t.actor} onDone={reload} onError={setErr} />
                  </>
                )}
                {t.mine && (
                  <>
                    {t.can_decide && ' · '}
                    <button
                      type="button"
                      className="link"
                      {...act('transfer.withdraw')}
                      onClick={() => api.decideTransfer(t.id, 'withdraw').then(reload, (e: Error) => setErr(e.message))}
                    >
                      Withdraw
                    </button>
                  </>
                )}
              </>
            ) : t.status === 'accepted' ? (
              <>
                accepted{t.owner && <>, owner {t.owner}</>}
                {t.path && (
                  <>
                    {' '}
                    · <a href={docHref(t.path)}>open</a>
                  </>
                )}
              </>
            ) : (
              t.status
            )}
          </li>
        ))}
      </ul>
    </>
  )
}
