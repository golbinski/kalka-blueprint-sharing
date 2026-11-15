// Team view (spec 11.1): members, owners, domain owners, open invites and
// the log as a readable history; Join, Invite, Remove (with a preview of
// the documents to be reassigned), Add owner, Assign domain.
import { useCallback, useEffect, useState } from 'react'
import { api, type BlueprintInfo, type DomainRow, type InviteResult, type Join, type Reassigned, type Team, type TeamMember } from './api'
import { docHref, localTime, plural } from './util'

export function TeamPage({ self, reloadKey }: { self: string; reloadKey: number }) {
  const [teams, setTeams] = useState<Team[] | null>(null)
  const [joins, setJoins] = useState<Join[]>([])
  const [err, setErr] = useState('')
  const load = useCallback(() => {
    api.teams().then(setTeams, (e: Error) => setErr(e.message))
    api.joins().then(setJoins, () => {})
  }, [])
  useEffect(() => {
    load()
  }, [load, reloadKey])
  const open = joins.filter((j) => j.state !== 'joined' || Date.now() - new Date(j.last_try ?? j.created).getTime() < 600_000)
  return (
    <div className="page">
      <h1>Team</h1>
      <p className="muted">
        A team is a signed log of who belongs and who may change the roster. It lives in your folder next to the team's
        documents. Members trust each other's keys through it, so they never compare codes.
      </p>
      {err && <div className="error-box">{err}</div>}
      {open.length > 0 && <Joins joins={open} />}
      {teams?.map((t) => <TeamCard key={t.team} t={t} self={self} reload={load} />)}
      <div className="team-start">
        <JoinForm onDone={load} />
        {teams && (teams.length === 0 || teams.every((t) => t.me !== 'none')) && <CreateForm onDone={load} first={teams.length === 0} />}
      </div>
    </div>
  )
}

function Joins({ joins }: { joins: Join[] }) {
  return (
    <section className="team-joins">
      {joins.map((j) => (
        <div key={j.id} className={'banner ' + (j.state === 'failed' ? 'danger' : j.state === 'joined' ? 'notice' : 'warn')}>
          {j.state === 'pending' && (
            <>
              <b>Joining {j.name}…</b> {j.detail || 'Looking for the team owner.'} The invite is valid until {localTime(j.expires)}
              {j.tries > 0 && <> · {plural(j.tries, 'attempt')}</>}.
            </>
          )}
          {j.state === 'joined' && (
            <>
              <b>You joined {j.name}</b>
              {j.owner && <> through {j.owner}</>}.
            </>
          )}
          {j.state === 'failed' && (
            <>
              <b>Joining {j.name} failed:</b> {j.detail}
            </>
          )}
        </div>
      ))}
    </section>
  )
}

function JoinForm({ onDone }: { onDone: () => void }) {
  const [token, setToken] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [warn, setWarn] = useState('')
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setErr('')
    setWarn('')
    try {
      const j = await api.join(token)
      setToken('')
      setWarn(j.warning ?? '')
      onDone()
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy(false)
  }
  return (
    <form className="team-box" onSubmit={submit}>
      <h2>Join a team</h2>
      <p className="muted small">
        Paste the invite a team owner sent you. kalka reaches their installation directly and keeps trying while they are
        offline, until the invite expires.
      </p>
      <textarea
        className="token-input mono"
        rows={3}
        value={token}
        onChange={(e) => setToken(e.target.value)}
        placeholder="Invite token"
        aria-label="Invite token"
        spellCheck={false}
      />
      <div>
        <button type="submit" className="primary" disabled={busy || !token.trim()}>
          {busy ? 'Joining…' : 'Join'}
        </button>
      </div>
      {warn && <div className="banner warn">{warn}</div>}
      {err && <div className="error-box">{err}</div>}
    </form>
  )
}

function CreateForm({ onDone, first }: { onDone: () => void; first: boolean }) {
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setErr('')
    try {
      await api.createTeam(name.trim())
      setName('')
      onDone()
    } catch (e) {
      setErr((e as Error).message)
    }
    setBusy(false)
  }
  return (
    <form className="team-box" onSubmit={submit}>
      <h2>{first ? 'Create a team' : 'Create another team'}</h2>
      <p className="muted small">You become its first owner. The name is used for the team's folder: lower-case letters, digits and dashes.</p>
      <div className="row">
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="lending-desk" aria-label="Team name" pattern="[a-z0-9][a-z0-9-]*" />
        <button type="submit" disabled={busy || !name.trim()}>
          Create
        </button>
      </div>
      {err && <div className="error-box">{err}</div>}
    </form>
  )
}

