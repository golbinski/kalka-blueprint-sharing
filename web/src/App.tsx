import { useCallback, useEffect, useRef, useState } from 'react'
import { api, ApiError, events, type BlueprintInfo, type Doc, type Entry, type Notice, type Status } from './api'
import { PublishDialog, SeedDialog } from './Dialogs'
import { notifyNotice, permission, requestPermission } from './notify'
import { SettingsPage } from './Settings'
import { CanvasPage } from './Canvas'
import { Review } from './Review'
import { Blueprints, PreBacklog, Setup } from './Views'
import { JoinScreen } from './Join'
import { TracePage } from './Trace'
import { DomainsPage, TeamPage } from './Team'
import { canvasHref, docHref, roleText, traceHref, useRoute } from './util'

type Toast = { id: number; text: string }

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

  const toast = (text: string) => {
    const id = Date.now() + Math.random()
    setToasts((t) => [...t, { id, text }])
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), 6000)
  }

  const loadStatus = useCallback(async () => {
    try {
      setStatus(await api.status())
      setFatal(null)
    } catch (e) {
      if (e instanceof ApiError) setFatal(e)
    }
  }, [])
  const loadList = useCallback(() => api.blueprints().then(setList, () => {}), [])
  const loadDoc = useCallback(async () => {
    const p = docPathRef.current
    const v = docVersionRef.current
    if (!p) return
    try {
      const d = await api.doc(p, v)
      if (docPathRef.current === p && docVersionRef.current === v) {
        setDoc(d)
        setDocErr('')
      }
    } catch (e) {
      setDocErr((e as Error).message)
    }
  }, [])

  useEffect(() => {
    loadStatus()
  }, [loadStatus])

  const ready = status !== null && !status.setup
  useEffect(() => {
    if (!ready) return
    loadList()
    const stop = events(
      (type, data) => {
        switch (type) {
          case 'folder':
            loadList()
            loadDoc()
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
            if (n && !notifyNotice(n)) toast(n.message)
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

  if (fatal && (!status || fatal.kind === 'token')) {
    return (
      <div className="page narrow">
        <div className="brand-big">kalka</div>
        {fatal.kind === 'token' ? (
          <>
            <h1>Open kalka from its link</h1>
            <p>
              This page needs the one-time link that kalka opens when it starts. Run <code>kalka open</code> in a terminal, or
              restart <code>kalka</code>.
            </p>
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

  return (
    <>
      <header className="top">
        <a className="brand" href="#/">
          kalka
        </a>
        <span className="sep" />
        {inDoc && doc ? (
          <>
            <span className="doc-title">{doc.title}</span>
            <span className="muted small">{roleText(doc.role, doc.owner)}</span>
          </>
        ) : (
          <nav className="nav">
            <a href="#/" className={route.view === 'list' ? 'active' : ''}>
              Blueprints
            </a>
            <a href="#/seeds" className={route.view === 'seeds' ? 'active' : ''}>
              Pre-backlog
            </a>
            <a href="#/domains" className={route.view === 'domains' ? 'active' : ''}>
              Domains
            </a>
            <a href="#/team" className={route.view === 'team' ? 'active' : ''}>
              Team
            </a>
            <a href="#/settings" className={route.view === 'settings' ? 'active' : ''}>
              Settings
            </a>
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
          <a className="back" href="#/">
            All blueprints
          </a>
        )}
      </header>

      <div className="banners">
        {status.notice && <div className="banner notice">{status.notice}</div>}
        {status.paused && (
          <div className="banner warn">
            Sync is paused: kalka is offline and nothing reaches your teammates until you resume. Your own work is kept and sent
            then.{' '}
            <button type="button" className="link" onClick={() => api.setPaused(false).then(loadStatus, (e: Error) => toast(e.message))}>
              Resume sync
            </button>
          </div>
        )}
        {!status.online && !status.paused && (
          <div className="banner danger">
            Not connected to the rendezvous server{status.error ? `: ${status.error}` : ''}. Nothing reaches your teams until it is
            back; kalka keeps retrying.
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
        {!live && <div className="banner danger">Lost the connection to kalka. Live updates resume when it is back.</div>}
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
        {route.view === 'list' && <Blueprints list={list} status={status} />}
        {inDoc && docErr && (
          <div className="page">
            <div className="error-box">{docErr}</div>
          </div>
        )}
        {route.view === 'doc' && doc && <Review doc={doc} reload={loadDoc} onSeed={(e) => setSeedFor({ from: e })} />}
        {route.view === 'canvas' && doc && <CanvasPage doc={doc} />}
        {route.view === 'seeds' && <PreBacklog self={status.email} />}
        {route.view === 'team' && <TeamPage self={status.email} reloadKey={teamTick} />}
        {route.view === 'settings' && <SettingsPage tick={teamTick} />}
        {route.view === 'join' && <JoinScreen token={route.token} />}
        {route.view === 'domains' && <DomainsPage reloadKey={teamTick} list={list} />}
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
        {toasts.map((t) => (
          <div className="toast" key={t.id}>
            {t.text}
          </div>
        ))}
      </div>
    </>
  )
}
