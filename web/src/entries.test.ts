import { describe, expect, it } from 'vitest'
import type { Entry } from './api'
import { oldThread, versionAt } from './entries'

const e = (x: Partial<Entry>): Entry => ({ id: 'x', kind: 'comment', author: 'ola@library.example', timestamp: '', anchor: 'a', rev: '', body: '', valid: true, ...x })
const plan = 'plan:' + '0'.repeat(64) + ' · owner:jan@library.example'

describe('versionAt', () => {
  const log = [
    e({ id: 'r1', kind: 'release', timestamp: '2026-01-01T00:00:00Z', body: 'v1 · ' + plan }),
    e({ id: 'r2', kind: 'release', timestamp: '2026-02-01T00:00:00Z', body: 'v2 · ' + plan }),
  ]
  it('is the last release before the entry', () => {
    expect(versionAt(log, '2025-12-01T00:00:00Z')).toBe(0)
    expect(versionAt(log, '2026-01-15T00:00:00Z')).toBe(1)
    expect(versionAt(log, '2026-03-01T00:00:00Z')).toBe(2)
  })
})

describe('oldThread', () => {
  it('folds only threads that are all on an earlier text', () => {
    const top = e({ id: 't', outdated: true })
    const replies = new Map([['t', [e({ id: 'r', reply_to: 't', outdated: true })]]])
    expect(oldThread(top, replies)).toBe(true)
    replies.set('t', [e({ id: 'r', reply_to: 't' })])
    expect(oldThread(top, replies)).toBe(false)
    expect(oldThread(e({ id: 'd', kind: 'deviation', outdated: true }), new Map())).toBe(false)
  })
})