function TeamCard({ t, self, reload }: { t: Team; self: string; reload: () => void }) {
  const owner = t.me === 'owner'
  const [err, setErr] = useState('')
  const [removing, setRemoving] = useState<{ email: string; docs: Reassigned[] | null } | null>(null)
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
        <h2>{t.name}</h2>
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
                onRemove={async () => {
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
                  {d.title} <span className="muted small">({d.after > 0 ? `released up to v${d.after}` : 'not released yet'})</span>
                </li>
              ))}
            </ul>
          )}
          <div className="row">
            <button
              type="button"
              className="danger"
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

      <h3>Domains</h3>
      {t.domains.length === 0 ? (
        <p className="muted small">No domain owners yet.</p>
      ) : (
        <ul className="plain">
          {t.domains.map((d) => (
            <li key={d.domain}>
              <span className="mono">{d.domain}</span> · {d.owner === self ? 'you' : d.owner} · from version {d.after + 1}
            </li>
          ))}
        </ul>
      )}
      {owner && <AssignForm t={t} run={run} />}

      {owner && <Invites t={t} run={run} />}

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

function MemberRow({ m, owner, onOwner, onRemove }: { m: TeamMember; owner: boolean; onOwner: (make: boolean) => void; onRemove: () => void }) {
  return (
    <tr>
      <td>
        {m.email}
        {m.you && <span className="muted small"> (you)</span>}
      </td>
      <td>{m.owner ? 'owner' : 'member'}</td>
      <td>
        <span className="mono small">{m.fingerprint}</span>
      </td>
      <td className="small">{m.since ? localTime(m.since) : ''}</td>
      {owner && (
        <td className="actions">
          {!m.you && (
            <>
              <button type="button" className="link" onClick={() => onOwner(!m.owner)}>
                {m.owner ? 'Make member' : 'Make owner'}
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

function AssignForm({ t, run }: { t: Team; run: (f: () => Promise<unknown>) => Promise<void> }) {
  const [domain, setDomain] = useState('')
  const [email, setEmail] = useState(t.members[0]?.email ?? '')
  return (
    <form
      className="row"
      onSubmit={(e) => {
        e.preventDefault()
        run(() => api.assignDomain(t.team, domain.trim(), email)).then(() => setDomain(''))
      }}
    >
      <input value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="domain, e.g. loans" aria-label="Domain" />
      <select value={email} onChange={(e) => setEmail(e.target.value)} aria-label="Domain owner">
        {t.members.map((m) => (
          <option key={m.email} value={m.email}>
            {m.email}
          </option>
        ))}
      </select>
      <button type="submit" disabled={!domain.trim() || !email}>
        Assign domain
      </button>
    </form>
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
  const [copied, setCopied] = useState(false)
  const [err, setErr] = useState('')
  const open = t.invites.filter((i) => i.state === 'open')
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    setErr('')
    setCopied(false)
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
        <button type="submit" disabled={!email.trim()}>
          Invite
        </button>
      </form>
      {err && <div className="error-box">{err}</div>}
      {made && (
        <div className="token-box">
          <p className="small">
            Send this to <b>{made.email}</b> through any channel. It works once, only for them, until {localTime(made.expires)}, and only
            while your kalka is online. They paste it under “Join a team”.
          </p>
          <textarea className="token-input mono" readOnly rows={3} value={made.token} aria-label="Invite token" onFocus={(e) => e.target.select()} />
          <div className="row">
            <button
              type="button"
              onClick={() =>
                navigator.clipboard?.writeText(made.token).then(
                  () => setCopied(true),
                  () => setCopied(false),
                )
              }
            >
              {copied ? 'Copied' : 'Copy'}
            </button>
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
                {i.state === 'open' && (
                  <>
                    {' '}
                    ·{' '}
                    <button type="button" className="link" onClick={() => run(() => api.cancelInvite(i.id))}>
                      Withdraw
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
// who was reached, what was refused and why, and Replicate now.
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
            disabled={busy}
            onClick={async () => {
              setBusy(true)
              await run(() => api.replicate(t.team))
              setBusy(false)
            }}
          >
            {busy ? 'Replicating…' : 'Replicate now'}
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
// version, domain owner, open comments, kept versions.
export function DomainsPage({ reloadKey, list }: { reloadKey: number; list: BlueprintInfo[] }) {
  const [rows, setRows] = useState<DomainRow[] | null>(null)
  const [err, setErr] = useState('')
  useEffect(() => {
    api.domains().then(setRows, (e) => setErr((e as Error).message))
  }, [reloadKey, list])
  return (
    <div className="page">
      <h1>Domains</h1>
      <p className="muted">
        A domain document holds a team's shared language: glossary, contexts, invariants, events. Its domain owner, assigned by a
        team owner on the <a href="#/team">Team</a> page, releases its versions; everyone else comments. Blueprints name the
        version they rely on with <code>domain: &lt;team&gt;/&lt;domain&gt;@v&lt;N&gt;</code>.
      </p>
      {err && <div className="error-box">{err}</div>}
      {rows && rows.length === 0 && (
        <p className="muted">
          No domain documents here yet. Write one with <code>kind: domain</code>, <code>team:</code> and <code>domain:</code> in its
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
                <th>Domain owner</th>
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
