import { useEffect, useRef, useState } from 'react'
import { api, ApiError, type Doc, type Entry, type Peer, type Prompt, type PublishResult, type SyncEvent, type SyncResult, type Team } from './api'
import { docHref } from './util'

const labels: Record<string, [string, string]> = {
  'verify-code': ['The codes match', 'They differ'],
  'accept-new': ['Accept', 'Decline'],
  'accept-handoff': ['Accept ownership', 'Decline'],
  converge: ['Replace my plan', 'Keep my copy'],
}

// PromptCard asks one question from the daemon.
export function PromptCard({ prompt, onDone }: { prompt: Prompt; onDone?: () => void }) {
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [yes, no] = labels[prompt.kind] ?? ['Yes', 'No']
  const answer = async (accept: boolean) => {
    setBusy(true)
    try {
      await api.answer(prompt.id, accept)
      onDone?.()
    } catch (e) {
      setErr(e instanceof ApiError && e.status === 404 ? 'This question was already answered or timed out.' : String((e as Error).message))
    }
    setBusy(false)
  }
  return (
    <div className={'prompt prompt-' + prompt.kind} role="alertdialog" aria-label={prompt.message}>
      {prompt.kind === 'verify-code' && (
        <>
          <div className="prompt-title">First sync with {prompt.peer}</div>
          <p>Compare this code with {prompt.peer}, on a call or in person. They see the same code if nobody is in between.</p>
          <div className="code">{prompt.code}</div>
        </>
      )}
      {prompt.kind !== 'verify-code' && <p className="prompt-message">{prompt.message}</p>}
      {err && <p className="error-text">{err}</p>}
      <div className="row">
        <button type="button" className="primary" disabled={busy} onClick={() => answer(true)}>
          {yes}
        </button>
        <button type="button" disabled={busy} onClick={() => answer(false)}>
          {no}
        </button>
      </div>
    </div>
  )
}

function explain(kind: string | undefined, msg: string): string {
  switch (kind) {
    case 'offline':
      return msg
    case 'key-changed':
      return msg
    case 'diverged':
      return msg
    case 'rejected-declined':
      return 'The recipient declined. ' + msg
    case 'rejected-not-owner':
      return 'The recipient has a different plan and only the owner can change it. ' + msg
    default:
      return msg
  }
}

interface Props {
  path: string
  title: string
  intent: 'sync' | 'handoff'
  peers: Peer[]
  self: string
  onClose: () => void
}

// SyncDialog runs one sync or handoff and shows its progress and prompts.
export function SyncDialog({ path, title, intent, peers, self, onClose }: Props) {
  const [to, setTo] = useState('')
  const [running, setRunning] = useState(false)
  const [steps, setSteps] = useState<string[]>([])
  const [prompt, setPrompt] = useState<Prompt | null>(null)
  const [result, setResult] = useState<SyncResult | null>(null)
  const [error, setError] = useState('')
  const input = useRef<HTMLInputElement>(null)
  useEffect(() => input.current?.focus(), [])

  const start = async (e: React.FormEvent) => {
    e.preventDefault()
    const email = to.trim()
    if (!email) return
    setRunning(true)
    setSteps([])
    setResult(null)
    setError('')
    try {
      await api.sync({ path, to: email, intent }, (ev: SyncEvent) => {
        switch (ev.type) {
          case 'progress':
            setSteps((s) => [...s, ev.message ?? ''])
            break
          case 'prompt':
            setPrompt(ev.prompt ?? null)
            break
          case 'prompt-closed':
            setPrompt((p) => (p && p.id === ev.prompt?.id ? null : p))
            break
          case 'result':
            setResult(ev.result ?? null)
            break
          case 'error':
            setError(explain(ev.kind, ev.error ?? 'the sync failed'))
            break
        }
      })
    } catch (err) {
      setError((err as Error).message)
    }
    setPrompt(null)
    setRunning(false)
  }

  const others = peers.filter((p) => p.email.toLowerCase() !== self.toLowerCase())
  const verb = intent === 'handoff' ? 'Hand off' : 'Sync'
  return (
    <div className="modal-back" onClick={(e) => e.target === e.currentTarget && !running && onClose()}>
      <div className="modal" role="dialog" aria-label={`${verb} ${title}`}>
        <h2>
          {intent === 'handoff' ? 'Hand off' : 'Sync'} <span className="muted">{title}</span>
        </h2>
        {intent === 'handoff' && (
          <p className="muted">
            The recipient becomes the owner and is asked to accept. Your file changes only after they accept; after that only
            they can revise the plan.
          </p>
        )}
        <form onSubmit={start} className="row">
          <input
            ref={input}
            type="email"
            list="peer-list"
            placeholder="teammate@corp.com"
            value={to}
            onChange={(e) => setTo(e.target.value)}
            disabled={running}
            aria-label="Peer email"
          />
          <datalist id="peer-list">
            {others.map((p) => (
              <option key={p.email} value={p.email}>
                {p.presence}
              </option>
            ))}
          </datalist>
          <button type="submit" className="primary" disabled={running || !to.trim()}>
            {running ? 'Working…' : verb}
          </button>
        </form>
        {others.length > 0 && !running && !result && (
          <div className="chips">
            {others.map((p) => (
              <button type="button" key={p.email} className="chip" onClick={() => setTo(p.email)}>
                <span className={'dot ' + p.presence} aria-label={p.presence} /> {p.email}
              </button>
            ))}
          </div>
        )}
        {steps.length > 0 && (
          <ul className="steps">
            {steps.map((s, i) => (
              <li key={i}>{s}</li>
            ))}
          </ul>
        )}
        {prompt && <PromptCard prompt={prompt} />}
        {result && (
          <div className="ok-box">
            {intent === 'handoff'
              ? `Handed off to ${result.peer}. They are the owner now.`
              : `Synced with ${result.peer}: ${result.outcome === 'unchanged' ? 'already up to date' : result.outcome}.`}
            {result.conflict && <div>Your previous copy was saved as {result.conflict}.</div>}
            {result.notes?.map((n, i) => (
              <div key={i} className="muted">
                {n}
              </div>
            ))}
          </div>
        )}
        {error && <div className="error-box">{error}</div>}
        <div className="row end">
          <button type="button" onClick={onClose} disabled={running}>
            Close
          </button>
        </div>
      </div>
    </div>
  )
}

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
                  <button type="button" className="primary" disabled={busy} onClick={retry}>
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
              <button type="submit" className="primary" disabled={busy || !title.trim()}>
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
export function PublishDialog({ doc, onClose, onDone }: { doc: Doc; onClose: () => void; onDone: () => void }) {
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
            <p>You are not a member of a team yet. Create one or join one on the <a href="#/team" onClick={onClose}>Team</a> page.</p>
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
              <button type="submit" className="primary" disabled={busy || !teams || !team}>
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
