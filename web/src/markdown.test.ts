// Hardening tests for spec 11.2 and M3: content from a received blueprint
// must not run script or produce dangerous links.
import { describe, expect, it } from 'vitest'
import { render } from './markdown'

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
