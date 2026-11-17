// Needs you (spec 11.1, 11.3): across every team, oldest first, what only
// you can move forward (Decide, Yours now), what is new on blueprints
// since you last opened them, and what is in progress with nothing for
// you to do (joins, your offers to other teams). Only the first two are
// waiting items; the rest are not counted by the tray.
import { useEffect, useState } from 'react'
import { act } from './actions'
import { api, type Join, type Notice, type TransferView, type Unread, type WaitingItem } from './api'
import { MemberSelect, useMembers } from './Members'
import { shortName, waitingRows, type WaitingRow } from './scope'
import { AcceptButton, DeclineButton } from './Transfer'
import { docHref, localTime, plural, warningOf } from './util'
import { noticeHref } from './notify'

export function NeedsPage({ reloadKey, notices }: { reloadKey: number; notices: (Notice & { at: string })[] }) {
  const [items, setItems] = useState<WaitingItem[] | null>(null)
  const [unread, setUnread] = useState<Unread[]>([])
  const [joins, setJoins] = useState<Join[]>([])
  const [offers, setOffers] = useState<TransferView[]>([])
  const [err, setErr] = useState('')
  const [warn, setWarn] = useState('')
  const [tick, setTick] = useState(0)
  useEffect(() => {
    setErr('')
    api.waiting().then(setItems, (e: Error) => setErr(e.message))
    api.unread().then(setUnread, () => {})
    api.joins().then(setJoins, () => {})
    api.transfers().then((l) => setOffers(l.filter((t) => t.mine && t.status === 'pending')), () => {})
  }, [reloadKey, tick])
  const reload = () => setTick((n) => n + 1)
  const rows = waitingRows(items ?? [])
  const groups = new Map<string, WaitingRow[]>()
  for (const r of rows) {
    const k = (r.group === 'decide' ? 'Decide' : 'Yours now') + ' · ' + r.item.team_name
    groups.set(k, [...(groups.get(k) ?? []), r])
  }
  const order = [...groups.keys()].sort((a, b) => Number(b.startsWith('Decide')) - Number(a.startsWith('Decide')))
  const joining = joins.filter((j) => j.state === 'pending' || j.state === 'failed')
  const nothing = items !== null && rows.length === 0 && unread.length === 0 && joining.length === 0 && offers.length === 0
  return (
    <div className="page">
      <h1>Needs you</h1>
      <p className="muted">Across all your teams, oldest first: what only you can move forward, and what is new since you last looked.</p>
      {err && <div className="error-box">{err}</div>}
      {warn && <div className="warn-box">{warn}</div>}
      {nothing && <div className="empty">Nothing needs you right now.</div>}

      {order.map((k) => (
        <section key={k} className="needs-group">
          <h2 className="group-title">{k}</h2>
          {groups.get(k)!.map((r) => (
            <NeedsCard key={r.key} r={r} onDone={reload} onError={setErr} onWarn={setWarn} />
          ))}
        </section>
      ))}

      {unread.length > 0 && (
        <section className="needs-group">
          <h2 className="group-title">New since you last looked</h2>
          {unread.map((u) => (
            <div className="needs-card" key={u.path}>
              <div className="grow">
                <div>
                  <b>“{u.title || u.rel}”</b> · {newText(u)}
                </div>
                <div className="muted small">
                  {u.never ? `in ${u.team}, from ${u.owner ? shortName(u.owner) : 'a teammate'}` : u.mine ? `your blueprint in ${u.team}` : `${u.team} · since you last opened it`}
                  {u.authors.length > 0 && <> · {u.authors.map(shortName).join(', ')}</>}
                </div>
              </div>
              <a className="button" href={docHref(u.rel)}>
                Open
              </a>
            </div>
          ))}
        </section>
      )}

      {(joining.length > 0 || offers.length > 0) && (
        <section className="needs-group">
          <h2 className="group-title">In progress · nothing to do</h2>
          {joining.map((j) => (
            <div className="needs-card quiet" key={j.id}>
              <div className="grow">
                <div>{j.state === 'failed' ? `Could not join ${j.name}` : `Joining ${j.name}, waiting for ${shortName(j.inviter ?? j.owner ?? 'the inviter')} to come online`}</div>
                <div className="muted small">
                  {j.state === 'failed'
                    ? j.detail
                    : `${j.last_try ? `last tried ${localTime(j.last_try)} · ` : ''}the invite is valid until ${localTime(j.expires)}`}
                </div>
              </div>
              <a href="#/join">Details</a>
            </div>
          ))}
          {offers.map((t) => (
            <div className="needs-card quiet" key={t.id}>
              <div className="grow">
                <div>
                  Your {t.mode === 'move' ? 'move' : 'copy'} of “{t.title}” is offered to {t.team}
                </div>
                <div className="muted small">{localTime(t.at)} · waiting for a team owner of {t.team} to accept</div>
              </div>
              <button
                type="button"
                className="link"
                {...act('transfer.withdraw')}
                onClick={() => api.decideTransfer(t.id, 'withdraw').then(reload, (e: Error) => setErr(e.message))}
              >
                Withdraw
              </button>
            </div>
          ))}
        </section>
      )}

      {notices.length > 0 && (
        <section className="needs-group">
          <h2 className="group-title">Recent notices</h2>
          <ul className="plain small">
            {notices.map((n, i) => {
              const href = noticeHref(n)
              return (
                <li key={i}>
                  <span className="muted">{localTime(n.at)}</span> {href ? <a href={href}>{n.message}</a> : n.message}
                </li>
              )
            })}
          </ul>
        </section>
      )}
    </div>
  )
}

