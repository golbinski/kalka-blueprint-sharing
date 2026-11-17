import { describe, expect, it } from 'vitest'
import corpus from '../../testdata/glossary/corpus.json'
import type { Term } from './api'
import { Matcher } from './glossary'
import { render, renderParts, termCounts } from './markdown'

const terms = corpus.terms.map((t) => ({ ...t, line: 0 })) as Term[]

// The Go tests run the same corpus through the daemon's matcher.
describe('glossary corpus', () => {
  const m = new Matcher(terms)
  for (const c of corpus.cases) {
    it(JSON.stringify(c.text), () => {
      const got = m.match(c.text).map((x) => [c.text.slice(x.start, x.end), x.term, x.alias])
      expect(got).toEqual(c.matches)
    })
  }
})

describe('linking', () => {
  const link = { terms: new Matcher(terms), invariants: new Map([['I1', 'A parcel has one courier at a time'], ['delivery#I3', 'At most three attempts']]) }
  const dom = (src: string) => {
    const d = document.createElement('div')
    d.innerHTML = render(src, link)
    return d
  }

  it('links prose, headings and link text, not code or link targets', () => {
    const d = dom('## Parcel rules\n\nA package and `parcel` and [the depot](https://example.org/parcel) and\n\n```\nparcel\n```\n')
    expect([...d.querySelectorAll('.term')].map((e) => e.textContent)).toEqual(['Parcel', 'package', 'depot'])
    expect(d.querySelector('.term.alias')?.getAttribute('data-def')).toContain('An alias of “parcel”')
    expect(d.querySelector('a')?.getAttribute('href')).toBe('https://example.org/parcel')
    expect(d.querySelector('code')?.textContent).toBe('parcel')
  })

  it('matches a term across a line break, and not across markup', () => {
    const d = dom('one delivery\nattempt, and par*cel*')
    expect([...d.querySelectorAll('.term')].map((e) => e.textContent)).toEqual(['delivery\nattempt'])
  })

  it('links invariant ids as whole words, not inside links', () => {
    const d = dom('I1 holds, see delivery#I3; not I10 or xI1, [I1](https://example.org)')
    expect([...d.querySelectorAll('.inv-ref')].map((e) => e.getAttribute('data-inv'))).toEqual(['I1', 'delivery#I3'])
    expect(d.querySelector('.inv-ref')?.getAttribute('data-def')).toBe('A parcel has one courier at a time')
  })

  it('escapes what it inserts', () => {
    const bad = new Matcher([{ term: 'parcel', definition: '"><img src=x onerror=alert(1)>', line: 0 }])
    const d = document.createElement('div')
    d.innerHTML = render('a parcel', { terms: bad })
    expect(d.querySelector('img')).toBeNull()
    expect(d.querySelector('.term')?.getAttribute('data-def')).toContain('<img')
  })

  it('counts terms and cuts out typed blocks', () => {
    const c = termCounts(['Parcels go to the depot.', '# Parcel'], link.terms)
    expect([...c.entries()].sort()).toEqual([
      [0, 2],
      [3, 1],
    ])
    const parts = renderParts('## Rules\n\n```invariants v1\ninvariants: []\n```\n', 10, link)
    expect(parts.map((p) => p.kind)).toEqual(['html', 'invariants'])
    expect(parts[1]).toMatchObject({ line: 12, end: 14 })
  })
})
