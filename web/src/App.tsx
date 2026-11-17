import { useCallback, useEffect, useRef, useState } from 'react'
import { act } from './actions'
import { api, ApiError, events, type BlueprintInfo, type Doc, type Entry, type Join, type Notice, type Status, type Summary, type Team } from './api'
import { PublishDialog, SeedDialog } from './Dialogs'
import { noticeHref, notifyNotice, permission, requestPermission } from './notify'
import { SettingsPage } from './Settings'
import { CanvasPage } from './Canvas'
import { Review } from './Review'
import { Blueprints, PreBacklog, Setup } from './Views'
import { JoinScreen } from './Join'
import { TracePage } from './Trace'
import { DomainsPage, TeamPage } from './Team'
import { Switcher } from './Switcher'
import { NeedsPage } from './Needs'
import { SyncChip } from './Sync'
import { inDesktopWindow } from './shell'
import { findTeam, PERSONAL, rememberedScope, rememberScope, resolveScope } from './scope'
import { canvasHref, docHref, roleText, scopeHref, traceHref, useRoute, type ScopedView } from './util'

type Toast = { id: number; text: string; href: string }

export function App() {
  const route = useRoute()
  const [status, setStatus] = useState<Status | null>(null)
  const [fatal, setFatal] = useState<ApiError | null>(null)
  const [list, setList] = useState<BlueprintInfo[]>([])
  const [doc, setDoc] = useState<Doc | null>(null)
  const [docErr, setDocErr] = useState('')
  const [publishing, setPublishing] = useState(false)
  const [seedFor, setSeedFor] = useState<{ from?: Entry } | null>(null)
  const [toasts, setToasts] = useState<Toast[]>([])
  const [live, setLive] = useState(true)
  const [teamTick, setTeamTick] = useState(0)
  const [teams, setTeams] = useState<Team[] | null>(null)
  const [joins, setJoins] = useState<Join[]>([])
  const [summary, setSummary] = useState<Summary | null>(null)
  const [unreadCount, setUnreadCount] = useState(0)
  const [notices, setNotices] = useState<(Notice & { at: string })[]>([])
  const [remembered, setRemembered] = useState(rememberedScope)
  const routeScope = 'scope' in route ? route.scope : ''
  const scope = resolveScope(routeScope, remembered, teams)
  const team = scope === PERSONAL || !teams ? undefined : findTeam(scope, teams)
  const scopeRef = useRef(scope)
  scopeRef.current = scope
  const [notifyPerm, setNotifyPerm] = useState(permission)
  const [notifyAsked, setNotifyAsked] = useState(() => {
    try {
      return localStorage.getItem('kalka.notify-dismissed') === '1'
    } catch {
      return false
    }
  })
  const docPath = route.view === 'doc' || route.view === 'trace' || route.view === 'canvas' ? route.path : ''
  const docVersion = route.view === 'doc' ? route.version : undefined
  const docPathRef = useRef(docPath)
  const docVersionRef = useRef(docVersion)
  docVersionRef.current = docVersion
  // A dialog belongs to the document it was opened on.
  useEffect(() => setSeedFor(null), [docPath])
  docPathRef.current = docPath

  const toast = (text: string, href = '') => {
    const id = Date.now() + Math.random()
    setToasts((t) => [...t, { id, text, href }])
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 8000)
  }

  const loadStatus = useCallback(async () => {
    try {
      setStatus(await api.status())
      setFatal(null)
    } catch (e) {
      if (e instanceof ApiError) setFatal(e)
    }
  }, [])
  const loadList = useCallback(() => {
    const sc = scopeRef.current
    if (!sc) return
    api.blueprints(sc).then((l) => scopeRef.current === sc && setList(l), () => {})
  }, [])
  const loadTeams = useCallback(() => {
    api.teams().then(setTeams, () => {})
    api.joins().then(setJoins, () => {})
    api.summary().then(setSummary, () => {})
    api.unread().then((l) => setUnreadCount(l.length), () => {})
  }, [])
  const loadDoc = useCallback(async () => {
    const p = docPathRef.current
    const v = docVersionRef.current
    if (!p) return
    try {
      const d = await api.doc(p, v)
      if (docPathRef.current === p && docVersionRef.current === v) {
        setDoc(d)
        setDocErr('')
        // What is on screen is read (Needs you, spec 11.3).
        if (!v) api.markRead(p).catch(() => {})
      }
    } catch (e) {
      setDocErr((e as Error).message)
    }
  }, [])

  useEffect(() => {
    loadStatus()
  }, [loadStatus])

  const ready = status !== null && !status.setup
  // The route's scope is remembered; opening a document switches to the
  // document's scope (spec 11.1).
  useEffect(() => {
    if (routeScope) {
      rememberScope(routeScope)
      setRemembered(routeScope)
    }
  }, [routeScope])
  useEffect(() => {
    if (doc && !doc.domain?.viewing && doc.scope) {
      rememberScope(doc.scope)
      setRemembered(doc.scope)
    }
  }, [doc])
  useEffect(() => {
    if (ready) loadTeams()
  }, [ready, teamTick, loadTeams])
  useEffect(() => {
    setList([])
    if (ready) loadList()
  }, [ready, scope, loadList])
  useEffect(() => {
    if (!ready) return
    const stop = events(
      (type, data) => {
        switch (type) {
          case 'folder':
            loadList()
            loadDoc()
            api.unread().then((l) => setUnreadCount(l.length), () => {})
            break
          case 'status':
            loadStatus()
            setTeamTick((n) => n + 1)
            break
          case 'settings':
            loadStatus()
            setTeamTick((n) => n + 1)
            break
          case 'notice': {
            const n = data as Notice
            if (!n) break
            setNotices((l) => [{ ...n, at: new Date().toISOString() }, ...l].slice(0, 10))
            if (!notifyNotice(n)) toast(n.message, noticeHref(n))
            break
          }
          case 'teams':
          case 'waiting':
            setTeamTick((n) => n + 1)
            break
          case 'refs': {
            // Background resolution finished for a blueprint.
            const p = (data as { path?: string } | undefined)?.path
            if (p && p === docPathRef.current) loadDoc()
            break
          }
        }
      },
      (open) => {
        setLive(open)
        // A restarted daemon has a new token: find out, so the page can
        // say how to get back in instead of going quiet.
        if (!open) loadStatus()
        if (open) {
          loadStatus()
          loadList()
          loadDoc()
        }
      },
    )
    return stop
  }, [ready, loadList, loadDoc, loadStatus])

  useEffect(() => {
    setDoc(null)
    setDocErr('')
    if (ready && docPath) loadDoc()
  }, [docPath, docVersion, ready, loadDoc])

  useEffect(() => {
    const view: Record<string, string> = { needs: 'Needs you', list: 'Blueprints', seeds: 'Seeds', team: 'Team', domains: 'Domains', settings: 'Settings', join: 'Join a team' }
    const what = doc && (route.view === 'doc' || route.view === 'trace' || route.view === 'canvas') ? doc.title : view[route.view] ?? ''
    document.title = what ? `${what} · kalka` : 'kalka'
  }, [route.view, doc])

  if (fatal && (!status || fatal.kind === 'token')) {
    return (
      <div className="page narrow">
        <div className="brand-big">kalka</div>
        {fatal.kind === 'token' ? (
          <>
            <h1>{inDesktopWindow() ? 'kalka restarted' : 'Open kalka from its link'}</h1>
            {inDesktopWindow() ? (
              <p>Close this window and open kalka again from its icon.</p>
            ) : (
              <p>
                This page needs the one-time link that kalka opens when it starts. Run <code>kalka open</code> in a terminal, or open kalka
                from its icon.
              </p>
            )}
          </>
        ) : (
          <>
            <h1>kalka is not reachable</h1>
            <p>{fatal.message}</p>
          </>
        )}
      </div>
    )
  }
  if (!status) return <div className="page muted">Loading…</div>
  if (status.setup) return <Setup status={status} />

  const inDoc = route.view === 'doc' || route.view === 'trace' || route.view === 'canvas'
  const personal = scope === PERSONAL
  const navLink = (view: ScopedView, label: string, extra?: React.ReactNode) => (
    <a href={scopeHref(scope, view)} className={route.view === view ? 'active' : ''}>
      {label}
      {extra}
    </a>
  )
  const needs = (summary?.waiting.total ?? 0) + unreadCount

  return (
    <>
      <header className="top">
        <Switcher teams={teams ?? []} joins={joins} scope={scope} onCreated={loadTeams} />
        <span className="sep" />
        {inDoc && doc ? (
          <>
            <span className="doc-title">{doc.title}</span>
            <span className="muted small">{roleText(doc.role, doc.owner)}</span>
          </>
        ) : (
          <nav className="nav">
            <a href="#/needs" className={route.view === 'needs' ? 'active' : ''}>
              Needs you{needs > 0 && <span className="count">{needs}</span>}
            </a>
            {navLink('list', 'Blueprints')}
            {navLink('seeds', 'Pre-backlog')}
            {!personal && navLink('domains', 'Domains')}
            {!personal && navLink('team', 'Team')}
          </nav>
        )}
        <span className="grow" />
        {inDoc && doc && (
          <>
            <nav className="tabs" aria-label="Views">
              <a className={route.view === 'doc' ? 'active' : ''} href={docHref(doc.rel)}>
                Document
              </a>
              <a className={route.view === 'canvas' ? 'active' : ''} href={canvasHref(doc.rel)}>
                Canvas
              </a>
              {doc.traces.length > 0 ? (
                <a className={route.view === 'trace' ? 'active' : ''} href={traceHref(doc.rel, route.view === 'trace' ? route.index : 0)}>
                  Trace
                </a>
              ) : (
                <span className="disabled" title="This blueprint has no trace blocks">
                  Trace
                </span>
              )}
            </nav>
            {!doc.domain?.viewing && !doc.problems.some((p) => p.severity === 'error') && (
              <button type="button" onClick={() => setSeedFor({})} title="Split an idea off this blueprint into a seed you own">
                Create seed
              </button>
            )}
            {doc.role === 'owner' && !doc.team && !doc.problems.some((p) => p.severity === 'error') && (
              <button type="button" onClick={() => setPublishing(true)} title="Share this blueprint with every member of a team">
                Publish to team…
              </button>
            )}
          </>
        )}
        {inDoc && (
          <a className="back" href={scopeHref(scope)}>
            All blueprints
          </a>
        )}
        <SyncChip summary={summary} teams={teams ?? []} onChange={loadTeams} />
        {!inDoc && (
          <a href="#/settings" className={'settings-link' + (route.view === 'settings' ? ' active' : '')}>
            Settings
          </a>
        )}
      </header>

      <div className="banners">
        {status.notice && <div className="banner notice">{status.notice}</div>}
        {status.paused && (
          <div className="banner warn">
            Sync is paused: kalka is offline and nothing reaches your teammates until you resume. Your own work is kept and sent
            then.{' '}
            <button type="button" className="link" {...act('sync.pause')} onClick={() => api.setPaused(false).then(loadStatus, (e: Error) => toast(e.message))}>
              Resume sync
            </button>
          </div>
        )}
        {!status.online && !status.paused && (
          <div className="banner danger">
            Cannot reach your team's kalka server. Nothing is sent or received until it is back; kalka keeps trying.
            {status.error && (
              <details className="small">
                <summary>Details</summary>
                {status.error}
              </details>
            )}
          </div>
        )}
        {notifyPerm === 'default' && !notifyAsked && (
          <div className="banner notice">
            Turn on desktop notifications so you hear of comments and team changes while this tab is in the background.{' '}
            <button type="button" className="link" onClick={() => requestPermission().then(setNotifyPerm)}>
              Turn on
            </button>{' '}
            ·{' '}
            <button
              type="button"
              className="link"
              onClick={() => {
                setNotifyAsked(true)
                try {
                  localStorage.setItem('kalka.notify-dismissed', '1')
                } catch {
                  /* private window */
                }
              }}
            >
              Not now
            </button>
          </div>
        )}
        {!live && (
          <div className="banner danger">
            Lost the connection to kalka. Live updates resume when it is back
            {inDesktopWindow() ? '; if it does not come back, open kalka again from its icon.' : '.'}
          </div>
        )}
        {inDoc && doc?.mismatch && !doc.duplicate && (
          <div className="banner warn">
            Not shared: {doc.mismatch.replace(/^not shared: /, '')}.
          </div>
        )}
        {inDoc && doc?.duplicate && (
          <div className="banner warn">Another file in the folder has the same kalka_id. Publishing is refused until one is removed.</div>
        )}
        {inDoc && doc && doc.problems.some((p) => p.severity === 'error') && (
          <div className="banner warn">
            This file has format errors and cannot be published:
            <ul>
              {doc.problems
                .filter((p) => p.severity === 'error')
                .map((p, i) => (
                  <li key={i}>
                    {p.line ? `line ${p.line}: ` : ''}
                    {p.message}
                  </li>
                ))}
            </ul>
          </div>
        )}
      </div>

      <main>
        {route.view === 'list' && scope && <Blueprints list={list} status={status} scope={scope} team={team} reload={loadList} />}
        {inDoc && docErr && (
          <div className="page">
            <div className="error-box">{docErr}</div>
          </div>
        )}
        {route.view === 'doc' && doc && <Review doc={doc} reload={loadDoc} onSeed={(e) => setSeedFor({ from: e })} />}
        {route.view === 'canvas' && doc && <CanvasPage doc={doc} />}
        {route.view === 'seeds' && scope && <PreBacklog self={status.email} scope={scope} />}
        {route.view === 'team' && !personal && scope && <TeamPage self={status.email} team={scope} reloadKey={teamTick} />}
        {route.view === 'needs' && <NeedsPage reloadKey={teamTick} notices={notices} />}
        {(route.view === 'team' || route.view === 'domains') && personal && (
          <div className="page">
            <div className="empty">Personal is only on this computer: it has no team or domains. Choose a team in the switcher.</div>
          </div>
        )}
        {route.view === 'settings' && <SettingsPage tick={teamTick} />}
        {route.view === 'join' && (
          <JoinScreen token={route.token} status={status} reloadKey={teamTick} onResume={() => api.setPaused(false).then(loadStatus, (e: Error) => toast(e.message))} />
        )}
        {route.view === 'domains' && !personal && scope && <DomainsPage team={scope} reloadKey={teamTick} list={list} />}
        {route.view === 'trace' && doc && <TracePage doc={doc} index={route.index} nodeId={route.node} reload={loadDoc} />}
      </main>

      {publishing && doc && (
        <PublishDialog
          doc={doc}
          onClose={() => setPublishing(false)}
          onDone={() => {
            loadDoc()
            loadList()
          }}
        />
      )}
      {seedFor && doc && (
        <SeedDialog
          doc={doc}
          from={seedFor.from}
          onClose={() => setSeedFor(null)}
          onDone={() => {
            loadDoc()
            loadList()
          }}
        />
      )}
      <div className="toasts" aria-live="polite">
        {toasts.map((t) =>
          t.href ? (
            <a className="toast" key={t.id} href={t.href} onClick={() => setToasts((l) => l.filter((x) => x.id !== t.id))}>
              {t.text}
            </a>
          ) : (
            <div className="toast" key={t.id}>
              {t.text}
            </div>
          ),
        )}
      </div>
    </>
  )
}
