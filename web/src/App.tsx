import { useCallback, useEffect, useRef, useState } from 'react'
import { api, ApiError, events, type BlueprintInfo, type Doc, type Peer, type Prompt, type Status, type SyncResult } from './api'
import { PromptCard, SyncDialog } from './Dialogs'
import { Review } from './Review'
import { Blueprints, Peers, Setup } from './Views'
import { roleText, useRoute } from './util'

type Toast = { id: number; text: string }

export function App() {
  const route = useRoute()
  const [status, setStatus] = useState<Status | null>(null)
  const [fatal, setFatal] = useState<ApiError | null>(null)
  const [list, setList] = useState<BlueprintInfo[]>([])
  const [peers, setPeers] = useState<Peer[]>([])
  const [doc, setDoc] = useState<Doc | null>(null)
  const [docErr, setDocErr] = useState('')
  const [prompts, setPrompts] = useState<Prompt[]>([])
  const [dialog, setDialog] = useState<'sync' | 'handoff' | null>(null)
  const [toasts, setToasts] = useState<Toast[]>([])
  const [live, setLive] = useState(true)
  const docPath = route.view === 'doc' ? route.path : ''
  const docPathRef = useRef(docPath)
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
  const loadPeers = useCallback(() => api.peers().then(setPeers, () => {}), [])
  const loadDoc = useCallback(async () => {
    const p = docPathRef.current
    if (!p) return
    try {
      const d = await api.doc(p)
      if (docPathRef.current === p) {
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
    loadPeers()
    api.prompts().then((ps) => setPrompts(ps.filter((p) => !p.scope)), () => {})
    const stop = events(
      (type, data) => {
        switch (type) {
          case 'folder':
            loadList()
            loadDoc()
            break
          case 'status':
            loadStatus()
            break
          case 'peers':
            loadPeers()
            break
          case 'prompt': {
            const p = data as Prompt
            if (!p.scope) setPrompts((ps) => (ps.some((x) => x.id === p.id) ? ps : [...ps, p]))
            break
          }
          case 'prompt-closed': {
            const p = data as Prompt
            setPrompts((ps) => ps.filter((x) => x.id !== p.id))
            break
          }
          case 'sync': {
            const r = data as SyncResult
            const what = r.outcome === 'rejected' ? `rejected (${r.reason})` : r.outcome
            toast(`${r.peer} synced “${r.title ?? r.kalka_id}”: ${what}`)
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
    const t = setInterval(loadPeers, 30000)
    return () => {
      stop()
      clearInterval(t)
    }
  }, [ready, loadList, loadPeers, loadDoc, loadStatus])

  useEffect(() => {
    setDoc(null)
    setDocErr('')
    if (ready && docPath) loadDoc()
  }, [docPath, ready, loadDoc])

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

  const changedKeys = peers.filter((p) => p.key_changed)
  const inDoc = route.view === 'doc'

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
            <a href="#/peers" className={route.view === 'peers' ? 'active' : ''}>
              Peers
            </a>
          </nav>
        )}
        <span className="grow" />
        {inDoc && doc && (
          <>
            <nav className="tabs" aria-label="Views">
              <a className="active" href={location.hash}>
                Document
              </a>
              <span className="disabled" title="Canvas review comes in a later milestone">
                Canvas
              </span>
              <span className="disabled" title="The trace view comes in a later milestone">
                Trace
              </span>
            </nav>
            {doc.role === 'owner' && (
              <button type="button" onClick={() => setDialog('handoff')}>
                Hand off to…
              </button>
            )}
            <button type="button" className="primary" onClick={() => setDialog('sync')}>
              Sync with…
            </button>
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
        {!status.online && (
          <div className="banner danger">
            Not connected to the rendezvous server{status.error ? `: ${status.error}` : ''}. Syncs cannot start until it is back;
            kalka keeps retrying.
          </div>
        )}
        {!live && <div className="banner danger">Lost the connection to kalka. Live updates resume when it is back.</div>}
        {changedKeys.map((p) => (
          <div className="banner danger loud" key={p.email}>
            <b>{p.email} presented a different identity key.</b> This happens after a reinstall, or when someone else is using
            that email. Syncs with {p.email} are blocked. Confirm with them in person or by phone, then remove the old pin on the{' '}
            <a href="#/peers">Peers</a> page and verify the new code.
          </div>
        ))}
        {inDoc && doc?.owner_edited && (
          <div className="banner warn">
            <code>owner:</code> was changed by hand in this file. That works, but without a handoff entry and without the
            protection of a handoff: use “Hand off to…” instead.
          </div>
        )}
        {inDoc && doc?.duplicate && (
          <div className="banner warn">Another file in the folder has the same kalka_id. Syncing is refused until one is removed.</div>
        )}
        {inDoc && doc && doc.problems.some((p) => p.severity === 'error') && (
          <div className="banner warn">
            This file has format errors and cannot be synced:
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
        {route.view === 'peers' && <Peers peers={peers} reload={loadPeers} />}
        {inDoc && docErr && (
          <div className="page">
            <div className="error-box">{docErr}</div>
          </div>
        )}
        {inDoc && doc && <Review doc={doc} reload={loadDoc} />}
      </main>

      {prompts.length > 0 && (
        <div className="modal-back">
          <div className="modal" role="dialog" aria-label="Questions from kalka">
            <h2>{prompts.length === 1 ? 'A teammate is syncing with you' : `${prompts.length} questions`}</h2>
            {prompts.map((p) => (
              <PromptCard key={p.id} prompt={p} onDone={() => setPrompts((ps) => ps.filter((x) => x.id !== p.id))} />
            ))}
          </div>
        </div>
      )}
      {dialog && doc && (
        <SyncDialog
          path={doc.rel}
          title={doc.title}
          intent={dialog}
          peers={peers}
          self={status.email}
          onClose={() => {
            setDialog(null)
            loadDoc()
            loadPeers()
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
