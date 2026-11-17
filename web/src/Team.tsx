// Team view (spec 11.1): the selected team's members with presence,
// owners, open invites, transfers and the log as a readable history;
// Invite, Remove (with a preview of the documents to be reassigned), Make
// owner, Sync now. Creating and joining teams is in the switcher.
import { useCallback, useEffect, useState } from 'react'
import { act } from './actions'
import { api, type BlueprintInfo, type DomainRow, type InviteResult, type Reassigned, type Team, type TeamMember } from './api'
import { findTeam } from './scope'
import { Transfers } from './Transfer'
import { docHref, localTime, plural } from './util'

export function TeamPage({ self, team, reloadKey }: { self: string; team: string; reloadKey: number }) {
  const [teams, setTeams] = useState<Team[] | null>(null)
  const [err, setErr] = useState('')
  const load = useCallback(() => {
    api.teams().then(setTeams, (e: Error) => setErr(e.message))
  }, [])
  useEffect(() => {
    load()
  }, [load, reloadKey])
  const t = teams ? findTeam(team, teams) : undefined
  return (
    <div className="page">
      <h1>{t ? t.name : 'Team'}</h1>
      <p className="muted">
        A team is a signed log of who belongs and who may change the roster. It lives in your folder next to the team's
        documents. Members trust each other's keys through it, so they never compare codes.
      </p>
      {err && <div className="error-box">{err}</div>}
      {teams && !t && <div className="empty">No team “{team}” here. Choose one in the switcher, or create or join one there.</div>}
      {t && <TeamCard key={t.team} t={t} self={self} reload={load} reloadKey={reloadKey} />}
    </div>
  )
}

function TeamCard({ t, self, reload, reloadKey }: { t: Team; self: string; reload: () => void; reloadKey: number }) {
  const owner = t.me === 'owner'
  const [err, setErr] = useState('')
  const [removing, setRemoving] = useState<{ email: string; docs: Reassigned[] | null } | null>(null)
  const [reassigning, setReassigning] = useState<{ email: string; docs: Reassigned[] | null } | null>(null)
  const run = async (f: () => Promise<unknown>) => {
    setErr('')
    try {
      await f()
      reload()
    } catch (e) {
      setErr((e as Error).message)
    }
  }
  const notCounting = (t.bad_lines?.length ?? 0) + (t.rejected ?? 0)
  return (
    <section className="team-card">
      <div className="team-head">
        <span className="muted small mono" title={t.team}>
          {t.team.slice(0, 12)}
        </span>
        <span className="grow" />
        <span className="muted small">
          {t.me === 'owner' ? 'you are an owner' : t.me === 'member' ? 'you are a member' : 'you are not a member'} · {t.dir}
        </span>
      </div>
      {t.error && <div className="error-box">{t.error}</div>}
      {notCounting > 0 && (
        <div className="banner warn">
          {plural(notCounting, 'line')} of the log do not count (damaged, or not signed by the key they name). They are kept in the file and
          shown in the history below.
        </div>
      )}
      {t.me !== 'none' && <Replication t={t} self={self} run={run} />}
      {err && <div className="error-box">{err}</div>}

      <h3>Members</h3>
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr>
              <th>Email</th>
              <th>Role</th>
              <th>Presence</th>
              <th>Key fingerprint</th>
              <th>Since</th>
              {owner && <th />}
            </tr>
          </thead>
          <tbody>
            {t.members.map((m) => (
              <MemberRow
                key={m.email}
                m={m}
                owner={owner}
                onOwner={(make) => run(() => api.setOwner(t.team, m.email, make))}
                onReassign={async () => {
                  setRemoving(null)
                  setReassigning({ email: m.email, docs: null })
                  try {
                    setReassigning({ email: m.email, docs: await api.removalPreview(t.team, m.email) })
                  } catch (e) {
                    setReassigning(null)
                    setErr((e as Error).message)
                  }
                }}
                onRemove={async () => {
                  setReassigning(null)
                  setRemoving({ email: m.email, docs: null })
                  try {
                    setRemoving({ email: m.email, docs: await api.removalPreview(t.team, m.email) })
                  } catch (e) {
                    setRemoving(null)
                    setErr((e as Error).message)
                  }
                }}
              />
            ))}
          </tbody>
        </table>
      </div>
      {removing && (
        <div className="banner warn">
          <b>Remove {removing.email} from {t.name}?</b>{' '}
          {removing.docs === null
            ? 'Looking for their documents…'
            : removing.docs.length === 0
              ? 'None of the team’s published documents in your folder are theirs.'
              : `Their ${plural(removing.docs.length, 'published document')} in your folder will be reassigned to you:`}
          {removing.docs && removing.docs.length > 0 && (
            <ul>
              {removing.docs.map((d) => (
                <li key={d.kalka_id}>
                  {d.title} <span className="muted small">({d.domain ? `domain ${d.domain}, ` : ''}{d.after > 0 ? `released up to v${d.after}` : 'not released yet'})</span>
                </li>
              ))}
            </ul>
          )}
          <div className="row">
            <button
              type="button"
              className="danger"
              {...act('team.remove')}
              disabled={removing.docs === null}
              onClick={() => {
                const email = removing.email
                setRemoving(null)
                run(() => api.removeMember(t.team, email))
              }}
            >
              Remove
            </button>
            <button type="button" onClick={() => setRemoving(null)}>
              Cancel
            </button>
          </div>
        </div>
      )}

      {reassigning && (
        <ReassignPanel
          t={t}
          self={self}
          email={reassigning.email}
          docs={reassigning.docs}
          run={run}
          onClose={() => setReassigning(null)}
          onDone={(kalkaID) => setReassigning((r) => r && { ...r, docs: r.docs?.filter((d) => d.kalka_id !== kalkaID) ?? null })}
        />
      )}

      {owner && <Invites t={t} run={run} />}

      {t.me !== 'none' && <Transfers team={t.team} reloadKey={reloadKey} />}

      {(t.not_shared?.length ?? 0) > 0 && (
        <>
          <h3>Not shared</h3>
          <ul className="plain">
            {t.not_shared!.map((d) => (
              <li key={d.rel}>
                <code>{d.rel}</code> · {d.reason}
              </li>
            ))}
          </ul>
        </>
      )}

      {(t.deleted?.length ?? 0) > 0 && (
        <>
          <h3>Deleted for the team</h3>
          <ul className="plain">
            {t.deleted!.map((d) => (
              <li key={d.kalka_id}>
                {d.title || d.kalka_id} · deleted by {d.by === self ? 'you' : d.by} at version {d.version}
              </li>
            ))}
          </ul>
          <p className="muted small">The last full copy of each is in the team folder's .kalka/trash.</p>
        </>
      )}

      <h3>History</h3>
      <ol className="team-log">
        {t.history.map((h) => (
          <li key={h.hash} className={h.effective ? '' : 'void'}>
            <span className="muted small">{h.at ? localTime(h.at) : ''}</span> <b>{h.signer === self ? 'You' : h.signer}</b> {h.text}
            {!h.effective && <span className="cv-flag"> does not count: {h.reason}</span>}
          </li>
        ))}
        {t.bad_lines?.map((b) => (
          <li key={'bad' + b.line} className="void">
            line {b.line}: <span className="cv-flag">{b.error}</span>
          </li>
        ))}
      </ol>
    </section>
  )
}

