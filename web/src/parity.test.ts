// Parity (spec 12.2): the action ids of internal/cli/parity.json, those
// declared in actions.ts, and those the controls carry are one set.
import { describe, expect, it } from 'vitest'
import { ACTIONS } from './actions'

const parity = Object.values(import.meta.glob('../../internal/cli/parity.json', { import: 'default', eager: true }))[0] as {
  rows: { command: string; class: string; action?: string }[]
}

const sources = import.meta.glob(['./**/*.tsx', '!./**/*.test.tsx'], { query: '?raw', import: 'default', eager: true }) as Record<string, string>

const declared = new Set(Object.keys(ACTIONS))
const inTable = new Set(parity.rows.flatMap((r) => (r.action ? [r.action] : [])))
const used = new Map<string, string[]>()
for (const [file, src] of Object.entries(sources)) {
  for (const m of src.matchAll(/\bact\(\s*['"]([a-z]+\.[a-z]+)['"]\s*\)/g)) {
    used.set(m[1], [...(used.get(m[1]) ?? []), file])
  }
}

describe('parity', () => {
  it('reads the table and the components', () => {
    expect(parity.rows.length).toBeGreaterThan(30)
    expect(Object.keys(sources).length).toBeGreaterThan(5)
  })
  it('declares every action id of the table', () => {
    expect([...inTable].filter((a) => !declared.has(a))).toEqual([])
  })
  it('has a command for every declared action', () => {
    expect([...declared].filter((a) => !inTable.has(a))).toEqual([])
  })
  it('puts every declared action on a control', () => {
    expect([...declared].filter((a) => !used.has(a))).toEqual([])
  })
  it('uses only declared actions', () => {
    expect([...used.keys()].filter((a) => !declared.has(a))).toEqual([])
  })
  it('gives every workflow row an action', () => {
    expect(parity.rows.filter((r) => r.class === 'workflow' && !r.action).map((r) => r.command)).toEqual([])
  })
})
