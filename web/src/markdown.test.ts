// Hardening tests for spec 11.2: content from a received blueprint
// must not run script or produce dangerous links.
import { describe, expect, it } from 'vitest'
import { pluginRef, render } from './markdown'
import corpus from '../../testdata/refs/corpus.json'

function dom(src: string): HTMLElement {
  const div = document.createElement('div')
  div.innerHTML = render(src)
  return div
}

describe('render', () => {
  it('escapes raw HTML, including script tags and event handlers', () => {
    const d = dom('Hello <script>window.pwned = 1</script>\n\n<img src=x onerror="window.pwned=2">\n\n<div onclick="x()">hi</div>')
    expect(d.querySelector('script')).toBeNull()
    expect(d.querySelector('img')).toBeNull()
    expect(d.querySelector('[onclick]')).toBeNull()
    expect(d.querySelector('[onerror]')).toBeNull()
    expect(d.textContent).toContain('<script>')
  })

  it('drops javascript:, data: and vbscript: links', () => {
    for (const url of ['javascript:alert(1)', 'JaVaScRiPt:alert(1)', ' javascript:alert(1)', 'data:text/html,<script>x</script>', 'vbscript:x', 'file:///etc/passwd']) {
      const d = dom(`[click](${url}) and <${url}>`)
      for (const a of Array.from(d.querySelectorAll('a'))) {
        expect(a.getAttribute('href') ?? '').toMatch(/^(https?:|#)/)
      }
    }
  })

  it('keeps http and https links and opens them without opener', () => {
    const a = dom('[x](https://example.com/a)').querySelector('a')!
    expect(a.getAttribute('href')).toBe('https://example.com/a')
    expect(a.getAttribute('rel')).toBe('noopener noreferrer')
  })

  it('turns images into links and never into img elements', () => {
    const d = dom('![diagram](https://example.com/x.png) ![bad](javascript:alert(1))')
    expect(d.querySelector('img')).toBeNull()
    expect(d.querySelector('a.img-link')?.getAttribute('href')).toBe('https://example.com/x.png')
    // A disallowed source is not parsed as an image at all: it stays text.
    expect(d.textContent).toContain('bad')
    expect(d.querySelectorAll('a').length).toBe(1)
  })

  it('renders code fences, including trace and mermaid, as escaped code', () => {
    const d = dom('```trace\nlanes: [a, b]\n<script>x</script>\n```')
    expect(d.querySelector('pre code')?.textContent).toContain('<script>x</script>')
    expect(d.querySelector('script')).toBeNull()
  })
})

describe('renderParts', () => {
  it('cuts out top-level trace and mermaid fences and keeps references working', async () => {
    const { renderParts } = await import('./markdown')
    const src = '## Plan\n\nSee [the ticket][t].\n\n```trace\nlanes: [a, b]\n```\n\n- item\n\n  ```trace\n  nested\n  ```\n\n```mermaid\ngraph LR\n```\n\nAfter [again][t].\n\n[t]: https://example.com/t\n'
    const parts = renderParts(src, 10)
    expect(parts.map((p) => p.kind)).toEqual(['html', 'trace', 'html', 'mermaid', 'html'])
    expect(parts[1]).toEqual({ kind: 'trace', line: 14, source: 'lanes: [a, b]\n' })
    const html = parts.filter((p) => p.kind === 'html').map((p) => (p as { html: string }).html)
    expect(html[0]).toContain('href="https://example.com/t"')
    expect(html[1]).toContain('<code class="language-trace">')
    expect(html[2]).toContain('href="https://example.com/t"')
  })
})

describe('plugin references', () => {
  it('render as spans keyed by the reference as written, never as links', () => {
    const html = render('See [parse](gh:src/a.py#L1-L9) and <jira:ABC-12>, [js](javascript:alert(1)), [d](data:text/html,x), [JS](JavaScript:alert(1)).')
    expect(html).toContain('<span class="ref" data-ref="gh:src/a.py#L1-L9">parse</span>')
    expect(html).toContain('<span class="ref" data-ref="jira:ABC-12">jira:ABC-12</span>')
    expect(render('[form](gh:web:src/Form.tsx#L1-L9)')).toContain('<span class="ref" data-ref="gh:web:src/Form.tsx#L1-L9">form</span>')
    expect(html).not.toMatch(/href="(javascript|data|JavaScript):/i)
    expect(html).not.toContain('data-ref="javascript')
    expect(html).not.toContain('data-ref="data')
    const quoted = render('[x](gh:a"onmouseover="alert(1))')
    expect(quoted).not.toMatch(/data-ref="[^"]*"onmouseover/)
  })
})

// The shared corpus: internal/blueprint classifies the same targets the
// same way (TestPluginRefCorpus reads this file too).
describe('plugin references, shared with the daemon', () => {
  for (const c of corpus.cases) {
    it(JSON.stringify(c.target), () => expect(pluginRef(c.target)).toBe(c.scheme))
  }
})

describe('the kalka language v2 blocks', () => {
  it('cuts out a goals block with its file lines', async () => {
    const { renderParts, plainText } = await import('./markdown')
    const src = 'Intro\n\n```goals v2\ngoal G1: The queue head learns\n```\n\nAfter\n'
    const parts = renderParts(src, 10)
    expect(parts.map((p) => p.kind)).toEqual(['html', 'goals', 'html'])
    const g = parts[1] as { kind: 'goals'; line: number; end: number }
    expect([g.line, g.end]).toEqual([12, 14])
    expect(plainText(src)).toBe('Intro [goals] After')
  })
  it('says where a term is defined and what narrows it', async () => {
    const { termDef } = await import('./markdown')
    const def = termDef({
      term: 'loan',
      definition: 'One copy lent to one member',
      line: 3,
      domain: 'library',
      refined: [{ domain: 'library.reservations', id: 'R2', statement: 'A loan with a queue cannot be renewed' }],
    })
    expect(def).toBe('One copy lent to one member — library\nNarrowed by library.reservations#R2: A loan with a queue cannot be renewed')
  })
})

describe('ownNodes', () => {
  it('lists the document’s own goals and criteria, not other documents’', async () => {
    const { ownNodes } = await import('./Goals')
    const doc = {
      plan: {
        path: [],
        children: [],
        goals: [
          {
            kind: 'goal',
            id: 'G1',
            text: 'a',
            state: 'claimed',
            own: true,
            claimants: [
              { kind: 'criterion', id: 'AC1', text: 'b', state: 'open', own: true },
              { kind: 'criterion', id: 'AC9', text: 'c', state: 'open' },
            ],
          },
        ],
      },
    } as unknown as import('./api').Doc
    expect(ownNodes(doc).map((n) => n.id)).toEqual(['G1', 'AC1'])
  })
})