function MemberRow({
  m,
  owner,
  onOwner,
  onReassign,
  onRemove,
}: {
  m: TeamMember
  owner: boolean
  onOwner: (make: boolean) => void
  onReassign: () => void
  onRemove: () => void
}) {
  return (
    <tr>
      <td>
        {m.email}
        {m.you && <span className="muted small"> (you)</span>}
      </td>
      <td>{m.owner ? 'owner' : 'member'}</td>
      <td className="small">{m.you ? '' : m.presence === 'online' ? <span className="presence on">online</span> : m.presence === 'offline' ? 'offline' : <span className="muted">not seen yet</span>}</td>
      <td>
        <span className="mono small">{m.fingerprint}</span>
      </td>
      <td className="small">{m.since ? localTime(m.since) : ''}</td>
      {owner && (
        <td className="actions">
          {!m.you && (
            <>
              <button type="button" className="link" {...act('team.owner')} onClick={() => onOwner(!m.owner)}>
                {m.owner ? 'Remove as owner' : 'Make owner'}
              </button>{' '}
              ·{' '}
              <button type="button" className="link" onClick={onReassign}>
                Reassign…
              </button>{' '}
              ·{' '}
              <button type="button" className="link" onClick={onRemove}>
                Remove
              </button>
            </>
          )}
        </td>
      )}
    </tr>
  )
}

