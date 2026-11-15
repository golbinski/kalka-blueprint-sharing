// The join screen (spec 11.1, 19.7): what an invite link or token is, and
// one Join button. Opening it never joins. The team name and the inviter
// come from the token and are claims until the join completes, so they
// are only ever rendered as text.
import { useEffect, useState } from 'react'
import { api, type JoinPreview } from './api'
import { joinHref, localTime } from './util'

export function JoinScreen({ token }: { token: string }) {
  const [p, setP] = useState<JoinPreview | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    setP(null)
    setErr('')
    if (!token) return
    let live = true
    api.joinPreview(token).then(
      (v) => live && setP(v),
      (e) => live && setErr((e as Error).message),
    )
    return () => {
      live = false
    }
  }, [token])

  const join = async () => {
    if (!p) return
    setBusy(true)
    setErr('')
    try {
      await api.join(p.token)
      location.hash = '#/team'
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy(false)
  }

  return (
    <div className="page narrow">
      <h1>Join a team</h1>
      {!token && <p className="muted">This link holds no invite. Paste the link or the token the team owner sent you.</p>}
      {p && (
        <section className="team-box join-screen">
          <dl className="join-facts">
            <dt>Team</dt>
            <dd>
              <b>{p.name}</b> <span className="muted mono small">{p.team.slice(0, 12)}</span>
            </dd>
            <dt>Invited by</dt>
            <dd>{p.inviter}</dd>
            <dt>For</dt>
            <dd>{p.email}</dd>
            <dt>Valid until</dt>
            <dd>{localTime(p.expires)}</dd>
          </dl>
          {p.joining && p.joining.state !== 'failed' ? (
            <div className={'banner ' + (p.joining.state === 'joined' ? 'notice' : 'warn')}>
              {p.joining.state === 'joined' ? `You joined ${p.name}.` : `Joining ${p.name}… kalka keeps looking for ${p.inviter}.`}{' '}
              <a href="#/team">Open Team</a>
            </div>
          ) : p.problem ? (
            <div className="banner danger" role="status">
              {p.message}
            </div>
          ) : (
            <div className="row">
              <button type="button" className="primary" onClick={join} disabled={busy}>
                {busy ? 'Joining…' : 'Join'}
              </button>
              <span className="muted small">kalka reaches {p.inviter}'s installation directly and keeps trying while it is offline.</span>
            </div>
          )}
        </section>
      )}
      {err && <div className="error-box">{err}</div>}
      {(!token || err) && <PasteBox />}
    </div>
  )
}

// PasteBox is "Join with link or token…": it opens the join screen for
// what was pasted.
export function PasteBox({ heading = false }: { heading?: boolean }) {
  const [text, setText] = useState('')
  return (
    <form
      className="team-box"
      onSubmit={(e) => {
        e.preventDefault()
        location.hash = joinHref(text)
        setText('')
      }}
    >
      {heading && <h2>Join with link or token</h2>}
      <p className="muted small">Paste the kalka:// link or the token a team owner sent you. You see the team before you join.</p>
      <textarea
        className="token-input mono"
        rows={3}
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder="kalka://join/… or the token"
        aria-label="Invite link or token"
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
