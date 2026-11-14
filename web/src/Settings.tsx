import { useCallback, useEffect, useState } from 'react'
import { api, type Settings, type Summary } from './api'
import { localTime } from './util'

// SettingsPage is the Settings view (spec 11, 22.2): the daemon keeps the
// values in state.yaml; start at login is the system's own entry.
export function SettingsPage({ tick }: { tick: number }) {
  const [s, setS] = useState<Settings | null>(null)
  const [sum, setSum] = useState<Summary | null>(null)
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const load = useCallback(() => {
    api.settings().then(setS, (e: Error) => setErr(e.message))
    api.summary().then(setSum, () => {})
  }, [])
  useEffect(() => {
    load()
  }, [load, tick])
  const change = async (patch: Settings) => {
    setBusy(true)
    setErr('')
    try {
      setS(await api.applySettings(patch))
      api.summary().then(setSum, () => {})
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy(false)
  }
  if (!s) return <div className="page muted">{err || 'Loading…'}</div>
  return (
    <div className="page narrow">
      <h1>Settings</h1>
      {sum && (
        <p className="small">
          {sum.paused ? 'Paused' : sum.online ? 'Online' : 'Offline' + (sum.error ? `: ${sum.error}` : '')}
          {' · '}
          {sum.peers_online} of {sum.peers_known} peers reachable
          {' · '}
          last sync {sum.last_sync ? localTime(sum.last_sync) : 'none since start'}
          {sum.prompts + sum.joins > 0 && <> · {sum.prompts + sum.joins} waiting for you</>}
          {sum.pushes > 0 && <> · {sum.pushes} team push{sum.pushes > 1 ? 'es' : ''} queued</>}
        </p>
      )}
      {err && <div className="error-box">{err}</div>}
      <div className="settings">
        <label className="setting">
          <input type="checkbox" checked={!!s.paused} disabled={busy} onChange={(e) => change({ paused: e.target.checked })} />
          <span>
            <b>Pause sync</b>
            <span className="muted small">
              kalka goes offline: no syncs, no team replication, no joins. Your own comments, seeds and releases are kept and
              sent after you resume. The pause lasts across restarts.
            </span>
          </span>
        </label>
        <label className="setting">
          <input type="checkbox" checked={!!s.notifications} disabled={busy} onChange={(e) => change({ notifications: e.target.checked })} />
          <span>
            <b>Notifications</b>
            <span className="muted small">
              New comments on your documents, joins, newer domain versions. Questions from teammates' syncs always show here.
            </span>
          </span>
        </label>
        <label className="setting">
          <input
            type="checkbox"
            checked={!!s.start_at_login}
            disabled={busy || !s.start_at_login_available}
            onChange={(e) => change({ start_at_login: e.target.checked })}
          />
          <span>
            <b>Start at login</b>
            <span className="muted small">
              {s.start_at_login_available
                ? 'Team replication works only while members are online at the same time, so kalka runs in the background from login.'
                : 'This kalka cannot change its login entry; use `kalka autostart on` in a terminal.'}
              {s.start_at_login_error && <> ({s.start_at_login_error})</>}
            </span>
          </span>
        </label>
        <label className="setting">
          <input
            type="checkbox"
            checked={!!s.keep_running_on_close}
            disabled={busy}
            onChange={(e) => change({ keep_running_on_close: e.target.checked })}
          />
          <span>
            <b>Keep running when the window is closed</b>
            <span className="muted small">For the desktop app: closing its window hides it, and kalka keeps syncing. A browser tab never stops kalka.</span>
          </span>
        </label>
      </div>
    </div>
  )
}
