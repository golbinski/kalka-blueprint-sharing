// The team switcher (spec 11.1): the current scope, and a menu with
// Personal, every team with its waiting badge, the pending joins, Create
// team… and Join with link or token….
import { useEffect, useRef, useState } from 'react'
import { act } from './actions'
import { api, type Join, type Team } from './api'
import { joiningLines, scopeLabel, switcherRows } from './scope'
import { scopeHref } from './util'

export function Switcher({ teams, joins, scope, onCreated }: { teams: Team[]; joins: Join[]; scope: string; onCreated: () => void }) {
  const [open, setOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false)
    }
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', close)
    document.addEventListener('keydown', esc)
    return () => {
      document.removeEventListener('mousedown', close)
      document.removeEventListener('keydown', esc)
    }
  }, [open])
  useEffect(() => setCreating(false), [open])
  const rows = switcherRows(teams, scope)
  const total = rows.reduce((n, r) => n + (r.current ? 0 : r.waiting), 0)
  const joining = joiningLines(joins)
  return (
    <div className="switcher" ref={box}>
      <button type="button" className="switcher-button" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen(!open)}>
        <span className="brand">kalka</span>
        <span className="switcher-scope">{scopeLabel(scope, teams)}</span>
        {total > 0 && (
          <span className="count" title="waiting for you in other teams">
            {total}
          </span>
        )}
        <span className="caret" aria-hidden>
          ▾
        </span>
      </button>
      {open && (
        <div className="switcher-menu" role="menu">
          {rows.map((r) => (
            <div key={r.key} className={'switcher-row' + (r.current ? ' current' : '')}>
              <a href={r.href} role="menuitem" {...act('team.switch')} onClick={() => setOpen(false)}>
                {r.label}
                {r.hash && <span className="muted mono small"> {r.hash}</span>}
              </a>
              {r.waiting > 0 && (
                <a className="count" href={scopeHref(r.key, 'waiting')} title={`${r.waiting} waiting for you`} onClick={() => setOpen(false)}>
                  {r.waiting}
                </a>
              )}
            </div>
          ))}
          {joining.map((j) => (
            <div key={j} className="switcher-row muted small">
              {j}
            </div>
          ))}
          <hr />
          {creating ? (
            <CreateTeam
              onDone={(key) => {
                setOpen(false)
                onCreated()
                location.hash = scopeHref(key)
              }}
            />
          ) : (
            <button type="button" className="link switcher-item" onClick={() => setCreating(true)}>
              Create team…
            </button>
          )}
          <a className="switcher-item" href="#/join" {...act('team.join')} onClick={() => setOpen(false)}>
            Join with link or token…
          </a>
        </div>
      )}
    </div>
  )
}

function CreateTeam({ onDone }: { onDone: (key: string) => void }) {
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  return (
    <form
      className="switcher-create"
      onSubmit={async (e) => {
        e.preventDefault()
        setBusy(true)
        setErr('')
        try {
          const t = await api.createTeam(name.trim())
          onDone(t.name)
        } catch (e) {
          setErr((e as Error).message)
        }
        setBusy(false)
      }}
    >
      <p className="muted small">You become its first owner. Lower-case letters, digits and dashes.</p>
      <div className="row">
        <input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="lending-desk" aria-label="Team name" pattern="[a-z0-9][a-z0-9-]*" />
        <button type="submit" {...act('team.create')} disabled={busy || !name.trim()}>
          Create
        </button>
      </div>
      {err && <div className="error-text small">{err}</div>}
    </form>
  )
}
