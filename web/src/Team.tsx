// Team view (spec 11.1): the team's sync in words with Sync now, Invite
// a teammate, the members with when they were last seen, what they own
// and a ⋯ menu (Make team owner, Give their blueprints to someone,
// Remove), transfers, and folded the history and the technical details.
// Creating and joining teams is in the switcher.
import { useCallback, useEffect, useState } from 'react'
import { act } from './actions'
import { api, type BlueprintInfo, type DomainRow, type InviteResult, type Reassigned, type Team, type TeamMember } from './api'
import { Menu } from './Menu'
import { findTeam, shortName } from './scope'
import { Transfers } from './Transfer'
import { ago, docHref, localTime, plural, shortTime, warningOf } from './util'
import { OpenFolder } from './Views'

export function TeamPage({ self, team, reloadKey, list }: { self: string; team: string; reloadKey: number; list: BlueprintInfo[] }) {
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
      {err && <div className="error-box">{err}</div>}
      {teams && !t && (
        <>
          <h1>Team</h1>
          <div className="empty">No team “{team}” on this computer. Choose one in the switcher, or create or join one there.</div>
        </>
      )}
      {t && <TeamCard key={t.team} t={t} self={self} reload={load} reloadKey={reloadKey} list={list} />}
    </div>
  )
}

