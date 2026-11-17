// Diagnostics of a document (spec 6.17), as the daemon's checker reports
// them: the page never computes one itself.
import type { Doc, Problem } from './api'

/** Structural errors: the file cannot be published until they are fixed. */
export const blocking = (doc: Pick<Doc, 'problems'>): Problem[] => doc.problems.filter((p) => p.blocks)

/** Errors that do not block: Publish and Release ask first. */
export const confirmable = (doc: Pick<Doc, 'problems'>): Problem[] => doc.problems.filter((p) => p.severity === 'error' && !p.blocks)

/** Hints of the blueprint reference (H…), shown in their own strip. */
export const hintsOf = (doc: Pick<Doc, 'problems'>): Problem[] => doc.problems.filter((p) => p.rule.startsWith('H'))

/** What the diagnostics list shows: everything but hints and the note
 * that a document is local. */
export const listed = (doc: Pick<Doc, 'problems'>): Problem[] => doc.problems.filter((p) => !p.rule.startsWith('H') && p.rule !== 'F005')

const order = { error: 0, warning: 1, info: 2 }

/** bySeverity sorts errors first, then warnings, then information, keeping
 * file order within each. */
export function bySeverity(ps: Problem[]): Problem[] {
  return ps
    .map((p, i) => ({ p, i }))
    .sort((a, b) => order[a.p.severity] - order[b.p.severity] || a.i - b.i)
    .map((x) => x.p)
}

/** where is "line 12" or "line 12, column 4", or "". */
export function where(p: Problem): string {
  if (!p.line) return ''
  return p.column ? `line ${p.line}, column ${p.column}` : `line ${p.line}`
}
