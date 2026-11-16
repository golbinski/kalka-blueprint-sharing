import { useCallback, useEffect, useState } from 'react'
import { act } from './actions'
import { api, type BlueprintInfo, type JoinPreview, type SeedRow, type Status, type Team } from './api'
import { PublishDialog } from './Dialogs'
import { PERSONAL } from './scope'
import { docHref, plural, setupFromInvite, useRoute } from './util'

// Setup asks for the three required settings (spec 5).
export function Setup({ status }: { status: Status }) {
  const [email, setEmail] = useState('')
  const [mode, setMode] = useState<'cloud' | 'server'>('cloud')
  const [server, setServer] = useState('')
  const [folder, setFolder] = useState(status.defaults?.folder ?? '')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [invite, setInvite] = useState<JoinPreview | null>(null)
  const route = useRoute()
  const token = route.view === 'join' ? route.token : ''

  // Opened from a join link: the email and the rendezvous come from the
  // token, so a new teammate types only the folder; after saving, the
  // page reloads on the join screen (spec 11.1).
  useEffect(() => {
    if (!token) return
    let live = true
    api.joinPreview(token).then(
      (p) => {
        if (!live) return
        const f = setupFromInvite(p)
        setInvite(p)
        setEmail(f.email)
        setMode(f.mode)
        setServer(f.server)
      },
      () => {},
    )
    return () => {
      live = false
    }
  }, [token])

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
      {invite && (
        <div className="banner notice">
          You are joining <b>{invite.name}</b>, invited by {invite.inviter}. Your email and the rendezvous server come from the invite;
          choose the folder, then join.
        </div>
      )}
      <form className="form" onSubmit={save}>
        <label>
          Your email
          <input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@library.example" autoFocus={!invite} />
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
          <input type="text" required value={folder} onChange={(e) => setFolder(e.target.value)} autoFocus={!!invite} />
          <span className="muted small">Created if it does not exist. Received blueprints are saved here.</span>
        </label>
        {err && <div className="error-box">{err}</div>}
        <button type="submit" className="primary" {...act('setup.save')} disabled={busy}>
          {busy ? 'Starting…' : invite ? 'Save and continue' : 'Save and start'}
        </button>
      </form>
    </div>
  )
}

const roleLabel = (b: BlueprintInfo, self: string) =>
  !b.kalka_id ? 'local' : b.owner && b.owner.toLowerCase() === self.toLowerCase() ? 'owner' : 'reviewer'

/** Blueprints lists the files of the scope (spec 11.1). In Personal each
 * offers Publish to team…; in a team folder a file that does not
 * replicate says why. */
export function Blueprints({ list, status, scope, team, reload }: { list: BlueprintInfo[]; status: Status; scope: string; team?: Team; reload: () => void }) {
  const [publishing, setPublishing] = useState<BlueprintInfo | null>(null)
  const personal = scope === PERSONAL
  const notShared = new Map((team?.not_shared ?? []).map((n) => [n.rel, n.reason]))
  const where = personal ? 'Personal' : team?.name ?? scope
  if (list.length === 0) {
    return (
      <div className="page">
        <h1>Blueprints · {where}</h1>
        <div className="empty">
          <p>No blueprints here yet.</p>
          <p className="muted">
            {personal ? (
              <>
                Put a Markdown file into <code>{status.folder}/personal</code>: what you write there stays yours until you publish it.
              </>
            ) : (
              <>A team's documents arrive as its members publish them. Publish one of yours from Personal.</>
            )}
          </p>
        </div>
      </div>
    )
  }
  return (
    <div className="page">
      <h1>Blueprints · {where}</h1>
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
              {personal && <th />}
            </tr>
          </thead>
          <tbody>
            {list.map((b) => {
              const role = roleLabel(b, status.email)
              const why = notShared.get(b.rel)
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
                      <span className="badge warn" title="Two files have the same kalka_id; publishing is refused until one is removed">
                        duplicate id
                      </span>
                    )}
                    {why && (
                      <span className="badge warn" title={why}>
                        not shared: publish it or move it to Personal
                      </span>
                    )}
                  </td>
                  <td className="mono muted">{b.rel}</td>
                  {personal && (
                    <td>
                      {b.errors === 0 && !b.parse_error && !b.duplicate && (
                        <button type="button" className="link" {...act('doc.publish')} onClick={() => setPublishing(b)}>
                          Publish to team…
                        </button>
                      )}
                    </td>
                  )}
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      {publishing && (
        <PublishDialog doc={{ rel: publishing.rel, title: publishing.title || publishing.rel }} onClose={() => setPublishing(null)} onDone={reload} />
      )}
    </div>
  )
}

function age(active?: string): string {
  if (!active) return 'age unknown'
  const ms = Date.now() - new Date(active).getTime()
  if (isNaN(ms)) return 'age unknown'
  const h = ms / 3_600_000
  if (h < 1) return 'active now'
  if (h < 48) return `${Math.floor(h)} h`
  return `${Math.floor(h / 24)} days`
}

// PreBacklog lists every seed in the folder (spec 6.11): title, owner,
// parent and age, newest activity first.
export function PreBacklog({ self, scope }: { self: string; scope: string }) {
  const [rows, setRows] = useState<SeedRow[] | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState('')
  const load = useCallback(() => api.seeds(scope).then(setRows, (e: Error) => setErr(e.message)), [scope])
  useEffect(() => {
    load()
  }, [load])
  const promote = async (rel: string) => {
    setBusy(rel)
    try {
      await api.promote(rel)
      await load()
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy('')
  }
  return (
    <div className="page">
      <h1>Pre-backlog</h1>
      <p className="muted">
        Seeds: ideas split off from growing blueprints, kept small until someone takes them up. Create one from a blueprint
        (“Create seed”) or from a deviation entry.
      </p>
      {err && <div className="error-box">{err}</div>}
      {rows && rows.length === 0 && <div className="empty">No seeds yet.</div>}
      {rows && rows.length > 0 && (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>Seed</th>
                <th>Owner</th>
                <th>Split off from</th>
                <th>Age</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.kalka_id + r.rel}>
                  <td>
                    <a href={docHref(r.rel)}>{r.title}</a>
                  </td>
                  <td>{r.mine ? 'you' : r.owner}</td>
                  <td>
                    {r.parent_rel ? <a href={docHref(r.parent_rel)}>{r.parent_title || r.parent_rel}</a> : r.parent ? <span className="muted">not in your folder</span> : '—'}
                  </td>
                  <td title={r.active}>{age(r.active)}</td>
                  <td>
                    {r.mine && bpEq(r.owner, self) && (
                      <button type="button" className="link" {...act('seed.promote')} disabled={busy === r.rel} onClick={() => promote(r.rel)}>
                        Promote to draft
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}

const bpEq = (a: string, b: string) => a.toLowerCase() === b.toLowerCase()
