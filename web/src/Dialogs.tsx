import { useEffect, useRef, useState } from 'react'
import { api, ApiError, type Peer, type Prompt, type SyncEvent, type SyncResult } from './api'

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
