import { useCallback, useEffect, useState } from 'react'
import { act } from './actions'
import { api, type BlueprintInfo, type JoinPreview, type SeedRow, type Status, type Team } from './api'
import { PublishDialog } from './Dialogs'
import { PERSONAL, shortName } from './scope'
import { AUTOJOIN_KEY } from './Join'
import { inDesktopWindow } from './shell'
import { ago, docHref, plural, scopeHref, setupFromInvite, useRoute } from './util'

// Setup asks for the three required settings (spec 5). Opened from an
// invite, the email and the server come from it and only the folder is
// asked; "Save and join" then joins without a second click (spec 11.1).
export function Setup({ status }: { status: Status }) {
  const [email, setEmail] = useState('')
  const [mode, setMode] = useState<'cloud' | 'server'>('cloud')
  const [server, setServer] = useState('')
  const [folder, setFolder] = useState(status.defaults?.folder ?? '')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [invite, setInvite] = useState<JoinPreview | null>(null)
  const [changing, setChanging] = useState(false)
  const route = useRoute()
  const token = route.view === 'join' ? route.token : ''

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

  const choose = async () => {
    setErr('')
    try {
      const dir = await api.pickFolder(folder)
      if (dir) setFolder(dir)
    } catch (e) {
      setErr((e as Error).message)
    }
  }

  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setErr('')
    try {
      await api.setup({ email: email.trim(), rendezvous: mode === 'cloud' ? 'peerjs-cloud' : server.trim(), folder: folder.trim() })
      if (invite) {
        try {
          sessionStorage.setItem(AUTOJOIN_KEY, invite.token)
        } catch {
          /* then the join screen asks once more */
        }
      }
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
      setErr(
        inDesktopWindow()
          ? 'kalka did not start with these settings. Open it again from its icon; what went wrong is in kalka.log in its configuration folder.'
          : 'kalka did not come back after saving. Check the terminal it runs in, or kalka.log in its configuration folder.',
      )
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy(false)
  }

  const serverName = mode === 'cloud' ? 'the public demo server' : server
  const fixed = invite && !changing
  return (
    <div className="page narrow">
      <h1>{invite ? `Join ${invite.name}` : 'Set up kalka'}</h1>
      {invite ? (
        <p>
          {invite.inviter} invited you to the team <b>{invite.name}</b>. Choose where kalka keeps your blueprints, and you are in as
          soon as {invite.inviter.split('@')[0]}'s kalka is running.
        </p>
      ) : (
        <p className="muted">Three settings. You can change them later with <code>kalka setup</code>.</p>
      )}
      <form className="form" onSubmit={save}>
        {fixed ? (
          <div className="facts">
            You join as <b>{email}</b>, through {serverName}.{' '}
            <button type="button" className="link" onClick={() => setChanging(true)}>
              Change
            </button>
          </div>
        ) : (
          <>
            <label>
              Your email
              <input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} placeholder="you@library.example" autoFocus={!invite} />
              <span className="muted small">
                {invite
                  ? `The invite is for ${invite.email}; another address cannot use it.`
                  : 'Teammates reach you by this address. It is how they find you, not an account.'}
              </span>
            </label>
            <fieldset>
              <legend>Your team's kalka server (rendezvous)</legend>
              <label className="inline">
                <input type="radio" checked={mode === 'server'} onChange={() => setMode('server')} /> Your team's server
              </label>
              {mode === 'server' && (
                <input type="url" required value={server} onChange={(e) => setServer(e.target.value)} placeholder="wss://kalka.example.org/" />
              )}
              <label className="inline">
                <input type="radio" checked={mode === 'cloud'} onChange={() => setMode('cloud')} /> Public demo server
              </label>
              {mode === 'cloud' && <span className="muted small">For trying kalka: community-run, no availability guarantee, not for confidential plans.</span>}
            </fieldset>
          </>
        )}
        <label>
          Blueprint folder
          <span className="row">
            <input type="text" className="grow" required value={folder} onChange={(e) => setFolder(e.target.value)} autoFocus={!!invite} />
            {inDesktopWindow() && (
              <button type="button" onClick={choose}>
                Choose…
              </button>
            )}
          </span>
          <span className="muted small">Created if it does not exist. The suggested folder is fine; your blueprints and your teams' are kept here.</span>
        </label>
        {err && <div className="error-box">{err}</div>}
        <button type="submit" className="primary" {...act('setup.save')} disabled={busy}>
          {busy ? 'Starting…' : invite ? `Save and join ${invite.name}` : 'Save and start'}
        </button>
      </form>
    </div>
  )
}

