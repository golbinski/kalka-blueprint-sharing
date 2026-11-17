// The team switcher (spec 11.1): the current scope, and a list with
// Personal, every team with its waiting badge, the joins in progress or
// failed (each a link to its progress), Create team… and Join with an
// invite…. Up and Down move through it, Escape closes it and returns to
// the button.
import { useEffect, useRef, useState } from 'react'
import { act } from './actions'
import { api, type Join, type Team } from './api'
import { joiningLines, scopeLabel, switcherRows, teamSlug } from './scope'

export function Switcher({ teams, joins, scope, onCreated }: { teams: Team[]; joins: Join[]; scope: string; onCreated: () => void }) {
  const [open, setOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  const button = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLDivElement>(null)
  const close = (refocus: boolean) => {
    setOpen(false)
    if (refocus) button.current?.focus()
  }
  useEffect(() => {
    if (!open) return
    const outside = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', outside)
    // Focus the current row, or the first.
    const items = menu.current?.querySelectorAll<HTMLElement>('[data-item]')
    const cur = menu.current?.querySelector<HTMLElement>('.current [data-item]')
    ;(cur ?? items?.[0])?.focus()
    return () => document.removeEventListener('mousedown', outside)
  }, [open])
  useEffect(() => setCreating(false), [open])
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault()
      close(true)
      return
    }
    if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
    if ((e.target as HTMLElement).tagName === 'INPUT') return
    const items = [...(menu.current?.querySelectorAll<HTMLElement>('[data-item]') ?? [])]
    if (items.length === 0) return
    e.preventDefault()
    const i = items.indexOf(document.activeElement as HTMLElement)
    const next = e.key === 'ArrowDown' ? (i + 1) % items.length : (i - 1 + items.length) % items.length
    items[next].focus()
  }
  const rows = switcherRows(teams, scope)
  const total = rows.reduce((n, r) => n + (r.current ? 0 : r.waiting), 0)
  const joining = joiningLines(joins)
  return (
    <div className="switcher" ref={box} onKeyDown={onKey}>
      <button ref={button} type="button" className="switcher-button" aria-expanded={open} onClick={() => setOpen(!open)}>
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
        <div className="switcher-menu" ref={menu} aria-label="Teams">
          {rows.map((r) => (
            <div key={r.key} className={'switcher-row' + (r.current ? ' current' : '')}>
              <a href={r.href} data-item aria-current={r.current ? 'true' : undefined} {...act('team.switch')} onClick={() => setOpen(false)}>
                {r.label}
                {r.hash && <span className="muted mono small"> {r.hash}</span>}
              </a>
              {r.waiting > 0 && (
                <a className="count" href="#/needs" title={`${r.waiting} need you`} onClick={() => setOpen(false)}>
                  {r.waiting}
                </a>
              )}
            </div>
          ))}
          {joining.map((j) => (
            <div key={j.id} className="switcher-row">
              <a href="#/join" data-item onClick={() => setOpen(false)} className={j.failed ? 'join-failed' : ''}>
                {j.name} <span className="muted small">· {j.text}</span>
              </a>
            </div>
          ))}
          <hr />
          {creating ? (
            <CreateTeam
              onDone={(key) => {
                setOpen(false)
                onCreated()
                location.hash = '#/t/' + encodeURIComponent(key) + '/team'
              }}
            />
          ) : (
            <button type="button" data-item className="link switcher-item" onClick={() => setCreating(true)}>
              Create team…
            </button>
          )}
          <a className="switcher-item" data-item href="#/join" {...act('team.join')} onClick={() => setOpen(false)}>
            Join with an invite…
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
  const slug = teamSlug(name)
  return (
    <form
      className="switcher-create"
      onSubmit={async (e) => {
        e.preventDefault()
        setBusy(true)
        setErr('')
        try {
          const t = await api.createTeam(slug)
          onDone(t.name)
        } catch (e) {
          setErr((e as Error).message)
        }
        setBusy(false)
      }}
    >
      <p className="muted small">You become its first team owner. Next you invite your teammates.</p>
      <div className="row">
        <input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="platform-team" aria-label="Team name" />
        <button type="submit" {...act('team.create')} disabled={busy || !slug}>
          Create
        </button>
      </div>
      {name.trim() && slug !== name.trim() && <div className="muted small">{slug ? <>It will be named <b>{slug}</b>.</> : 'Use at least one letter or digit.'}</div>}
      {err && <div className="error-text small">{err}</div>}
    </form>
  )
}
