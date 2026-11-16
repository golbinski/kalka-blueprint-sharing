import { useEffect, useRef, useState } from 'react'
import { api, type Doc, type Entry, type PublishResult, type Team } from './api'
import { act } from './actions'
import { docHref } from './util'

// SeedDialog splits a seed off a blueprint (spec 6.11), optionally from an
// entry such as a deviation, whose text becomes the problem by default.
export function SeedDialog({ doc, from, onClose, onDone }: { doc: Doc; from?: Entry; onClose: () => void; onDone: () => void }) {
  const [title, setTitle] = useState('')
  const [problem, setProblem] = useState('')
  const [why, setWhy] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [made, setMade] = useState<{ rel: string; backlinkError?: string } | null>(null)
  const first = useRef<HTMLInputElement>(null)
  useEffect(() => first.current?.focus(), [])
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault()
    setBusy(true)
    setErr('')
    try {
      const r = await api.bud({ path: doc.rel, from: from?.id, title, problem: problem || undefined, why: why || undefined })
      setMade({ rel: r.seed, backlinkError: r.backlink_error })
      onDone()
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy(false)
  }
  const retry = async () => {
    if (!made) return
    setBusy(true)
    try {
      await api.backlink(made.rel)
      setMade({ rel: made.rel })
      onDone()
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy(false)
  }
  return (
    <div className="modal-back" onClick={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div className="modal" role="dialog" aria-label="Create seed">
        <h2>Create seed</h2>
        {made ? (
          <>
            <p>
              The seed is written:{' '}
              <a href={docHref(made.rel)} onClick={onClose}>
                {made.rel}
              </a>
              .
            </p>
            {made.backlinkError ? (
              <>
                <p className="error-text">The link back from “{doc.title}” could not be added: {made.backlinkError}</p>
                <div className="row">
                  <button type="button" className="primary" {...act('seed.backlink')} disabled={busy} onClick={retry}>
                    Add the backlink
                  </button>
                  <button type="button" onClick={onClose}>
                    Later
                  </button>
                </div>
              </>
            ) : (
              <div className="row">
                <button type="button" className="primary" onClick={onClose}>
                  Done
                </button>
              </div>
            )}
          </>
        ) : (
          <form onSubmit={submit} className="seed-form">
            <p className="muted">
              A small blueprint you own, with status <code>seed</code>, linked from “{doc.title}”. The parent's plan is not changed; a{' '}
              <code>budded</code> entry is added to its review log.
            </p>
            <label>
              Title
              <input ref={first} value={title} onChange={(e) => setTitle(e.target.value)} required maxLength={120} />
            </label>
            <label>
              Problem{from ? ' (empty: quote the ' + from.kind + ')' : ''}
              <textarea rows={4} value={problem} onChange={(e) => setProblem(e.target.value)} required={!from} />
            </label>
            <label>
              Why it was split off <span className="muted">(optional)</span>
              <textarea rows={2} value={why} onChange={(e) => setWhy(e.target.value)} />
            </label>
            {err && <p className="error-text">{err}</p>}
            <div className="row">
              <button type="submit" className="primary" {...act('seed.create')} disabled={busy || !title.trim()}>
                Create seed
              </button>
              <button type="button" onClick={onClose}>
                Cancel
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  )
}

// PublishDialog publishes a document to a team (spec 20.1): the team, when
// the user is in several, and a note for the release.
export function PublishDialog({ doc, onClose, onDone }: { doc: Pick<Doc, 'rel' | 'title'>; onClose: () => void; onDone: () => void }) {
  const [teams, setTeams] = useState<Team[] | null>(null)
  const [team, setTeam] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [done, setDone] = useState<PublishResult | null>(null)
  useEffect(() => {
    api
      .teams()
      .then((l) => {
        const mine = l.filter((t) => t.me !== 'none')
        setTeams(mine)
        if (mine.length > 0) setTeam(mine[0].team)
      })
      .catch((e) => setErr((e as Error).message))
  }, [])
  const submit = async (ev: React.FormEvent) => {
    ev.preventDefault()
    setBusy(true)
    setErr('')
    try {
      setDone(await api.publish(doc.rel, note, team))
      onDone()
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy(false)
  }
  return (
    <div className="modal-back" onClick={(e) => e.target === e.currentTarget && !busy && onClose()}>
      <div className="modal" role="dialog" aria-label="Publish to team">
        <h2>Publish to team</h2>
        {done ? (
          <>
            <p>
              “{doc.title}” is published to {done.team} as version {done.version}. It replicates to the team's members.
            </p>
            {done.local_only > 0 && (
              <p className="muted">
                {done.local_only} {done.local_only === 1 ? 'entry stays' : 'entries stay'} in your file only: unsigned, or by
                someone outside the team.
              </p>
            )}
            <div className="row">
              <button type="button" className="primary" onClick={onClose}>
                Done
              </button>
            </div>
          </>
        ) : teams && teams.length === 0 ? (
          <>
            <p>You are not a member of a team yet. Create one or join one from the switcher at the top left.</p>
            <div className="row">
              <button type="button" onClick={onClose}>
                Close
              </button>
            </div>
          </>
        ) : (
          <form onSubmit={submit} className="seed-form">
            <p className="muted">
              Every member of the team gets the document and the review entries signed by members. You stay its owner and
              release each new version; a document belongs to one team.
            </p>
            {teams && teams.length > 1 && (
              <label>
                Team
                <select value={team} onChange={(e) => setTeam(e.target.value)}>
                  {teams.map((t) => (
                    <option key={t.team} value={t.team}>
                      {t.name}
                    </option>
                  ))}
                </select>
              </label>
            )}
            {teams && teams.length === 1 && <p>Team: {teams[0].name}</p>}
            <label>
              Note <span className="muted">(optional)</span>
              <input value={note} onChange={(e) => setNote(e.target.value)} maxLength={200} />
            </label>
            {err && <p className="error-text">{err}</p>}
            <div className="row">
              <button type="submit" className="primary" {...act('doc.publish')} disabled={busy || !teams || !team}>
                Publish
              </button>
              <button type="button" onClick={onClose}>
                Cancel
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  )
}
