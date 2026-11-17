// Picking a member instead of typing an address (spec 11.1): the team's
// members come from GET /api/teams.
import { useEffect, useState } from 'react'
import { api, type TeamMember } from './api'

/** useMembers is the members of a team, by its name or hash. */
export function useMembers(team: string | undefined): TeamMember[] | null {
  const [members, setMembers] = useState<TeamMember[] | null>(null)
  useEffect(() => {
    if (!team) return
    let live = true
    api.teams().then(
      (ts) => {
        const t = ts.find((x) => x.team === team) ?? ts.find((x) => x.name === team)
        if (live) setMembers(t?.members ?? [])
      },
      () => live && setMembers([]),
    )
    return () => {
      live = false
    }
  }, [team])
  return members
}

/** MemberSelect is a member picker. first lists some members on top (the
 * candidates of a dispute); exclude leaves some out (the current owner). */
export function MemberSelect({
  members,
  value,
  onChange,
  exclude = [],
  first = [],
  label,
}: {
  members: TeamMember[] | null
  value: string
  onChange: (email: string) => void
  exclude?: string[]
  first?: string[]
  label: string
}) {
  const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase()
  const list = (members ?? []).filter((m) => !exclude.some((x) => same(x, m.email)))
  list.sort((a, b) => Number(first.some((x) => same(x, b.email))) - Number(first.some((x) => same(x, a.email))))
  return (
    <select value={value} onChange={(e) => onChange(e.target.value)} aria-label={label} disabled={members === null}>
      <option value="">{members === null ? 'Loading members…' : 'Choose a member…'}</option>
      {list.map((m) => (
        <option key={m.email} value={m.email}>
          {m.you ? `you (${m.email})` : m.email}
          {first.some((x) => same(x, m.email)) ? ' · released it' : ''}
        </option>
      ))}
    </select>
  )
}
