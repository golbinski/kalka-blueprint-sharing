import { useState } from 'react'
import { api, type BlueprintInfo, type Peer, type Status } from './api'
import { docHref, plural } from './util'

// Setup asks for the three required settings (spec 5).
export function Setup({ status }: { status: Status }) {
  const [email, setEmail] = useState('')
  const [mode, setMode] = useState<'cloud' | 'server'>('cloud')
  const [server, setServer] = useState('')
  const [folder, setFolder] = useState(status.defaults?.folder ?? '')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setErr('')
    try {
      await api.setup({ email: email.trim(), rendezvous: mode === 'cloud' ? 'peerjs-cloud' : server.trim(), folder: folder.trim() })
      // The daemon starts on the same port; wait for it.
      for (let i = 0; i < 60; i++) {
        await new Promise((r) => setTimeout(r, 500))
        try {
          const s = await api.status()
          if (!s.setup) {
            location.reload()
            return
          }
        } catch {
          /* restarting */
        }
      }
      setErr('kalka did not come back after saving. Check the terminal it runs in.')
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy(false)
  }

  return (
    <div className="page narrow">
      <h1>Set up kalka</h1>
      <p className="muted">Three settings. They are saved in kalka's config file and can be changed there later.</p>
      <form className="form" onSubmit={save}>
        <label>
          Your email
          <input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@corp.com" autoFocus />
          <span className="muted small">Teammates reach you by this address. It is how they find you, not an account.</span>
        </label>
        <fieldset>
          <legend>Rendezvous server</legend>
          <label className="inline">
            <input type="radio" checked={mode === 'server'} onChange={() => setMode('server')} /> Your team's server
          </label>
          {mode === 'server' && (
            <input type="url" required value={server} onChange={(e) => setServer(e.target.value)} placeholder="wss://kalka.corp.example/" />
          )}
          <label className="inline">
            <input type="radio" checked={mode === 'cloud'} onChange={() => setMode('cloud')} /> Public PeerJS cloud (demo)
          </label>
          {mode === 'cloud' && (
            <span className="muted small">Community-run, no availability guarantee, not for sensitive blueprints.</span>
          )}
        </fieldset>
        <label>
          Blueprint folder
          <input type="text" required value={folder} onChange={(e) => setFolder(e.target.value)} />
          <span className="muted small">Created if it does not exist. Received blueprints are saved here.</span>
        </label>
        {err && <div className="error-box">{err}</div>}
        <button type="submit" className="primary" disabled={busy}>
          {busy ? 'Starting…' : 'Save and start'}
        </button>
      </form>
    </div>
  )
}

const roleLabel = (b: BlueprintInfo, self: string) =>
  !b.kalka_id ? 'local' : b.owner && b.owner.toLowerCase() === self.toLowerCase() ? 'owner' : 'reviewer'

export function Blueprints({ list, status }: { list: BlueprintInfo[]; status: Status }) {
  if (list.length === 0) {
    return (
      <div className="page">
        <h1>Blueprints</h1>
        <div className="empty">
          <p>No blueprints yet.</p>
          <p className="muted">
            Put a Markdown file into <code>{status.folder}</code>, or wait for a teammate to send you one.
          </p>
        </div>
      </div>
    )
  }
  return (
    <div className="page">
      <h1>Blueprints</h1>
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr>
              <th>Title</th>
              <th>Role</th>
              <th>Owner</th>
              <th>Entries</th>
              <th>Problems</th>
              <th>File</th>
            </tr>
          </thead>
          <tbody>
            {list.map((b) => {
              const role = roleLabel(b, status.email)
              return (
                <tr key={b.path}>
                  <td>
                    <a href={docHref(b.rel)}>{b.title || b.rel}</a>
                  </td>
                  <td>
                    <span className={'role role-' + role}>{role}</span>
                  </td>
                  <td>{b.owner ?? <span className="muted">—</span>}</td>
                  <td>{b.entries}</td>
                  <td>
                    {b.parse_error && <span className="badge warn">{b.parse_error}</span>}
                    {b.errors > 0 && <span className="badge warn">{plural(b.errors, 'error')}</span>}
                    {b.duplicate && (
                      <span className="badge warn" title="Two files have the same kalka_id; syncing is refused until one is removed">
                        duplicate id
                      </span>
                    )}
                    {b.owner_edited && <span className="badge warn">owner edited by hand</span>}
                  </td>
                  <td className="mono muted">{b.rel}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function PeerRow({ p, reload }: { p: Peer; reload: () => void }) {
  const [confirm, setConfirm] = useState<'' | 'verify' | 'forget'>('')
  const [err, setErr] = useState('')
  const run = async (f: () => Promise<unknown>) => {
    setErr('')
    try {
      await f()
      setConfirm('')
      reload()
    } catch (e) {
      setErr((e as Error).message)
    }
  }
  let key = <span className="muted">not pinned yet</span>
  if (p.key_changed) key = <span className="badge danger">key changed: syncs blocked</span>
  else if (p.verified) key = <span className="badge ok">verified</span>
  else if (p.pinned) key = <span className="badge">pinned, not verified</span>
  return (
    <tr>
      <td>{p.email}</td>
      <td>
        <span className={'dot ' + p.presence} /> {p.presence}
      </td>
      <td>{key}</td>
      <td className="mono">{p.code ?? ''}</td>
      <td className="actions">
        {confirm === '' && (
          <>
            {p.pinned && !p.verified && !p.key_changed && (
              <button type="button" onClick={() => setConfirm('verify')}>
                Mark verified…
              </button>
            )}
            {p.pinned && (
              <button type="button" onClick={() => setConfirm('forget')}>
                Remove pin…
              </button>
            )}
          </>
        )}
        {confirm === 'verify' && (
          <span className="confirm">
            Only if {p.email} sees <b className="mono">{p.code}</b> too.{' '}
            <button type="button" className="primary" onClick={() => run(() => api.confirm(p.email))}>
              Codes match
            </button>
            <button type="button" onClick={() => setConfirm('')}>
              Cancel
            </button>
          </span>
        )}
        {confirm === 'forget' && (
          <span className="confirm">
            The next contact pins whatever key {p.email} presents and asks for the code again.{' '}
            <button type="button" className="danger" onClick={() => run(() => api.forget(p.email))}>
              Remove pin
            </button>
            <button type="button" onClick={() => setConfirm('')}>
              Cancel
            </button>
          </span>
        )}
        {err && <div className="error-text">{err}</div>}
      </td>
    </tr>
  )
}

export function Peers({ peers, reload }: { peers: Peer[]; reload: () => void }) {
  const [email, setEmail] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const add = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setErr('')
    try {
      await api.addPeer(email.trim())
      setEmail('')
      reload()
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy(false)
  }
  return (
    <div className="page">
      <h1>Peers</h1>
      <p className="muted">
        A peer's key is pinned at first contact. Compare the verification code with them once, on a call or in person.
      </p>
      <form className="row" onSubmit={add}>
        <input type="email" placeholder="teammate@corp.com" value={email} onChange={(e) => setEmail(e.target.value)} aria-label="Peer email" />
        <button type="submit" disabled={busy || !email.trim()}>
          {busy ? 'Checking…' : 'Add and check presence'}
        </button>
      </form>
      {err && <div className="error-box">{err}</div>}
      {peers.length === 0 ? (
        <p className="muted">No peers yet. They appear here after the first sync, or when you add an address.</p>
      ) : (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>Email</th>
                <th>Presence</th>
                <th>Key</th>
                <th>Code</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {peers.map((p) => (
                <PeerRow key={p.email} p={p} reload={reload} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