/** Blueprints lists the blueprints of the scope (spec 11.1), the most
 * recently active first. In Personal each offers Publish to team…; in a
 * team folder one that is not shared says why. */
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
          {personal ? (
            <>
              <p>
                <b>Nothing in Personal yet.</b> Personal is only on this computer: what you write here stays yours until you publish it to a
                team.
              </p>
              <p className="muted">
                Put a Markdown file into <code>{status.folder}/personal</code>, or ask an agent to write one (<code>kalka reference</code>{' '}
                tells it how).
              </p>
              <div className="row center">
                <OpenFolder rel="personal" />
                <AddSamples onAdded={reload} />
              </div>
            </>
          ) : (
            <>
              <p>
                <b>No blueprints in {where} yet.</b> They arrive as members publish them.
              </p>
              <div className="row">
                <a className="button" href={scopeHref(PERSONAL)}>
                  Publish one from Personal
                </a>
                <a href={scopeHref(scope, 'team')}>Invite a teammate</a>
              </div>
            </>
          )}
        </div>
      </div>
    )
  }
  const sorted = [...list].sort((a, b) => (b.activity ?? b.modified).localeCompare(a.activity ?? a.modified))
  const mine = (b: BlueprintInfo) => !!b.owner && b.owner.toLowerCase() === status.email.toLowerCase()
  return (
    <div className="page">
      <h1>Blueprints · {where}</h1>
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr>
              <th>Blueprint</th>
              <th>Owner</th>
              <th>Version</th>
              <th>Last activity</th>
              {personal && <th />}
            </tr>
          </thead>
          <tbody>
            {sorted.map((b) => {
              const why = notShared.get(b.rel)
              const broken = b.errors > 0 || !!b.parse_error || !!b.duplicate
              return (
                <tr key={b.path}>
                  <td>
                    <a href={docHref(b.rel)}>{b.title || b.rel}</a>
                    {(b.new ?? 0) > 0 && <span className="badge new">{b.new} new</span>}
                    {b.unreleased && <span className="badge">{mine(b) ? 'unreleased edits' : 'edited, not released'}</span>}
                    {b.parse_error && <span className="badge warn">{b.parse_error}</span>}
                    {b.errors > 0 && <span className="badge warn">{plural(b.errors, 'error')}</span>}
                    {b.duplicate && <span className="badge warn">same kalka_id as another file</span>}
                    {why && <div className="small warn-text">Not shared: {why.replace(/^not shared: /, '')}</div>}
                  </td>
                  <td title={b.owner}>{!b.owner ? <span className="muted">—</span> : mine(b) ? 'you' : shortName(b.owner)}</td>
                  <td>{b.version ? `v${b.version}` : <span className="muted">{personal ? 'not shared' : '—'}</span>}</td>
                  <td title={b.activity ?? b.modified}>{ago(b.activity ?? b.modified)}</td>
                  {personal && (
                    <td>
                      {!broken && (
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

/** OpenFolder opens a folder under the blueprint folder in the file
 * manager (spec 11.1). */
export function OpenFolder({ rel = '', label = 'Open folder' }: { rel?: string; label?: string }) {
  const [err, setErr] = useState('')
  return (
    <span className="row">
      <button type="button" onClick={() => api.openFolder(rel).then(() => setErr(''), (e: Error) => setErr(e.message))}>
        {label}
      </button>
      {err && <span className="error-text small">{err}</span>}
    </span>
  )
}

// PreBacklog is Seeds (spec 6.11): every seed in the scope, with its
// owner, where it was split off from and its last activity.
export function PreBacklog({ scope }: { scope: string }) {
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
      <h1>Seeds</h1>
      <p className="muted">
        Ideas split off from growing blueprints, kept small until someone takes them up. Split one off from a blueprint (More ▾ Split
        off a seed…) or from a deviation.
      </p>
      {err && <div className="error-box">{err}</div>}
      {rows && rows.length === 0 && (
        <div className="empty">
          <p>
            <b>No seeds yet.</b> When a review turns up an idea that does not belong in the plan, split it off as a seed instead of
            letting the plan grow.
          </p>
        </div>
      )}
      {rows && rows.length > 0 && (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>Seed</th>
                <th>Owner</th>
                <th>Split off from</th>
                <th>Last activity</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.kalka_id + r.rel}>
                  <td>
                    <a href={docHref(r.rel)}>{r.title}</a>
                  </td>
                  <td title={r.owner}>{r.mine ? 'you' : shortName(r.owner)}</td>
                  <td>
                    {r.parent_rel ? <a href={docHref(r.parent_rel)}>{r.parent_title || r.parent_rel}</a> : r.parent ? <span className="muted">not in your folder</span> : '—'}
                  </td>
                  <td title={r.active}>{ago(r.active)}</td>
                  <td>
                    {r.mine && (
                      <button type="button" className="link" {...act('seed.promote')} disabled={busy === r.rel} onClick={() => promote(r.rel)}>
                        Turn into a blueprint
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

/** AddSamples copies the sample blueprints into Personal/samples (kalka
 * samples, spec 12.1); files already there are kept. */
export function AddSamples({ onAdded }: { onAdded?: () => void }) {
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState('')
  const add = async () => {
    setBusy(true)
    try {
      const r = await api.samples()
      setNote(r.written.length ? `Added ${plural(r.written.length, 'sample')} to Personal/samples.` : 'The samples are already in Personal/samples.')
      onAdded?.()
    } catch (e) {
      setNote((e as Error).message)
    }
    setBusy(false)
  }
  return (
    <>
      <button type="button" onClick={add} disabled={busy} {...act('samples.add')}>
        {busy ? 'Adding…' : 'Add sample blueprints'}
      </button>
      {note && <span className="muted small">{note}</span>}
    </>
  )
}