// ReassignPanel lists a member's published documents in the team, each
// with a new owner to choose (spec 7.4: a team owner reassigns without the
// owner's consent).
function ReassignPanel({
  t,
  self,
  email,
  docs,
  run,
  onClose,
  onDone,
}: {
  t: Team
  self: string
  email: string
  docs: Reassigned[] | null
  run: (f: () => Promise<unknown>) => Promise<void>
  onClose: () => void
  onDone: (kalkaID: string) => void
}) {
  const others = t.members.filter((m) => m.email !== email)
  const first = others.find((m) => m.email === self)?.email ?? others[0]?.email ?? ''
  const [to, setTo] = useState<Record<string, string>>({})
  return (
    <div className="banner">
      <b>Reassign {email}’s documents</b>{' '}
      {docs === null
        ? 'Looking for their documents…'
        : docs.length === 0
          ? 'None of the team’s published documents in your folder are theirs.'
          : 'The new owner’s kalka releases the next version; edits the old owner has not released are moved to their team trash, with a notice.'}
      {docs && docs.length > 0 && (
        <ul className="plain">
          {docs.map((d) => {
            const target = to[d.kalka_id] ?? first
            return (
              <li key={d.kalka_id} className="row">
                <span className="grow">
                  {d.title} <span className="muted small">({d.domain ? `domain ${d.domain}, ` : ''}{d.after > 0 ? `released up to v${d.after}` : 'not released yet'})</span>
                </span>
                <select value={target} onChange={(e) => setTo({ ...to, [d.kalka_id]: e.target.value })} aria-label={`New owner of ${d.title}`}>
                  {others.map((m) => (
                    <option key={m.email} value={m.email}>
                      {m.email === self ? 'you' : m.email}
                    </option>
                  ))}
                </select>
                <button
                  type="button"
                  {...act('doc.reassign')}
                  disabled={!target}
                  onClick={() =>
                    run(async () => {
                      await api.reassign(d.path, target)
                      onDone(d.kalka_id)
                    })
                  }
                >
                  Reassign
                </button>
              </li>
            )
          })}
        </ul>
      )}
      <div className="row">
        <button type="button" onClick={onClose}>
          Done
        </button>
      </div>
    </div>
  )
}

const EXPIRIES = [
  ['24h', '1 day'],
  ['72h', '3 days'],
  ['168h', '7 days'],
]

function Invites({ t, run }: { t: Team; run: (f: () => Promise<unknown>) => Promise<void> }) {
  const [email, setEmail] = useState('')
  const [expires, setExpires] = useState('72h')
  const [made, setMade] = useState<InviteResult | null>(null)
  const [copied, setCopied] = useState<'' | 'link' | 'token'>('')
  const [err, setErr] = useState('')
  const open = t.invites.filter((i) => i.state === 'open')
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setErr('')
    setCopied('')
    try {
      setMade(await api.invite(t.team, email.trim(), expires))
      setEmail('')
      run(async () => {})
    } catch (e) {
      setErr((e as Error).message)
    }
  }
  return (
    <>
      <h3>Invites</h3>
      <form className="row" onSubmit={submit}>
        <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="new.member@library.example" aria-label="Email to invite" />
        <select value={expires} onChange={(e) => setExpires(e.target.value)} aria-label="Valid for">
          {EXPIRIES.map(([v, l]) => (
            <option key={v} value={v}>
              valid {l}
            </option>
          ))}
        </select>
        <button type="submit" {...act('team.invite')} disabled={!email.trim()}>
          Invite
        </button>
      </form>
      {err && <div className="error-box">{err}</div>}
      {made && (
        <div className="token-box">
          <p className="small">
            Send the link to <b>{made.email}</b> through any channel; if their chat tool does not make it clickable, send the token. It
            works once, only for them, until {localTime(made.expires)}, and only while your kalka is online. They open the link, or paste
            either under “Join with link or token”.
          </p>
          <textarea className="token-input mono" readOnly rows={3} value={made.link} aria-label="Invite link" onFocus={(e) => e.target.select()} />
          <div className="row">
            {(['link', 'token'] as const).map((k) => (
              <button
                key={k}
                type="button"
                onClick={() =>
                  navigator.clipboard?.writeText(k === 'link' ? made.link : made.token).then(
                    () => setCopied(k),
                    () => setCopied(''),
                  )
                }
              >
                {copied === k ? 'Copied' : k === 'link' ? 'Copy link' : 'Copy token'}
              </button>
            ))}
            <button type="button" className="link" onClick={() => setMade(null)}>
              Done
            </button>
          </div>
        </div>
      )}
      {t.invites.length > 0 && (
        <ul className="plain small">
          {t.invites
            .slice()
            .reverse()
            .map((i) => (
              <li key={i.id}>
                {i.email} · {i.state === 'open' ? `open until ${localTime(i.expires)}` : i.state === 'used' ? `joined ${i.used_at ? localTime(i.used_at) : ''}` : i.state}
                {i.state === 'open' && i.token && i.link && (
                  <>
                    {' '}
                    ·{' '}
                    <button
                      type="button"
                      className="link"
                      onClick={() => {
                        setCopied('')
                        setMade({ token: i.token!, link: i.link!, id: i.id, team: t.team, name: t.name, email: i.email, expires: i.expires })
                      }}
                    >
                      Show link
                    </button>
                  </>
                )}
                {i.state === 'open' && (
                  <>
                    {' '}
                    ·{' '}
                    <button type="button" className="link" {...act('team.uninvite')} onClick={() => run(() => api.cancelInvite(i.id))}>
                      Cancel
                    </button>
                  </>
                )}
              </li>
            ))}
        </ul>
      )}
      {open.length === 0 && t.invites.length === 0 && <p className="muted small">No invites made on this installation.</p>}
    </>
  )
}

