// Markdown rendering for untrusted blueprints (spec 11.2): raw HTML is off,
// so markdown-it escapes it; links are limited to http and https, and
// plugin references (gh:...) become spans, never links; images become links, since the CSP would block
// remote images anyway and they could be used for tracking.
import MarkdownIt from 'markdown-it'

const md = new MarkdownIt({ html: false, linkify: false, typographer: false })

const safeLink = /^(https?:\/\/|#)/i

export function allowedLink(url: string): boolean {
  return safeLink.test(url.trim())
}

// pluginRef returns the scheme of a reference for a plugin (spec 10.1),
// or null. Such links never become links: they render as a span the UI
// decorates with what the reader's plugin resolved.
const pluginRe = /^([a-z][a-z0-9+.-]*):\S*$/
const notDispatched = new Set(['http', 'https', 'mailto', 'file', 'javascript', 'vbscript', 'data', 'blob', 'about'])
export function pluginRef(url: string): string | null {
  const m = pluginRe.exec(url)
  return m && !notDispatched.has(m[1]) ? m[1] : null
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

// render turns Markdown into HTML that is safe to insert.
export function render(src: string): string {
  return md.render(src)
}

// Part is a piece of a section: Markdown rendered to safe HTML, or a fence
// the UI draws itself.
export type Part =
  | { kind: 'html'; html: string }
  | { kind: 'trace'; line: number; source: string }
  | { kind: 'mermaid'; source: string }

// renderParts renders a section with its top-level ```trace and ```mermaid
// fences cut out; line is the file line of the trace's opening fence, to
// match the daemon's blocks. firstLine is the section's first file line.
// Fences inside lists or quotes stay code.
export function renderParts(src: string, firstLine: number): Part[] {
  const env = {}
  const tokens = md.parse(src, env)
  const out: Part[] = []
  let start = 0
  const flush = (end: number) => {
    if (end > start) out.push({ kind: 'html', html: md.renderer.render(tokens.slice(start, end), md.options, env) })
  }
  tokens.forEach((t, i) => {
    if (t.type !== 'fence' || t.level !== 0 || !t.map) return
    const info = t.info.trim().split(/\s+/)[0]
    if (info !== 'trace' && info !== 'mermaid') return
    flush(i)
    out.push(info === 'trace' ? { kind: 'trace', line: firstLine + t.map[0], source: t.content } : { kind: 'mermaid', source: t.content })
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
      out.push(f[2] === 'trace' ? '[trace]' : f[2] === 'mermaid' ? '[diagram]' : '[code]')
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
