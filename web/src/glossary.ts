// Glossary matching (spec 6.16), the same as the daemon's
// (internal/blueprint/glossary.go): both run testdata/glossary/corpus.json.
import type { Term } from './api'

// Letters, digits and combining marks make up words, as in Go.
const wordRe = /[\p{L}\p{Nd}\p{M}]/u
// White space as Go's unicode.IsSpace has it.
const spaceRe = /[\t\n\v\f\r \u0085\u00a0\p{Zs}\u2028\u2029]/u

// lower is one character's simple lower case, as Go's unicode.ToLower: a
// character whose lower case is longer keeps its first code point.
function lower(ch: string): string {
  const l = ch.toLowerCase()
  return String.fromCodePoint(l.codePointAt(0)!)
}

/** A phrase as matching compares it: lower case, single spaces. */
export function phraseKey(s: string): string {
  return Array.from(s.trim().split(/[\t\n\v\f\r \u0085\u00a0\p{Zs}\u2028\u2029]+/u).join(' '))
    .map(lower)
    .join('')
}

interface Phrase {
  chars: string[]
  term: number
  alias: boolean
}

export interface TermMatch {
  /** Offsets in UTF-16 code units. */
  start: number
  end: number
  term: number
  alias: boolean
}

export class Matcher {
  readonly terms: Term[]
  private phrases: Phrase[] = []
  constructor(terms: Term[]) {
    this.terms = terms
    terms.forEach((t, i) => {
      for (const p of [t.term, ...(t.forms ?? [])]) this.add(p, i, false)
      for (const p of t.aliases ?? []) this.add(p, i, true)
    })
  }
  private add(p: string, term: number, alias: boolean) {
    const k = phraseKey(p)
    if (k) this.phrases.push({ chars: Array.from(k), term, alias })
  }
  get empty(): boolean {
    return this.phrases.length === 0
  }

  /** The terms in s: case-insensitive, whole words, longest first, never overlapping. */
  match(s: string): TermMatch[] {
    const out: TermMatch[] = []
    if (this.empty) return out
    // Code points with their UTF-16 offsets.
    const cps: { ch: string; at: number }[] = []
    for (let i = 0; i < s.length; ) {
      const ch = String.fromCodePoint(s.codePointAt(i)!)
      cps.push({ ch, at: i })
      i += ch.length
    }
    const offset = (k: number) => (k < cps.length ? cps[k].at : s.length)
    let prevWord = false
    for (let k = 0; k < cps.length; ) {
      const isWord = wordRe.test(cps[k].ch)
      if (isWord && !prevWord) {
        let best = -1
        let bi = -1
        this.phrases.forEach((ph, pi) => {
          const end = matchAt(cps, k, ph.chars)
          if (end > best) {
            best = end
            bi = pi
          }
        })
        if (bi >= 0) {
          const ph = this.phrases[bi]
          out.push({ start: offset(k), end: offset(best), term: ph.term, alias: ph.alias })
          prevWord = wordRe.test(cps[best - 1].ch)
          k = best
          continue
        }
      }
      prevWord = isWord
      k++
    }
    return out
  }
}

// matchAt is the index after ph matched at cps[k], or -1.
function matchAt(cps: { ch: string }[], k: number, ph: string[]): number {
  let j = k
  for (const pc of ph) {
    if (j >= cps.length) return -1
    if (pc === ' ') {
      let n = 0
      while (j < cps.length && spaceRe.test(cps[j].ch)) {
        j++
        n++
      }
      if (n === 0) return -1
      continue
    }
    if (lower(cps[j].ch) !== pc) return -1
    j++
  }
  if (j < cps.length && wordRe.test(cps[j].ch)) return -1
  return j
}