// Replication is the team's status line (spec 11.1, 20.3): the last round,
// who was reached, what was refused and why, and Sync now.
function Replication({ t, self, run }: { t: Team; self: string; run: (f: () => Promise<unknown>) => Promise<void> }) {
  const [busy, setBusy] = useState(false)
  const r = t.replication
  const reached = r?.reached ?? []
  const refused = r?.refused ?? []
  const others = t.members.filter((m) => m.email !== self).length
  return (
    <div className="replication">
      <p className="small">
        {plural(t.published, 'published document')} held here.{' '}
        {others === 0 ? (
          'No other members to replicate with yet.'
        ) : !r?.last_round ? (
          'No replication round yet.'
        ) : (
          <>
            Last round {localTime(r.last_round)}: reached {reached.length > 0 ? reached.join(', ') : 'nobody'}
            {r.tried > reached.length ? ` (${r.tried - reached.length} not online)` : ''}
            {r.received > 0 ? `, ${plural(r.received, 'document')} received` : ''}.
          </>
        )}{' '}
        {others > 0 && (
          <button
            type="button"
            className="link"
            {...act('team.sync')}
            disabled={busy}
            onClick={async () => {
              setBusy(true)
              await run(() => api.replicate(t.team))
              setBusy(false)
            }}
          >
            {busy ? 'Syncing…' : 'Sync now'}
          </button>
        )}
      </p>
      {r?.error && <p className="error-text small">{r.error}</p>}
      {refused.length > 0 && (
        <ul className="plain small">
          {refused.map((x, i) => (
            <li key={i}>
              <span className="cv-flag">refused</span> {x.title || x.kalka_id} from {x.from}: {x.reason}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

// DomainsPage lists the team domain documents held here (spec 6.12, 11.1):
// version, owner, open comments, kept versions.
export function DomainsPage({ team, reloadKey, list }: { team: string; reloadKey: number; list: BlueprintInfo[] }) {
  const [rows, setRows] = useState<DomainRow[] | null>(null)
  const [err, setErr] = useState('')
  useEffect(() => {
    api.domains(team).then(setRows, (e) => setErr((e as Error).message))
  }, [team, reloadKey, list])
  return (
    <div className="page">
      <h1>Domains</h1>
      <p className="muted">
        A domain document holds a team's shared language: glossary, contexts, invariants, events. Its owner releases its versions,
        like any document's; everyone else comments. Blueprints name the
        version they rely on with <code>kalka_domain: &lt;team&gt;/&lt;domain&gt;@v&lt;N&gt;</code>.
      </p>
      {err && <div className="error-box">{err}</div>}
      {rows && rows.length === 0 && (
        <p className="muted">
          No domain documents here yet. Write one with <code>kalka_kind: domain</code>, <code>kalka_team:</code> and <code>kalka_domain:</code> in its
          frontmatter, then release it.
        </p>
      )}
      {rows && rows.length > 0 && (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>Domain</th>
                <th>Version</th>
                <th>Owner</th>
                <th>Open comments</th>
                <th>Kept versions</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.path}>
                  <td>
                    <a href={docHref(r.rel)}>{r.title || r.domain}</a>
                    <div className="muted small mono">
                      {r.team}/{r.domain}
                    </div>
                  </td>
                  <td>
                    {r.version > 0 ? `v${r.version}` : 'draft'}
                    {r.version > 0 && r.unreleased && <span className="badge">unreleased edits</span>}
                  </td>
                  <td>{r.you ? 'you' : r.owner || <span className="muted">not assigned</span>}</td>
                  <td>{r.open}</td>
                  <td className="small">
                    {r.versions.length === 0
                      ? '—'
                      : r.versions.map((v, i) => (
                          <span key={v}>
                            {i > 0 && ' · '}
                            <a href={docHref(r.rel, v)}>v{v}</a>
                          </span>
                        ))}
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