function newText(u: Unread): string {
  const parts = []
  if (u.comments > 0) parts.push(plural(u.comments, 'new comment'))
  if (u.deviations > 0) parts.push(plural(u.deviations, 'deviation'))
  if (parts.length === 0) return u.never ? 'new blueprint' : 'new'
  return parts.join(', ')
}

function NeedsCard({ r, onDone, onError, onWarn }: { r: WaitingRow; onDone: () => void; onError: (m: string) => void; onWarn: (m: string) => void }) {
  const it = r.item
  return (
    <div className="needs-card">
      <div className="grow">
        <div>{r.text}</div>
        <div className="muted small">
          {localTime(r.at)}
          {r.by && it.kind !== 'handed' && <> · {r.by}</>}
          {it.kind === 'owner-gone' && ' · nobody can release it until it has an owner'}
        </div>
      </div>
      <div className="row">
        {r.buttons.includes('accept') && it.transfer && <AcceptButton id={it.transfer} team={it.team_name} onDone={onDone} onError={onError} />}
        {r.buttons.includes('decline') && it.transfer && <DeclineButton id={it.transfer} who={it.by} onDone={onDone} onError={onError} />}
        {r.buttons.includes('choose') && it.path && (
          <ChooseOwner path={it.path} team={it.team_name} first={it.candidates} exclude={it.kind === 'owner-gone' && it.by ? [it.by] : []} onDone={onDone} onError={onError} onWarn={onWarn} />
        )}
        {r.buttons.includes('open') && r.href && (
          <a className={r.buttons.length === 1 ? 'button' : ''} href={r.href}>
            Open
          </a>
        )}
      </div>
    </div>
  )
}

/** ChooseOwner reassigns a document to a member the team owner picks
 * (spec 7.3): the way to settle a dispute or a missing owner. */
export function ChooseOwner({
  path,
  team,
  first,
  exclude,
  onDone,
  onError,
  onWarn,
}: {
  path: string
  team: string
  first?: string[]
  exclude?: string[]
  onDone: () => void
  onError: (m: string) => void
  onWarn?: (m: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [to, setTo] = useState('')
  const [busy, setBusy] = useState(false)
  const members = useMembers(open ? team : undefined)
  if (!open) {
    return (
      <button type="button" onClick={() => setOpen(true)}>
        Choose owner…
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
          const res = await api.reassign(path, to)
          onWarn?.(warningOf(res))
          setOpen(false)
          onDone()
        } catch (e) {
          onError((e as Error).message)
        }
        setBusy(false)
      }}
    >
      <MemberSelect members={members} value={to} onChange={setTo} first={first} exclude={exclude} label="New owner" />
      <button type="submit" className="primary" {...act('doc.reassign')} disabled={busy || !to}>
        Make owner
      </button>
      <button type="button" className="link" onClick={() => setOpen(false)}>
        Cancel
      </button>
    </form>
  )
}
