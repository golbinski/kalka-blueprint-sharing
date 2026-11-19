// Syntax highlighting for snippets and fenced code: highlight.js core with a
// curated set of languages. The language is named by the plugin or inferred
// from a file extension, never auto-detected (slow, and wrong on short text).
// Only highlight.js's own escaped output is ever injected as HTML.
import hljs from 'highlight.js/lib/core'
import bash from 'highlight.js/lib/languages/bash'
import c from 'highlight.js/lib/languages/c'
import cpp from 'highlight.js/lib/languages/cpp'
import csharp from 'highlight.js/lib/languages/csharp'
import css from 'highlight.js/lib/languages/css'
import diff from 'highlight.js/lib/languages/diff'
import dockerfile from 'highlight.js/lib/languages/dockerfile'
import go from 'highlight.js/lib/languages/go'
import ini from 'highlight.js/lib/languages/ini'
import java from 'highlight.js/lib/languages/java'
import javascript from 'highlight.js/lib/languages/javascript'
import json from 'highlight.js/lib/languages/json'
import kotlin from 'highlight.js/lib/languages/kotlin'
import makefile from 'highlight.js/lib/languages/makefile'
import markdown from 'highlight.js/lib/languages/markdown'
import php from 'highlight.js/lib/languages/php'
import python from 'highlight.js/lib/languages/python'
import ruby from 'highlight.js/lib/languages/ruby'
import rust from 'highlight.js/lib/languages/rust'
import sql from 'highlight.js/lib/languages/sql'
import swift from 'highlight.js/lib/languages/swift'
import typescript from 'highlight.js/lib/languages/typescript'
import xml from 'highlight.js/lib/languages/xml'
import yaml from 'highlight.js/lib/languages/yaml'

const languages = { bash, c, cpp, csharp, css, diff, dockerfile, go, ini, java, javascript, json, kotlin, makefile, markdown, php, python, ruby, rust, sql, swift, typescript, xml, yaml }
for (const [name, def] of Object.entries(languages)) hljs.registerLanguage(name, def)

const byExt: Record<string, string> = {
  go: 'go', ts: 'typescript', tsx: 'typescript', mts: 'typescript', cts: 'typescript',
  js: 'javascript', jsx: 'javascript', mjs: 'javascript', cjs: 'javascript',
  py: 'python', java: 'java', kt: 'kotlin', kts: 'kotlin', rs: 'rust',
  c: 'c', h: 'c', cc: 'cpp', cpp: 'cpp', cxx: 'cpp', hpp: 'cpp', hh: 'cpp',
  cs: 'csharp', rb: 'ruby', php: 'php', swift: 'swift',
  sh: 'bash', bash: 'bash', zsh: 'bash', yml: 'yaml', yaml: 'yaml', json: 'json',
  sql: 'sql', xml: 'xml', html: 'xml', htm: 'xml', svg: 'xml', css: 'css',
  md: 'markdown', markdown: 'markdown', ini: 'ini', toml: 'ini', cfg: 'ini', conf: 'ini',
  diff: 'diff', patch: 'diff', mk: 'makefile',
}
const byName: Record<string, string> = { dockerfile: 'dockerfile', makefile: 'makefile', gnumakefile: 'makefile' }

// languageOf is the registered language for a snippet: the one the plugin
// named if it is known, else the one of the file extension (or name) in the
// first of names that has one (a title or url), else null.
export function languageOf(lang: string | undefined, ...names: (string | undefined)[]): string | null {
  const l = lang?.trim().toLowerCase()
  if (l && hljs.getLanguage(l)) return resolve(l)
  for (const n of names) {
    if (!n) continue
    const file = (n.split(/[?#]/)[0].split(/[\\/]/).pop() ?? '').toLowerCase()
    if (byName[file]) return byName[file]
    const dot = file.lastIndexOf('.')
    if (dot >= 0 && byExt[file.slice(dot + 1)]) return byExt[file.slice(dot + 1)]
  }
  return null
}

// resolve maps an alias (golang, ts, sh) to its registered name.
function resolve(alias: string): string {
  const name = hljs.getLanguage(alias)!.name
  return Object.keys(languages).find((k) => hljs.getLanguage(k)?.name === name) ?? alias
}

const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')

// splitLines cuts highlighted HTML at its newlines. A span open across a
// newline is closed at the end of the line and reopened on the next, so each
// line is balanced and line numbers stay aligned.
export function splitLines(html: string): string[] {
  const out: string[] = []
  const open: string[] = []
  let cur = ''
  const re = /<span[^>]*>|<\/span>|\n|[^<\n]+|</g
  for (const m of html.matchAll(re)) {
    const s = m[0]
    if (s === '\n') {
      out.push(cur + '</span>'.repeat(open.length))
      cur = open.join('')
    } else {
      if (s === '</span>') open.pop()
      else if (s.startsWith('<span')) open.push(s)
      cur += s
    }
  }
  out.push(cur + '</span>'.repeat(open.length))
  return out
}

// highlightLines is the HTML of each line of code (trailing newline
// dropped); lines are escaped text when the language is unknown or fails.
export function highlightLines(code: string, lang: string | null): string[] {
  const text = code.replace(/\n$/, '')
  if (lang && hljs.getLanguage(lang)) {
    try {
      return splitLines(hljs.highlight(text, { language: lang, ignoreIllegals: true }).value)
    } catch {
      // fall through to plain text
    }
  }
  return text.split('\n').map(esc)
}

// highlightFence is the highlighted HTML of a Markdown code block, or ''
// when its info string names no known language.
export function highlightFence(code: string, info: string): string {
  const lang = languageOf(info.trim().split(/\s+/)[0])
  if (!lang) return ''
  try {
    return hljs.highlight(code, { language: lang, ignoreIllegals: true }).value
  } catch {
    return ''
  }
}
