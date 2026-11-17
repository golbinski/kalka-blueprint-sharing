// The join screen (spec 11.1, 19.7): what an invite is, and one Join
// button; while the join waits, the steps with what kalka is waiting for;
// when it failed, the reason and the way forward. Opening it never joins,
// except right after "Save and join" in setup, which was that click. The
// team name and the inviter come from the token and are claims until the
// join completes, so they are only ever rendered as text.
import { useCallback, useEffect, useState } from 'react'
import { act } from './actions'
import { api, type Join, type JoinPreview, type Status } from './api'
import { joinState, shortName } from './scope'
import { joinHref, localTime, scopeHref } from './util'

/** The setup form sets this to the invite it saved for: the join screen
 * then joins without a second click. */
export const AUTOJOIN_KEY = 'kalka.autojoin'

function takeAutojoin(token: string): boolean {
  try {
    const t = sessionStorage.getItem(AUTOJOIN_KEY)
    if (t && t === token) {
      sessionStorage.removeItem(AUTOJOIN_KEY)
      return true
    }
  } catch {
    /* no storage */
  }
  return false
}

export function JoinScreen({ token, status, reloadKey, onResume }: { token: string; status: Status; reloadKey: number; onResume: () => void }) {
  const [p, setP] = useState<JoinPreview | null>(null)
  const [err, setErr] = useState('')
  const [badToken, setBadToken] = useState(false)
  const [busy, setBusy] = useState(false)
  const [joins, setJoins] = useState<Join[] | null>(null)

  const loadPreview = useCallback(async (tok: string) => {
    const v = await api.joinPreview(tok)
    setP(v)
    return v
  }, [])

  const join = useCallback(
    async (tok: string) => {
      setBusy(true)
      setErr('')
      try {
        await api.join(tok)
        await loadPreview(tok)
      } catch (e) {
        setErr((e as Error).message)
      }
      setBusy(false)
    },
    [loadPreview],
  )

  useEffect(() => {
    setP(null)
    setErr('')
    setBadToken(false)
    if (!token) return
    let live = true
    loadPreview(token).then(
      (v) => {
        if (live && joinState(v) === 'ready' && takeAutojoin(v.token)) join(v.token)
      },
      (e) => {
        if (!live) return
        setErr((e as Error).message)
        setBadToken(true)
      },
    )
    return () => {
      live = false
    }
  }, [token, loadPreview, join])

  // The join's progress and the list follow the daemon's events.
  useEffect(() => {
    api.joins().then(setJoins, () => {})
    if (token && p) loadPreview(p.token).catch(() => {})
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reloadKey])

  const forget = async (id: string) => {
    try {
      await api.forgetJoin(id)
      setJoins(await api.joins())
      if (token && p) await loadPreview(p.token)
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  const state = p ? joinState(p) : null
  const open = (joins ?? []).filter((j) => j.state === 'pending' || j.state === 'failed')

  return (
    <div className="page narrow">
      {!token && (
        <>
          {open.length === 0 && <h1>Join a team</h1>}
          {open.map((j) =>
            j.state === 'failed' ? (
              <JoinFailed key={j.id} j={j} onForget={() => forget(j.id)} />
            ) : (
              <JoinProgress key={j.id} j={j} status={status} onForget={() => forget(j.id)} />
            ),
          )}
          {!open.some((j) => j.state === 'failed') && <PasteBox heading={open.length > 0} />}
        </>
      )}
      {token && p && state === 'joining' && p.joining && <JoinProgress j={p.joining} status={status} onForget={() => forget(p.joining!.id)} />}
      {token && p && state === 'failed' && p.joining && <JoinFailed j={p.joining} onForget={() => forget(p.joining!.id)} />}
      {token && p && state !== 'joining' && state !== 'failed' && (
        <>
          <h1>Join {p.name}</h1>
          <section className="team-box join-screen">
            <dl className="join-facts">
              <dt>Team</dt>
              <dd>
                <b>{p.name}</b>
              </dd>
              <dt>Invited by</dt>
              <dd>{p.inviter}</dd>
              <dt>For</dt>
              <dd>{p.email}</dd>
              <dt>Valid until</dt>
              <dd>{localTime(p.expires)}</dd>
            </dl>
            {state === 'joined' ? (
              <div className="banner notice">
                You joined {p.name}. Its blueprints arrive from whoever is online. <a href={scopeHref(p.name)}>Open {p.name}</a>
              </div>
            ) : state === 'problem' ? (
              <div className="banner danger" role="status">
                {p.message}
              </div>
            ) : (
              <div className="row">
                <button type="button" className="primary" {...act('team.join')} onClick={() => join(p.token)} disabled={busy || status.paused}>
                  {busy ? 'Joining…' : `Join ${p.name}`}
                </button>
                <span className="muted small">
                  kalka reaches {shortName(p.inviter)}'s kalka directly, and keeps trying while it is not running.
                </span>
              </div>
            )}
            {status.paused && state === 'ready' && (
              <div className="banner warn">
                Sync is paused, so kalka cannot reach {shortName(p.inviter)}.{' '}
                <button type="button" className="link" {...act('sync.pause')} onClick={onResume}>
                  Resume sync
                </button>
              </div>
            )}
          </section>
        </>
      )}
      {err && <div className="error-box">{err}</div>}
      {token && badToken && <PasteBox />}
    </div>
  )
}

function CopyMessage({ to, text }: { to: string; text: string }) {
  const [done, setDone] = useState(false)
  return (
    <button
      type="button"
      className="link"
      onClick={() =>
        navigator.clipboard?.writeText(text).then(
          () => setDone(true),
          () => setDone(false),
        )
      }
    >
      {done ? 'Copied' : `Copy a message for ${shortName(to)}`}
    </button>
  )
}

// JoinProgress is a pending join as steps: set up, invite accepted here,
// waiting for the inviter's kalka, then the team's blueprints.
function JoinProgress({ j, status, onForget }: { j: Join; status: Status; onForget: () => void }) {
  const [canceling, setCanceling] = useState(false)
  const who = j.inviter ?? j.owner ?? 'the inviter'
  const short = shortName(who)
  return (
    <>
      <h1>Joining {j.name}</h1>
      <section className="team-box join-steps">
        <ol className="steps">
          <li className="done">
            <b>kalka is set up</b>
            <span className="muted small">
              {status.email} · {status.folder}
            </span>
          </li>
          <li className="done">
            <b>Invite accepted on this computer</b>
            <span className="muted small">
              from {who}, valid until {localTime(j.expires)}
            </span>
          </li>
          <li className="now" aria-current="step">
            <b>Waiting for {who} to be online</b>
            <span className="muted small">
              kalka has no server of its own: {short}'s kalka has to be running once to let you in.{' '}
              {j.tries > 0 ? `Tried ${j.tries} ${j.tries === 1 ? 'time' : 'times'}${j.last_try ? `, last at ${localTime(j.last_try)}` : ''}; ` : ''}
              it keeps trying every minute, also while this window is closed.
            </span>
            {j.detail && !/not online/.test(j.detail) && <span className="muted small">Last attempt: {j.detail}</span>}
          </li>
          <li>
            <b>Get the team's blueprints</b>
            <span className="muted small">from whoever is online, a few seconds after you are in</span>
          </li>
        </ol>
        <div className="banner notice">
          Nothing more to do here. You can tell {short} you are waiting:{' '}
          <CopyMessage to={who} text={`I accepted your kalka invite to ${j.name}. It completes once your kalka is running; could you start it?`} />
        </div>
        <div className="row">
          <a className="button" href="#/">
            Close
          </a>
          {canceling ? (
            <>
              <span className="small">Stop joining {j.name}? The invite stays valid until it expires.</span>
              <button type="button" className="danger" {...act('team.join')} onClick={onForget}>
                Cancel the join
              </button>
              <button type="button" onClick={() => setCanceling(false)}>
                Keep waiting
              </button>
            </>
          ) : (
            <button type="button" className="link" onClick={() => setCanceling(true)}>
              Cancel this join
            </button>
          )}
        </div>
      </section>
    </>
  )
}

// JoinFailed says why a join ended, and how to go on: a new invite.
function JoinFailed({ j, onForget }: { j: Join; onForget: () => void }) {
  const who = j.inviter ?? j.owner ?? 'the team owner'
  const [first, ...rest] = (j.detail ?? 'the join did not complete').split(': ')
  return (
    <>
      <h1>Could not join {j.name}</h1>
      <section className="team-box">
        <div className="error-box" role="alert">
          <b>{cap(first)}.</b> {rest.length > 0 && cap(rest.join(': ')) + '.'}
        </div>
        <p>Ask {who} for a new invite link, then paste it here.</p>
        <PasteBox bare />
        <div className="row">
          <CopyMessage to={who} text={`My kalka invite to ${j.name} did not work (${j.detail ?? 'it failed'}). Could you send me a new invite link?`} />
          <button type="button" className="link" onClick={onForget}>
            Dismiss
          </button>
        </div>
        <details className="small">
          <summary>Details</summary>
          <p className="muted">
            Invite made {localTime(j.created)}, valid until {localTime(j.expires)}; {j.tries} {j.tries === 1 ? 'attempt' : 'attempts'}
            {j.last_try ? `, the last at ${localTime(j.last_try)}` : ''}.{j.kind ? ` Reason code: ${j.kind}.` : ''}
          </p>
        </details>
      </section>
    </>
  )
}

const cap = (s: string) => (s ? s[0].toUpperCase() + s.slice(1) : s)

// PasteBox is "Join with an invite…": it opens the join screen for what
// was pasted.
export function PasteBox({ heading = false, bare = false }: { heading?: boolean; bare?: boolean }) {
  const [text, setText] = useState('')
  return (
    <form
      className={bare ? 'paste-box' : 'team-box paste-box'}
      onSubmit={(e) => {
        e.preventDefault()
        location.hash = joinHref(text)
        setText('')
      }}
    >
      {heading && <h2>Join with an invite</h2>}
      {!bare && <p className="muted small">Paste the kalka:// link or the invite code a team owner sent you. You see the team before you join.</p>}
      <textarea
        className="token-input mono"
        rows={2}
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder="kalka://join/… or the invite code"
        aria-label="Invite link or code"
        spellCheck={false}
      />
      <div>
        <button type="submit" className="primary" disabled={!text.trim()}>
          Continue
        </button>
      </div>
    </form>
  )
}