function TeamCard({ t, self, reload, reloadKey, list }: { t: Team; self: string; reload: () => void; reloadKey: number; list: BlueprintInfo[] }) {
  const owner = t.me === 'owner'
  const [err, setErr] = useState('')
  const [removing, setRemoving] = useState<{ email: string; docs: Reassigned[] | null } | null>(null)
  const [reassigning, setReassigning] = useState<{ email: string; docs: Reassigned[] | null } | null>(null)
  const [ownerChange, setOwnerChange] = useState<{ email: string; make: boolean } | null>(null)
  const [warn, setWarn] = useState('')
  const run = async (f: () => Promise<unknown>) => {
    setErr('')
    setWarn('')
    try {
      setWarn(warningOf(await f()))
      reload()
    } catch (e) {
      setErr((e as Error).message)
    }
  }
  const owns = new Map<string, number>()
  for (const b of list) if (b.owner && b.version) owns.set(b.owner.toLowerCase(), (owns.get(b.owner.toLowerCase()) ?? 0) + 1)
  const ownsOf = (email: string) => owns.get(email.toLowerCase()) ?? 0
  const preview = async (email: string, set: typeof setRemoving) => {
    set({ email, docs: null })
    try {
      set({ email, docs: await api.removalPreview(t.team, email) })
    } catch (e) {
      set(null)
      setErr((e as Error).message)
    }
  }
  const notCounting = (t.bad_lines?.length ?? 0) + (t.rejected ?? 0)
  const voids = t.history.filter((h) => !h.effective).length + (t.bad_lines?.length ?? 0)
  return (
    <section className="team-card">
      <div className="team-title">
        <h1>
          {t.name}{' '}
          <span className="muted small">· {t.me === 'owner' ? 'you are a team owner' : t.me === 'member' ? 'you are a member' : 'you are not a member'}</span>
        </h1>
        <span className="grow" />
        {t.me !== 'none' && <SyncLine t={t} self={self} run={run} />}
      </div>
      {t.error && <div className="error-box">{t.error}</div>}
      <Refusals t={t} />
      {notCounting > 0 && (
        <div className="banner warn">
          {plural(notCounting, 'change')} in the team's history {notCounting === 1 ? 'does' : 'do'} not count: damaged, or not signed by the key
          {notCounting === 1 ? ' it names' : ' they name'}. They are listed under Team history.
        </div>
      )}
      {err && <div className="error-box">{err}</div>}
      {warn && <div className="warn-box">{warn}</div>}

      {owner && <Invites t={t} run={run} />}

      <h2>
        Members <span className="muted">· {t.members.length}</span>
      </h2>
      <div className="table-wrap">
        <table className="table members">
          <thead>
            <tr>
              <th>Member</th>
              <th>Role</th>
              <th>Last seen</th>
              <th>Owns</th>
              {owner && <th aria-label="Actions" />}
            </tr>
          </thead>
          <tbody>
            {t.members.map((m) => (
              <tr key={m.email}>
                <td>
                  {m.email}
                  {m.you && <span className="muted small"> (you)</span>}
                </td>
                <td>{m.owner ? 'team owner' : 'member'}</td>
                <td className="small">{lastSeen(m)}</td>
                <td>{ownsOf(m.email) > 0 ? plural(ownsOf(m.email), 'blueprint') : <span className="muted">—</span>}</td>
                {owner && (
                  <td className="actions">
                    {!m.you && (
                      <Menu
                        label="⋯"
                        title={`Actions for ${m.email}`}
                        items={[
                          {
                            label: m.owner ? 'Remove as team owner…' : 'Make team owner…',
                            sub: m.owner ? 'they stay a member' : 'they can invite, remove and change any owner',
                            onSelect: () => {
                              setRemoving(null)
                              setReassigning(null)
                              setOwnerChange({ email: m.email, make: !m.owner })
                            },
                          },
                          {
                            label: 'Give their blueprints to someone…',
                            sub: ownsOf(m.email) > 0 ? plural(ownsOf(m.email), 'blueprint') : 'none on this computer',
                            onSelect: () => {
                              setRemoving(null)
                              setOwnerChange(null)
                              preview(m.email, setReassigning)
                            },
                          },
                          {
                            label: 'Remove from team…',
                            sub: ownsOf(m.email) > 0 ? `their ${plural(ownsOf(m.email), 'blueprint')} ${ownsOf(m.email) === 1 ? 'becomes' : 'become'} yours` : undefined,
                            danger: true,
                            sep: true,
                            onSelect: () => {
                              setReassigning(null)
                              setOwnerChange(null)
                              preview(m.email, setRemoving)
                            },
                          },
                        ]}
                      />
                    )}
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {ownerChange && (
        <div className="confirm-box warn">
          <b>
            {ownerChange.make ? `Make ${ownerChange.email} a team owner of ${t.name}?` : `Remove ${ownerChange.email} as a team owner of ${t.name}?`}
          </b>{' '}
          {ownerChange.make
            ? 'They will be able to invite and remove members and give any blueprint a new owner. This is recorded in the team’s history and cannot be erased.'
            : 'They stay a member. What they did as a team owner stays. This is recorded in the team’s history.'}
          <div className="row">
            <button
              type="button"
              className={ownerChange.make ? 'primary' : 'danger'}
              {...act('team.owner')}
              onClick={() => {
                const o = ownerChange
                setOwnerChange(null)
                run(() => api.setOwner(t.team, o.email, o.make))
              }}
            >
              {ownerChange.make ? 'Make team owner' : 'Remove as team owner'}
            </button>
            <button type="button" onClick={() => setOwnerChange(null)}>
              Cancel
            </button>
          </div>
        </div>
      )}
      {removing && (
        <div className="confirm-box warn">
          <b>Remove {removing.email} from {t.name}?</b>{' '}
          {removing.docs === null
            ? 'Looking for their blueprints…'
            : removing.docs.length === 0
              ? 'None of the team’s blueprints on this computer are theirs.'
              : `Their ${plural(removing.docs.length, 'blueprint')} on this computer will become yours:`}
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
              Remove from team
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

      {t.me !== 'none' && <Transfers team={t.team} reloadKey={reloadKey} />}

      <details className="fold">
        <summary>
          <b>Team history</b>{' '}
          <span className="muted small">
            · {plural(t.history.length - voids + (t.bad_lines?.length ?? 0), 'change')}
            {voids > 0 && `, ${voids} ignored`}
          </span>
        </summary>
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
      </details>

      <details className="fold">
        <summary>
          <b>Technical details</b> <span className="muted small">· team id, folder, key fingerprints, files not shared, deleted blueprints</span>
        </summary>
        <dl className="facts">
          <dt>Team id</dt>
          <dd className="mono small">{t.team}</dd>
          <dt>Folder</dt>
          <dd className="small">
            {t.dir} <OpenFolder rel={t.name} />
          </dd>
          <dt>Blueprints</dt>
          <dd className="small">{plural(t.published, 'published blueprint')} on this computer</dd>
        </dl>
        <h3>Key fingerprints</h3>
        <p className="muted small">Members trust each other's keys through the team's signed history, so nobody has to compare these.</p>
        <ul className="plain small">
          {t.members.map((m) => (
            <li key={m.email}>
              {m.email} <span className="mono">{m.fingerprint}</span>
              {m.since && <span className="muted"> · member since {localTime(m.since)}</span>}
            </li>
          ))}
        </ul>
        {(t.not_shared?.length ?? 0) > 0 && (
          <>
            <h3>Not shared</h3>
            <ul className="plain small">
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
            <ul className="plain small">
              {t.deleted!.map((d) => (
                <li key={d.kalka_id}>
                  {d.title || d.kalka_id} · deleted by {d.by === self ? 'you' : d.by} at version {d.version}
                </li>
              ))}
            </ul>
            <p className="muted small">The last full copy of each is in the team folder's .kalka/trash.</p>
          </>
        )}
      </details>
    </section>
  )
}

function lastSeen(m: TeamMember): React.ReactNode {
  if (m.you) return ''
  if (m.presence === 'online') return <span className="presence on">online</span>
  if (m.last_seen) return ago(m.last_seen)
  if (m.presence === 'offline') return 'offline'
  return <span className="muted">not seen yet</span>
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
    <div className="confirm-box">
      <b>Give {shortName(email)}’s blueprints to someone</b>{' '}
      {docs === null
        ? 'Looking for their blueprints…'
        : docs.length === 0
          ? 'None of the team’s blueprints on this computer are theirs.'
          : `As a team owner you choose; ${shortName(email)} is not asked. The new owner releases the next version.`}
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
                      {m.email === self ? `you (${m.email})` : m.email}
                    </option>
                  ))}
                </select>
                <button
                  type="button"
                  {...act('doc.reassign')}
                  disabled={!target}
                  onClick={() =>
                    run(async () => {
                      const r = await api.reassign(d.path, target)
                      onDone(d.kalka_id)
                      return r
                    })
                  }
                >
                  Make owner
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

/** The releases page a new teammate installs kalka from. */
export const INSTALL_URL = 'https://github.com/golbinski/kalka-blueprint-sharing/releases/latest'

/** inviteMessage is what an owner pastes into a chat (spec 19.7): how to
 * install, the link, and the code for when the link is not clickable. */
export function inviteMessage(i: Pick<InviteResult, 'name' | 'link' | 'token' | 'email' | 'expires'>): string {
  return [
    `You are invited to the kalka team ${i.name}.`,
    `1. Install kalka: ${INSTALL_URL}`,
    `2. Open this link: ${i.link}`,
    `If the link does not open, start kalka, choose "Join with an invite…" and paste this code:`,
    i.token,
    `The invite works once, for ${i.email}, until ${localTime(i.expires)}, while my kalka is running.`,
  ].join('\n')
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
  const [copied, setCopied] = useState<'' | 'link' | 'token' | 'message'>('')
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
  const copy = (k: 'link' | 'token' | 'message', text: string) =>
    navigator.clipboard?.writeText(text).then(
      () => setCopied(k),
      () => setCopied(''),
    )
  return (
    <section className="invite">
      <h2>Invite a teammate</h2>
      <form className="row" onSubmit={submit}>
        <input className="grow" type="email" value={email} onChange={(e) => setEmail(e.target.value)} placeholder="teammate@library.example" aria-label="Email to invite" />
        <select value={expires} onChange={(e) => setExpires(e.target.value)} aria-label="Valid for">
          {EXPIRIES.map(([v, l]) => (
            <option key={v} value={v}>
              valid {l}
            </option>
          ))}
        </select>
        <button type="submit" className="primary" {...act('team.invite')} disabled={!email.trim()}>
          Create invite
        </button>
      </form>
      <p className="muted small">
        You get a message to paste into a chat. The invite works once, for that person, and only while your kalka is running.
        {open.map((i) => (
          <span key={i.id}>
            {' '}
            · Open invite: {i.email} until {localTime(i.expires)}{' '}
            {i.token && i.link && (
              <button
                type="button"
                className="link"
                onClick={() => {
                  setCopied('')
                  setMade({ token: i.token!, link: i.link!, id: i.id, team: t.team, name: t.name, email: i.email, expires: i.expires })
                }}
              >
                Show
              </button>
            )}{' '}
            ·{' '}
            <button type="button" className="link" {...act('team.uninvite')} onClick={() => run(() => api.cancelInvite(i.id))}>
              Cancel
            </button>
          </span>
        ))}
      </p>
      {err && <div className="error-box">{err}</div>}
      {made && (
        <div className="token-box">
          <p className="small">
            Paste the invite message into a chat with <b>{made.email}</b>: it says how to install kalka, and holds the link and the code.
            It works once, only for them, until {localTime(made.expires)}, and only while your kalka is running.
          </p>
          <pre className="invite-message small">{inviteMessage(made)}</pre>
          <div className="row">
            <button type="button" className="primary" onClick={() => copy('message', inviteMessage(made))}>
              {copied === 'message' ? 'Copied' : 'Copy invite message'}
            </button>
            <button type="button" onClick={() => copy('link', made.link)}>
              {copied === 'link' ? 'Copied' : 'Copy link'}
            </button>
            <button type="button" onClick={() => copy('token', made.token)}>
              {copied === 'token' ? 'Copied' : 'Copy code'}
            </button>
            <button type="button" className="link" onClick={() => setMade(null)}>
              Done
            </button>
          </div>
        </div>
      )}
    </section>
  )
}

// SyncLine is the team's sync in words (spec 11.1, 20.3): when kalka last
// synced, with whom, who was offline, and Sync now.
function SyncLine({ t, self, run }: { t: Team; self: string; run: (f: () => Promise<unknown>) => Promise<void> }) {
  const [busy, setBusy] = useState(false)
  const r = t.replication
  const others = t.members.filter((m) => m.email !== self && !m.you)
  const reached = new Set((r?.reached ?? []).map((x) => x.toLowerCase()))
  const with_ = others.filter((m) => reached.has(m.email.toLowerCase())).map((m) => shortName(m.email))
  const offline = others.filter((m) => !reached.has(m.email.toLowerCase())).map((m) => shortName(m.email))
  return (
    <div className="sync-line row">
      <span className="muted small">
        {others.length === 0
          ? 'Nobody to sync with yet: invite a teammate.'
          : !r?.last_round
            ? 'No sync yet.'
            : `Last sync ${shortTime(r.last_round)}${with_.length > 0 ? ` with ${with_.join(', ')}` : ', nobody online'}${offline.length > 0 && with_.length > 0 ? ` · ${offline.join(', ')} offline` : ''}`}
        {r?.error && <span className="error-text"> · {r.error}</span>}
      </span>
      {others.length > 0 && (
        <button
          type="button"
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
    </div>
  )
}

// Refusals says, in one sentence each, which blueprints the last sync
// could not take, and where to act on them.
function Refusals({ t }: { t: Team }) {
  const refused = t.replication?.refused ?? []
  if (refused.length === 0) return null
  return (
    <div className="banner warn">
      {plural(refused.length, 'blueprint')} could not be synced:{' '}
      {refused.map((x, i) => (
        <span key={i}>
          {i > 0 && '; '}“{x.title || x.kalka_id}” from {shortName(x.from)}: {x.reason}
        </span>
      ))}
      . <a href="#/needs">Needs you</a> lists what you can settle.
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
        Your team's shared vocabulary: a glossary, its contexts, rules and events, in a blueprint its owner releases in versions.
        Blueprints say which version they were written against, so a newer one never changes what an older plan meant.
      </p>
      {err && <div className="error-box">{err}</div>}
      {rows && rows.length === 0 && <DomainEmpty team={team} />}
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
                  <td title={r.owner}>{r.you ? 'you' : r.owner ? shortName(r.owner) : <span className="muted">no owner</span>}</td>
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

// DomainEmpty starts a domain: a blueprint in Personal with three lines
// on top, published to the team.
function DomainEmpty({ team }: { team: string }) {
  const [copied, setCopied] = useState(false)
  const head = `---\nkalka_kind: domain\nkalka_domain: lending\n---\n# Lending\n\n## Glossary\n\n- **Loan**: …\n`
  return (
    <div className="empty">
      <p>
        <b>No domains in {team} yet.</b> Start one as a blueprint in Personal that begins with these lines, then publish it to {team}.
        You become its owner.
      </p>
      <pre className="mono small">{head}</pre>
      <div className="row">
        <button type="button" onClick={() => navigator.clipboard?.writeText(head).then(() => setCopied(true))}>
          {copied ? 'Copied' : 'Copy the start'}
        </button>
        <OpenFolder rel="personal" label="Open Personal folder" />
      </div>
    </div>
  )
}
