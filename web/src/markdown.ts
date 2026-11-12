// Markdown rendering for untrusted blueprints (spec 11.2): raw HTML is off,
// so markdown-it escapes it; links are limited to http and https (plugin
// schemes come with M4); images become links, since the CSP would block
// remote images anyway and they could be used for tracking.
import MarkdownIt from 'markdown-it'

const md = new MarkdownIt({ html: false, linkify: false, typographer: false })

const safeLink = /^(https?:\/\/|#)/i

export function allowedLink(url: string): boolean {
  return safeLink.test(url.trim())
}

md.validateLink = allowedLink

const defaultLinkOpen =
  md.renderer.rules.link_open ?? ((tokens, idx, options, _env, self) => self.renderToken(tokens, idx, options))

md.renderer.rules.link_open = (tokens, idx, options, env, self) => {
  const t = tokens[idx]
  const href = String(t.attrGet('href') ?? '')
  if (!href.startsWith('#')) {
    t.attrSet('target', '_blank')
    t.attrSet('rel', 'noopener noreferrer')
  }
  return defaultLinkOpen(tokens, idx, options, env, self)
}

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
