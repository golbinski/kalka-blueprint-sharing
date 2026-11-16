import { describe, expect, it } from 'vitest'
import type { Join, JoinPreview, Team, WaitingItem } from './api'
import {
  attestedBadge,
  attestedText,
  findTeam,
  joiningLines,
  joinState,
  resolveScope,
  scopeLabel,
  switcherRows,
  teamKey,
  transferEntries,
  waitingRows,
} from './scope'
import { parseRoute, scopeHref } from './util'

const team = (name: string, hash: string, waiting = 0): Team =>
  ({ team: hash, name, dir: name, log: '', me: 'member', waiting, members: [], owners: [], invites: [], history: [], published: 0, deleted: null, not_shared: null }) as Team

describe('routes with a scope', () => {
  it('parses a team and its views', () => {
    expect(parseRoute('#/t/lending-desk/')).toEqual({ view: 'list', scope: 'lending-desk' })
    expect(parseRoute('#/t/lending-desk')).toEqual({ view: 'list', scope: 'lending-desk' })
    expect(parseRoute('#/t/lending-desk/waiting')).toEqual({ view: 'waiting', scope: 'lending-desk' })
    expect(parseRoute('#/t/personal/seeds')).toEqual({ view: 'seeds', scope: 'personal' })
    expect(parseRoute('#/t/parcels/team')).toEqual({ view: 'team', scope: 'parcels' })
    expect(parseRoute('#/t/parcels/domains')).toEqual({ view: 'domains', scope: 'parcels' })
  })
  it('falls back to the list, and to the remembered scope', () => {
    expect(parseRoute('#/t/parcels/nowhere')).toEqual({ view: 'list', scope: 'parcels' })
    expect(parseRoute('#/')).toEqual({ view: 'list', scope: '' })
    expect(parseRoute('#/team')).toEqual({ view: 'team', scope: '' })
    expect(parseRoute('#/t/%E0%A4%A/waiting')).toEqual({ view: 'waiting', scope: '' })
  })
  it('opens the route the tray names', () => {
    // core.WaitingRoute
    expect(parseRoute('#/t/parcels/waiting')).toEqual({ view: 'waiting', scope: 'parcels' })
  })
  it('round-trips', () => {
    for (const s of ['personal', 'lending desk', 'a/b']) {
      expect(parseRoute(scopeHref(s, 'waiting'))).toEqual({ view: 'waiting', scope: s })
      expect(parseRoute(scopeHref(s))).toEqual({ view: 'list', scope: s })
    }
  })
  it('keeps the unscoped views', () => {
    expect(parseRoute('#/settings')).toEqual({ view: 'settings' })
    expect(parseRoute('#/doc/lending%2Frenewals.md')).toEqual({ view: 'doc', path: 'lending/renewals.md', version: undefined })
  })
})

describe('the scope shown', () => {
  const teams = [team('returns', 'bbbb'), team('lending', 'aaaa')]
  it('takes the route, then what was remembered, then the first team by name', () => {
    expect(resolveScope('returns', 'lending', teams)).toBe('returns')
    expect(resolveScope('', 'lending', teams)).toBe('lending')
    expect(resolveScope('', 'personal', teams)).toBe('personal')
    expect(resolveScope('', 'gone', teams)).toBe('lending')
    expect(resolveScope('', '', teams)).toBe('lending')
    expect(resolveScope('', '', [])).toBe('personal')
  })
  it('waits for the teams before choosing', () => {
    expect(resolveScope('', '', null)).toBe('')
    expect(resolveScope('', 'lending', null)).toBe('lending')
  })
  it('finds a team by name or hash', () => {
    expect(findTeam('aaaa', teams)?.name).toBe('lending')
    expect(findTeam('returns', teams)?.team).toBe('bbbb')
    expect(findTeam('none', teams)).toBeUndefined()
  })
})

