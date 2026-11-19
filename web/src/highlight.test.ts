import { describe, expect, it } from 'vitest'
import { highlightFence, highlightLines, languageOf, splitLines } from './highlight'

describe('languageOf', () => {
  it('prefers the plugin language, resolving aliases', () => {
    expect(languageOf('go', 'x.py')).toBe('go')
    expect(languageOf('golang')).toBe('go')
    expect(languageOf('TS')).toBe('typescript')
  })
  it('infers from the extension of a title or url', () => {
    expect(languageOf(undefined, 'internal/a/b.go:10-20')).toBe(null)
    expect(languageOf(undefined, 'internal/a/b.go')).toBe('go')
    expect(languageOf(undefined, undefined, 'https://h/x/y.py?plain=1#L3')).toBe('python')
    expect(languageOf('nonsense', 'a.rs')).toBe('rust')
    expect(languageOf(undefined, 'Dockerfile')).toBe('dockerfile')
  })
  it('does not guess', () => {
    expect(languageOf(undefined, 'README')).toBe(null)
    expect(languageOf(undefined, 'a.unknownext')).toBe(null)
  })
})

describe('splitLines', () => {
  it('keeps spans balanced per line', () => {
    expect(splitLines('<span class="a">x\ny</span>z')).toEqual(['<span class="a">x</span>', '<span class="a">y</span>z'])
    expect(splitLines('a<span class="a"><span class="b">1\n2\n3</span></span>')).toEqual([
      'a<span class="a"><span class="b">1</span></span>',
      '<span class="a"><span class="b">2</span></span>',
      '<span class="a"><span class="b">3</span></span>',
    ])
  })
  it('keeps empty lines', () => {
    expect(splitLines('a\n\nb')).toEqual(['a', '', 'b'])
  })
})

describe('highlightLines', () => {
  it('keeps one entry per line for multi-line comments and strings', () => {
    const src = '/* one\n two\n three */\nconst s = `a\nb`\n'
    const lines = highlightLines(src, 'javascript')
    expect(lines).toHaveLength(5)
    for (const l of lines) expect((l.match(/<span/g) ?? []).length).toBe((l.match(/<\/span>/g) ?? []).length)
    expect(lines[1]).toContain('hljs-comment')
  })
  it('escapes markup, highlighted or not', () => {
    expect(highlightLines('<b>&"x"</b>', null)).toEqual(['&lt;b&gt;&amp;&quot;x&quot;&lt;/b&gt;'])
    const html = highlightLines('x := "<script>alert(1)</script>"', 'go').join('')
    expect(html).not.toContain('<script')
    expect(html).toContain('&lt;script&gt;')
  })
  it('falls back to plain lines for an unknown language', () => {
    expect(highlightLines('a\nb', 'nope')).toEqual(['a', 'b'])
  })
})

describe('highlightFence', () => {
  it('highlights known languages only', () => {
    expect(highlightFence('func main() {}', 'go')).toContain('hljs-keyword')
    expect(highlightFence('x', 'trace')).toBe('')
    expect(highlightFence('x', 'mermaid')).toBe('')
    expect(highlightFence('x', '')).toBe('')
  })
})
