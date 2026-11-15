import { describe, expect, it } from 'vitest'
import { joinHref, parseRoute, setupFromInvite } from './util'

describe('join route', () => {
  it('parses the join screen of a token', () => {
    expect(parseRoute('#/join/ABCDEF234567')).toEqual({ view: 'join', token: 'ABCDEF234567' })
  })
  it('opens an empty join screen for a link that held no invite', () => {
    expect(parseRoute('#/join/')).toEqual({ view: 'join', token: '' })
    expect(parseRoute('#/join')).toEqual({ view: 'join', token: '' })
    expect(parseRoute('#/join/%E0%A4%A')).toEqual({ view: 'join', token: '' })
  })
  it('round-trips what was pasted, for the daemon to parse', () => {
    const pasted = '  <kalka://join/ABC?x=1#y>  '
    const r = parseRoute(joinHref(pasted))
    expect(r).toEqual({ view: 'join', token: '<kalka://join/ABC?x=1#y>' })
  })
})

describe('setup from an invite', () => {
  it('takes the email and the cloud', () => {
    expect(setupFromInvite({ email: 'olga@library.example', rendezvous: 'peerjs-cloud' })).toEqual({
      email: 'olga@library.example',
      mode: 'cloud',
      server: '',
    })
  })
  it('takes the team server', () => {
    expect(setupFromInvite({ email: 'olga@library.example', rendezvous: 'wss://rdv.library.example/' })).toEqual({
      email: 'olga@library.example',
      mode: 'server',
      server: 'wss://rdv.library.example/',
    })
  })
})
