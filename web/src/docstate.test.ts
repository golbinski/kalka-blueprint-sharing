import { describe, expect, it } from 'vitest'
import type { Doc, TeamDocState } from './api'
import { docState } from './docstate'

const doc = (team?: Partial<TeamDocState>, more: Partial<Doc> = {}): Doc =>
  ({
    path: '/f/lending/renewals.md',
    rel: 'lending/renewals.md',
    title: 'Renewals',
    role: team ? 'reviewer' : 'owner',
    self: 'ola@library.example',
    rev: '',
    sections: [],
    headings: [],
    entries: [],
    problems: [],
    hints: [],
    scope: 'lending',
    traces: [],
    refs: {},
    team: team && { team: 'lending', held: true, version: 2, releaser: 'jan@library.example', ...team },
    ...more,
  }) as Doc

describe('docState', () => {
  it('asks the owner to release unreleased edits', () => {
    const s = docState(doc({ releaser: 'ola@library.example', can_act: true, unreleased: true }))
    expect(s.head).toBe('You own this · version 2 is with lending')
    expect(s.sub).toMatch(/as version 3/)
    expect(s.primary).toBe('release')
  })
  it('tells a reviewer who owns it', () => {
    const s = docState(doc({}))
    expect(s.head).toBe('jan owns this · you are reviewing version 2')
    expect(s.primary).toBe('')
  })
  it('offers Publish for a blueprint not shared yet', () => {
    expect(docState(doc(undefined, { scope: 'personal' }))).toMatchObject({ head: 'Yours · only on this computer', primary: 'publish' })
    expect(docState(doc(undefined, { problems: [{ severity: 'error', message: 'x' }] as Doc['problems'] })).primary).toBe('')
  })
  it('puts problems in place of the state', () => {
    expect(docState(doc({ disputed: true, team_owner: true }))).toMatchObject({ tone: 'warn', primary: 'choose' })
    expect(docState(doc({ disputed: true })).primary).toBe('')
    expect(docState(doc({ owner_not_member: true, team_owner: true }))).toMatchObject({ tone: 'danger', head: 'No owner: jan is no longer a member' })
    expect(docState(doc({}, { transfer: { moving_to: 'parcels' } })).head).toBe('Being moved to parcels')
  })
  it('says a change of owner is pending', () => {
    expect(docState(doc({ reassigned_to: 'ola@library.example' })).sub).toMatch(/Changing owner to you/)
  })
})
