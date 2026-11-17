// SyncChip is the header's status line (spec 12.1 "Header line", 22.1):
// synced when and with how many teammates online, or paused, offline, not
// synced yet. Its panel lists each team's last sync with Sync now.
import { useEffect, useRef, useState } from 'react'
import { act } from './actions'
import { api, type Summary, type Team } from './api'
import { shortTime } from './util'

export type ChipState = 'paused' | 'offline' | 'starting' | 'synced' | 'never'

/** chipText is what the chip says, and its colour. */
export function chipText(s: Summary | null): { state: ChipState; text: string } {
  if (!s) return { state: 'starting', text: 'Starting…' }
  if (s.paused) return { state: 'paused', text: 'Paused' }
  if (!s.online) return { state: 'offline', text: 'Offline' }
  const peers = s.peers_known > 0 ? ` · ${s.peers_online} of ${s.peers_known} teammates online` : ''
  if (!s.last_sync) return { state: 'never', text: 'Not synced yet' + peers }
  return { state: 'synced', text: `Synced ${shortTime(s.last_sync)}` + peers }
}

export function SyncChip({ summary, teams, onChange }: { summary: Summary | null; teams: Team[]; onChange: () => void }) {
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState('')
  const [err, setErr] = useState('')
  const box = useRef<HTMLDivElement>(null)
  const button = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!open) return
    const outside = (e: MouseEvent) => box.current && !box.current.contains(e.target as Node) && setOpen(false)
    document.addEventListener('mousedown', outside)
    return () => document.removeEventListener('mousedown', outside)
  }, [open])
  const { state, text } = chipText(summary)
  const mine = teams.filter((t) => t.me !== 'none')
  const sync = async (team: string) => {
    setBusy(team)
    setErr('')
    try {
      await api.replicate(team)
      onChange()
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy('')
  }
  return (
    <div
      className="sync-chip-box"
      ref={box}
      onKeyDown={(e) => {
        if (e.key === 'Escape' && open) {
          setOpen(false)
          button.current?.focus()
        }
      }}
    >
      <button ref={button} type="button" className={'sync-chip ' + state} aria-expanded={open} onClick={() => setOpen(!open)}>
        <span className="dot" aria-hidden />
        {text}
      </button>
      {open && (
        <div className="sync-panel">
          {state === 'offline' && <p className="small">Cannot reach your team's server. kalka keeps trying.{summary?.error && <span className="muted"> ({summary.error})</span>}</p>}
          {state === 'paused' && <p className="small">Sync is paused. Nothing is sent or received until you resume it.</p>}
          {mine.length === 0 && <p className="muted small">You are in no team yet.</p>}
          {mine.map((t) => {
            const r = t.replication
            const others = t.members.filter((m) => !m.you)
            const online = others.filter((m) => m.presence === 'online').length
            return (
              <div key={t.team} className="sync-row">
                <span className="grow">
                  <b>{t.name}</b>
                  <span className="muted small">
                    {' '}
                    · {r?.last_round ? `last sync ${shortTime(r.last_round)}` : 'no sync yet'} · {online} of {others.length} online
                  </span>
                </span>
                {others.length > 0 && (
                  <button type="button" className="link" {...act('team.sync')} disabled={!!busy || summary?.paused} onClick={() => sync(t.team)}>
                    {busy === t.team ? 'Syncing…' : 'Sync now'}
                  </button>
                )}
              </div>
            )
          })}
          {err && <div className="error-text small">{err}</div>}
        </div>
      )}
    </div>
  )
}