describe('switcher', () => {
  it('lists Personal first, then the teams by name with their badges', () => {
    const rows = switcherRows([team('returns', 'bbbb', 2), team('lending', 'aaaa')], 'returns')
    expect(rows.map((r) => [r.label, r.waiting, r.current])).toEqual([
      ['Personal', 0, false],
      ['lending', 0, false],
      ['returns', 2, true],
    ])
    expect(rows[2].href).toBe('#/t/returns/')
  })
  it('shows a short hash when two teams share a name, and routes by it', () => {
    const teams = [team('lending', 'aaaa1111aaaa1111'), team('lending', 'bbbb2222bbbb2222'), team('parcels', 'cccc')]
    const rows = switcherRows(teams, 'bbbb2222bbbb2222')
    expect(rows.slice(1).map((r) => [r.label, r.hash, r.key, r.current])).toEqual([
      ['lending', 'aaaa1111', 'aaaa1111aaaa1111', false],
      ['lending', 'bbbb2222', 'bbbb2222bbbb2222', true],
      ['parcels', undefined, 'parcels', false],
    ])
    expect(teamKey(teams[2], teams)).toBe('parcels')
    expect(scopeLabel('bbbb2222bbbb2222', teams)).toBe('lending')
    expect(scopeLabel('personal', teams)).toBe('Personal')
  })
  it('lists the pending joins', () => {
    const joins = [
      { id: '1', name: 'returns', state: 'pending' },
      { id: '2', name: 'lending', state: 'joined' },
    ] as Join[]
    expect(joiningLines(joins)).toEqual(['joining returns…'])
  })
})

describe('waiting list', () => {
  const item = (w: Partial<WaitingItem>): WaitingItem =>
    ({ kind: 'handed', team: 'h', team_name: 'parcels', at: '2026-03-01T10:00:00Z', target: '#/t/parcels/waiting', title: 'Lockers', message: '', ...w }) as WaitingItem
  it('gives each kind its buttons', () => {
    const rows = waitingRows([
      item({ kind: 'transfer', transfer: 't1', by: 'olga@library.example', message: 'Renewals offered by lending' }),
      item({ kind: 'handed', kalka_id: 'k1', target: '#/doc/parcels/lockers.md', path: '/f/parcels/lockers.md' }),
      item({ kind: 'disputed', kalka_id: 'k2', target: '#/doc/parcels/routes.md', path: '/f/parcels/routes.md' }),
      item({ kind: 'owner-gone', kalka_id: 'k3', target: '#/doc/parcels/vans.md', path: '/f/parcels/vans.md' }),
    ])
    expect(rows.map((r) => [r.kind, r.buttons, r.href])).toEqual([
      ['Transfer', ['accept', 'decline'], undefined],
      ['Yours now', ['open'], '#/doc/parcels/lockers.md'],
      ['Ownership disputed', ['open', 'reassign'], '#/doc/parcels/routes.md'],
      ['Owner left', ['open', 'reassign'], '#/doc/parcels/vans.md'],
    ])
    expect(rows[0].text).toBe('Renewals offered by lending')
    expect(rows[1].text).toBe('Lockers')
    expect(new Set(rows.map((r) => r.key)).size).toBe(4)
  })
  it('keeps the order it is given, oldest first', () => {
    const rows = waitingRows([item({ kalka_id: 'b', at: '2026-01-01T00:00:00Z' }), item({ kalka_id: 'a', at: '2026-02-01T00:00:00Z' })])
    expect(rows.map((r) => r.item.kalka_id)).toEqual(['b', 'a'])
  })
})

describe('join screen', () => {
  const p = (w: Partial<JoinPreview>): JoinPreview =>
    ({ token: 'T', team: 'h', name: 'returns', inviter: 'olga@library.example', email: 'ivo@library.example', expires: '', rendezvous: '', ...w }) as JoinPreview
  it('shows one of four states', () => {
    expect(joinState(p({}))).toBe('ready')
    expect(joinState(p({ problem: 'invite-expired', message: 'expired' }))).toBe('problem')
    expect(joinState(p({ joining: { state: 'pending' } as Join }))).toBe('joining')
    expect(joinState(p({ problem: 'already-member', joining: { state: 'joined' } as Join }))).toBe('joined')
    // A failed join may be tried again.
    expect(joinState(p({ joining: { state: 'failed' } as Join }))).toBe('ready')
  })
})

describe('attested history', () => {
  it('says where it came from and who vouches for it', () => {
    expect(attestedText({ mode: 'copy', from: 'lending/0b5e', actor: 'olga@library.example', by: 'ivo@library.example' })).toBe(
      'Copied from lending/0b5e. Its history is attested by olga@library.example, who offered it, and ivo@library.example, who accepted it.',
    )
    expect(attestedText({ mode: 'move', from: 'lending/0b5e' })).toBe('Moved from lending/0b5e.')
    expect(attestedText({ moving_to: 'returns' })).toBe('')
  })
  it('names the author on the badge', () => {
    expect(attestedBadge('olga@library.example')).toContain('olga@library.example')
  })
  it('counts what an offer brings', () => {
    expect(transferEntries(0)).toBe('no review entries')
    expect(transferEntries(1)).toBe('1 attested entry')
    expect(transferEntries(3)).toBe('3 attested entries')
  })
})
