import { describe, expect, it } from 'vitest'
import { blocking, bySeverity, confirmable, hintsOf, listed, where } from './diag'
import type { Problem } from './api'

const p = (rule: string, severity: Problem['severity'], extra: Partial<Problem> = {}): Problem => ({ rule, severity, message: rule, ...extra })

describe('diagnostics', () => {
  const doc = {
    problems: [p('F005', 'info'), p('T110', 'warning', { line: 9 }), p('F002', 'error', { blocks: true }), p('T040', 'error', { line: 4 }), p('H002', 'info')],
  }
  it('splits what blocks from what asks first', () => {
    expect(blocking(doc).map((x) => x.rule)).toEqual(['F002'])
    expect(confirmable(doc).map((x) => x.rule)).toEqual(['T040'])
  })
  it('keeps hints apart and leaves out the local note', () => {
    expect(hintsOf(doc).map((x) => x.rule)).toEqual(['H002'])
    expect(listed(doc).map((x) => x.rule)).toEqual(['T110', 'F002', 'T040'])
  })
  it('orders by severity, then file order', () => {
    expect(bySeverity(listed(doc)).map((x) => x.rule)).toEqual(['F002', 'T040', 'T110'])
  })
  it('says where', () => {
    expect(where(p('G020', 'warning', { line: 3, column: 7 }))).toBe('line 3, column 7')
    expect(where(p('F005', 'info'))).toBe('')
  })
})
