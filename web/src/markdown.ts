// Markdown rendering for untrusted blueprints (spec 11.2): raw HTML is off,
// so markdown-it escapes it; links are limited to http and https, and
// plugin references (gh:...) become spans, never links; images become links, since the CSP would block
// remote images anyway and they could be used for tracking.
import MarkdownIt from 'markdown-it'
import { Matcher } from './glossary'
import { highlightFence } from './highlight'

// Fenced code is highlighted for the languages highlight.ts knows, never the
// fences the page draws itself; hljs escapes what it returns.
const md = new MarkdownIt({
  html: false,
  linkify: false,
  typographer: false,
  highlight: (code, info) => (['trace', 'mermaid', 'glossary', 'invariants'].includes(info.trim().split(/\s+/)[0]) ? '' : highlightFence(code, info)),
})

const safeLink = /^(https?:\/\/|#)/i

export function allowedLink(url: string): boolean {
  return safeLink.test(url.trim())
}

// pluginRef returns the scheme of a reference for a plugin (spec 10.1),
// or null. Such links never become links: they render as a span the UI
// decorates with what the reader's plugin resolved.
const pluginRe = /^([a-z][a-z0-9+.-]*):/
// White space as Go's unicode.IsSpace has it, so the page and the daemon
// agree on what is a reference (testdata/refs/corpus.json).
const space = /[\t\n\v\f\r \u0085\u00a0\p{Zs}\u2028\u2029]/u
const notDispatched = new Set(['http', 'https', 'mailto', 'file', 'javascript', 'vbscript', 'data', 'blob', 'about'])
export function pluginRef(url: string): string | null {
  const m = pluginRe.exec(url)
  return m && !notDispatched.has(m[1]) && !space.test(url) ? m[1] : null
}

md.validateLink = (url) => allowedLink(url) || pluginRef(url) !== null
// Keep a reference exactly as written, so it matches the daemon's key.
const defaultNormalize = md.normalizeLink.bind(md)
md.normalizeLink = (url) => (pluginRef(url) ? url : defaultNormalize(url))

const defaultLinkOpen =
  md.renderer.rules.link_open ?? ((tokens, idx, options, _env, self) => self.renderToken(tokens, idx, options))

md.renderer.rules.link_open = (tokens, idx, options, env, self) => {
  const t = tokens[idx]
  const href = String(t.attrGet('href') ?? '')
  if (pluginRef(href)) {
    // Mark the matching close so it ends the span.
    for (let j = idx + 1, depth = 0; j < tokens.length; j++) {
      if (tokens[j].type === 'link_open') depth++
      if (tokens[j].type === 'link_close') {
        if (depth === 0) {
          tokens[j].meta = { ...(tokens[j].meta ?? {}), ref: true }
          break
        }
        depth--
      }
    }
    return `<span class="ref" data-ref="${md.utils.escapeHtml(href)}">`
  }
  if (!href.startsWith('#')) {
    t.attrSet('target', '_blank')
    t.attrSet('rel', 'noopener noreferrer')
  }
  return defaultLinkOpen(tokens, idx, options, env, self)
}

const defaultLinkClose =
  md.renderer.rules.link_close ?? ((tokens, idx, options, _env, self) => self.renderToken(tokens, idx, options))
md.renderer.rules.link_close = (tokens, idx, options, env, self) =>
  tokens[idx].meta?.ref ? '</span>' : defaultLinkClose(tokens, idx, options, env, self)

md.renderer.rules.image = (tokens, idx) => {
  const t = tokens[idx]
  const src = String(t.attrGet('src') ?? '')
  const alt = md.utils.escapeHtml(t.content || 'image')
  if (allowedLink(src) && !src.startsWith('#')) {
    return `<a href="${md.utils.escapeHtml(src)}" target="_blank" rel="noopener noreferrer" class="img-link">[image: ${alt}]</a>`
  }
  return `<span class="img-link">[image: ${alt}]</span>`
}

// Linker says what to link in prose (spec 6.15, 6.16): glossary terms,
// and invariant ids with their statements (ref → statement).
export interface Linker {
  terms?: Matcher
  invariants?: Map<string, string>
}

// LinkEnv is markdown-it's env while linking: counts collects how often
// each term is used.
interface LinkEnv {
  [key: string | symbol]: unknown
  link?: Linker
  counts?: Map<number, number>
}

type Token = ReturnType<typeof md.parse>[number]
type TokenCtor = new (type: string, tag: string, nesting: -1 | 0 | 1) => Token

const esc = (s: string) => md.utils.escapeHtml(s)
const wordCh = /[\p{L}\p{Nd}\p{M}#_-]/u

// invariantMatches finds the invariant refs in s as whole words.
function invariantMatches(s: string, refs: Map<string, string>): { start: number; end: number; ref: string }[] {
  if (refs.size === 0) return []
  const keys = [...refs.keys()].sort((a, b) => b.length - a.length)
  const re = new RegExp(keys.map((k) => k.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|'), 'g')
  const out: { start: number; end: number; ref: string }[] = []
  for (const m of s.matchAll(re)) {
    const start = m.index!
    const end = start + m[0].length
    const before = start > 0 ? String.fromCodePoint(s.codePointAt(start - 1)!) : ''
    const after = end < s.length ? String.fromCodePoint(s.codePointAt(end)!) : ''
    if ((before && wordCh.test(before)) || (after && wordCh.test(after))) continue
    out.push({ start, end, ref: m[0] })
  }
  return out
}

// linkInline replaces runs of text in one inline token's children with
// linked terms and invariant ids. Code, link targets and images are not
// text tokens, so they are never linked; ids are not linked inside links.
function linkInline(children: Token[], env: LinkEnv, TokenCtor: TokenCtor): Token[] {
  const link = env.link!
  const out: Token[] = []
  let depth = 0
  let run: Token[] = []
  const html = (content: string) => {
    const t = new TokenCtor('html_inline', '', 0)
    t.content = content
    return t
  }
  const flush = () => {
    if (run.length === 0) return
    const text = run.map((t) => (t.type === 'softbreak' ? '\n' : t.content)).join('')
    const terms = link.terms && !link.terms.empty ? link.terms.match(text) : []
    const invs = depth === 0 && link.invariants ? invariantMatches(text, link.invariants) : []
    type M = { start: number; end: number; html: string }
    const ms: M[] = []
    for (const m of invs) {
      const st = link.invariants!.get(m.ref) || 'not found in the domain version'
      ms.push({
        start: m.start,
        end: m.end,
        html: `<span class="inv-ref" tabindex="0" role="link" data-inv="${esc(m.ref)}" data-def="${esc(st)}">${esc(m.ref)}</span>`,
      })
    }
    // An invariant reference wins over a term inside it.
    for (const m of terms) {
      if (invs.some((x) => m.start < x.end && x.start < m.end)) continue
      const t = link.terms!.terms[m.term]
      env.counts?.set(m.term, (env.counts.get(m.term) ?? 0) + 1)
      const surface = text.slice(m.start, m.end)
      const def = (m.alias ? `An alias of “${t.term}”: ` : '') + t.definition + (t.context ? ` (${t.context})` : '')
      ms.push({
        start: m.start,
        end: m.end,
        html: `<span class="term${m.alias ? ' alias' : ''}" tabindex="0" data-term="${esc(t.term)}" data-def="${esc(def)}">${esc(surface)}</span>`,
      })
    }
    if (ms.length === 0) {
      out.push(...run)
      run = []
      return
    }
    ms.sort((a, b) => a.start - b.start)
    let at = 0
    const plain = (s: string) => {
      if (!s) return
      const t = new TokenCtor('text', '', 0)
      t.content = s
      out.push(t)
    }
    for (const m of ms) {
      plain(text.slice(at, m.start))
      out.push(html(m.html))
      at = m.end
    }
    plain(text.slice(at))
    run = []
  }
  for (const c of children) {
    if (c.type === 'text' || c.type === 'softbreak') {
      run.push(c)
      continue
    }
    flush()
    if (c.type === 'link_open') depth++
    if (c.type === 'link_close') depth--
    out.push(c)
  }
  flush()
  return out
}

md.core.ruler.push('kalka_link', (state) => {
  const env = state.env as LinkEnv
  if (!env?.link) return
  for (const t of state.tokens) {
    if (t.type === 'inline' && t.children) t.children = linkInline(t.children, env, state.Token)
  }
})

// render turns Markdown into HTML that is safe to insert; link names what
// to link in it.
export function render(src: string, link?: Linker): string {
  return md.render(src, { link })
}

// termCounts is how often each term (by index) is used in the texts, as
// the page links them.
export function termCounts(texts: string[], terms: Matcher): Map<number, number> {
  const env: LinkEnv = { link: { terms }, counts: new Map() }
  for (const s of texts) md.parse(s, env)
  return env.counts!
}

// Part is a piece of a section: Markdown rendered to safe HTML, or a fence
// the UI draws itself.
export type Part =
  | { kind: 'html'; html: string }
  | { kind: 'trace'; line: number; source: string }
  | { kind: 'mermaid'; source: string }
  // A glossary or invariants block: line and end are the file lines of its
  // fences, to find what the daemon read from it.
  | { kind: 'glossary' | 'invariants'; line: number; end: number; source: string }

// renderParts renders a section with its top-level ```trace and ```mermaid
// fences cut out; line is the file line of the trace's opening fence, to
// match the daemon's blocks. firstLine is the section's first file line.
// Fences inside lists or quotes stay code.
export function renderParts(src: string, firstLine: number, link?: Linker): Part[] {
  const env: LinkEnv = { link }
  const tokens = md.parse(src, env)
  const out: Part[] = []
  let start = 0
  const flush = (end: number) => {
    if (end > start) out.push({ kind: 'html', html: md.renderer.render(tokens.slice(start, end), md.options, env) })
  }
  tokens.forEach((t, i) => {
    if (t.type !== 'fence' || t.level !== 0 || !t.map) return
    const info = t.info.trim().split(/\s+/)[0]
    if (info !== 'trace' && info !== 'mermaid' && info !== 'glossary' && info !== 'invariants') return
    flush(i)
    const line = firstLine + t.map[0]
    if (info === 'trace') out.push({ kind: 'trace', line, source: t.content })
    else if (info === 'mermaid') out.push({ kind: 'mermaid', source: t.content })
    else out.push({ kind: info, line, end: firstLine + t.map[1] - 1, source: t.content })
    start = i + 1
  })
  flush(tokens.length)
  return out
}

// refsIn lists the plugin references (spec 10.1) a Markdown text links
// to, in order of first appearance, as written.
export function refsIn(src: string): string[] {
  const seen = new Set<string>()
  const walk = (ts: ReturnType<typeof md.parse>) => {
    for (const t of ts) {
      if (t.type === 'link_open') {
        const href = String(t.attrGet('href') ?? '')
        if (pluginRef(href)) seen.add(href)
      }
      if (t.children) walk(t.children)
    }
  }
  walk(md.parse(src, {}))
  return [...seen]
}

// plainText is a rough one-paragraph reading of Markdown for previews:
// headings, fences and markup are dropped, references keep their label.
export function plainText(src: string): string {
  const out: string[] = []
  let fence = ''
  for (const line of src.split('\n')) {
    const f = /^\s*(```+|~~~+)\s*(\S*)/.exec(line)
    if (fence) {
      if (f && f[1][0] === fence[0] && f[1].length >= fence.length) fence = ''
      continue
    }
    if (f) {
      fence = f[1]
      out.push(
        f[2] === 'trace'
          ? '[trace]'
          : f[2] === 'mermaid'
            ? '[diagram]'
            : f[2] === 'invariants'
              ? '[invariants]'
              : f[2] === 'glossary'
                ? '[glossary]'
                : '[code]',
      )
      continue
    }
    if (/^\s{0,3}#{1,6}\s/.test(line)) continue
    out.push(line)
  }
  return out
    .join(' ')
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/<[^>]*>/g, '')
    .replace(/(^|\s)(>|[-*+]|\d+\.)\s+/g, '$1')
    .replace(/[*_`|]+/g, '')
    .replace(/\s+/g, ' ')
    .trim()
}

// linkPlain is plain text (a trace step's text, an invariant's statement)
// as safe HTML with its terms and invariant ids linked.
export function linkPlain(text: string, link?: Linker): string {
  if (!link) return md.utils.escapeHtml(text)
  const t = new md.core.State('', md, {}).Token
  const tok = new t('text', '', 0)
  tok.content = text
  const env: LinkEnv = { link }
  return md.renderer.renderInline(linkInline([tok], env, t), md.options, env)
}
