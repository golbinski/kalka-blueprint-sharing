// The waiting list (spec 11.1, 11.3): what only you can move forward in
// the selected team, oldest first, with its buttons.
import { useEffect, useState } from 'react'
import { act } from './actions'
import { api, type WaitingItem } from './api'
import { waitingRows } from './scope'
import { AcceptButton, DeclineButton } from './Transfer'
import { localTime } from './util'

export function WaitingPage({ team, teamName, reloadKey }: { team: string; teamName: string; reloadKey: number }) {
  const [items, setItems] = useState<WaitingItem[] | null>(null)
  const [err, setErr] = useState('')
  const [tick, setTick] = useState(0)
  useEffect(() => {
    setErr('')
    api.waiting(team).then(setItems, (e: Error) => setErr(e.message))
  }, [team, reloadKey, tick])
  const reload = () => setTick((n) => n + 1)
  const rows = waitingRows(items ?? [])
  return (
    <div className="page">
      <h1>Waiting in {teamName}</h1>
      <p className="muted">
        What only you can move forward: transfers your team decides, documents that became yours, and documents a team owner
        has to reassign.
      </p>
      {err && <div className="error-box">{err}</div>}
      {items && rows.length === 0 && <div className="empty">Nothing waits for you in {teamName}.</div>}
      {rows.length > 0 && (
        <ul className="waiting-list">
          {rows.map((r) => (
            <li key={r.key}>
              <div>
                <span className="badge">{r.kind}</span> {r.text}
              </div>
              <div className="muted small">
                {r.by && <>{r.by} · </>}
                {localTime(r.at)}
              </div>
              <div className="row">
                {r.buttons.includes('accept') && r.item.transfer && <AcceptButton id={r.item.transfer} onDone={reload} onError={setErr} />}
                {r.buttons.includes('decline') && r.item.transfer && <DeclineButton id={r.item.transfer} onDone={reload} onError={setErr} />}
                {r.buttons.includes('open') && r.href && <a href={r.href}>Open</a>}
                {r.buttons.includes('reassign') && r.item.path && <Reassign path={r.item.path} onDone={reload} onError={setErr} />}
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

function Reassign({ path, onDone, onError }: { path: string; onDone: () => void; onError: (m: string) => void }) {
  const [open, setOpen] = useState(false)
  const [email, setEmail] = useState('')
  const [busy, setBusy] = useState(false)
  if (!open) {
    return (
      <button type="button" className="link" onClick={() => setOpen(true)}>
        Reassign…
      </button>
    )
  }
  return (
    <form
      className="row"
      onSubmit={async (e) => {
        e.preventDefault()
        setBusy(true)
        try {
          await api.reassign(path, email.trim())
          setOpen(false)
          onDone()
        } catch (e) {
          onError((e as Error).message)
        }
        setBusy(false)
      }}
    >
      <input autoFocus value={email} onChange={(e) => setEmail(e.target.value)} placeholder="member's email" aria-label="New owner" />
      <button type="submit" {...act('doc.reassign')} disabled={busy || !email.trim()}>
        Reassign
      </button>
      <button type="button" className="link" onClick={() => setOpen(false)}>
        Cancel
      </button>
    </form>
  )
}
