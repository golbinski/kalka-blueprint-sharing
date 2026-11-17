// The document's diagnostics (spec 6.17), with each rule's title and
// meaning from the registry, and the confirmation Publish and Release ask
// for when the document has errors.
import { useEffect, useState } from 'react'
import { api, type Doc, type Problem, type Rule } from './api'
import { bySeverity, confirmable, listed, where } from './diag'
import { plural } from './util'

let rulesCache: Promise<Map<string, Rule>> | null = null

/** useRules loads the registry once per page. */
export function useRules(): Map<string, Rule> {
  const [rules, setRules] = useState<Map<string, Rule>>(new Map())
  useEffect(() => {
    if (!rulesCache) {
      rulesCache = api.rules().then(
        (l) => new Map(l.map((r) => [r.id, r])),
        () => {
          rulesCache = null
          return new Map()
        },
      )
    }
    let live = true
    rulesCache.then((m) => live && setRules(m))
    return () => {
      live = false
    }
  }, [])
  return rules
}

function Item({ p, rules }: { p: Problem; rules: Map<string, Rule> }) {
  const [open, setOpen] = useState(false)
  const r = rules.get(p.rule)
  const at = where(p)
  return (
    <li className={'diag diag-' + p.severity}>
      <span className="diag-sev">{p.severity}</span>{' '}
      <span>
        {at && <span className="muted small">{at} · </span>}
        {p.message}{' '}
        <button type="button" className="link small mono" aria-expanded={open} onClick={() => setOpen(!open)} title={r?.title}>
          {p.rule}
        </button>
        {p.fix && (
          <span className="muted small">
            {' '}
            · write “{p.fix.text}”
          </span>
        )}
        {open && r && (
          <span className="diag-why">
            <b>{r.title}.</b> {r.meaning} <i>Why:</i> {r.why}
          </span>
        )}
      </span>
    </li>
  )
}

/** DiagnosticsPanel lists what kalka check reports for the document. */
export function DiagnosticsPanel({ doc }: { doc: Doc }) {
  const rules = useRules()
  const ps = bySeverity(listed(doc))
  if (ps.length === 0) return null
  const errors = ps.filter((p) => p.severity === 'error').length
  const warnings = ps.filter((p) => p.severity === 'warning').length
  const parts = [errors && plural(errors, 'error'), warnings && plural(warnings, 'warning')].filter(Boolean)
  return (
    <details className="fold diagnostics" open={errors > 0}>
      <summary>
        <b>Checks</b> <span className="muted small">· {parts.length ? parts.join(', ') : plural(ps.length, 'note')} · as kalka check reports them</span>
      </summary>
      <ul className="plain diag-list">
        {ps.map((p, i) => (
          <Item key={i} p={p} rules={rules} />
        ))}
      </ul>
    </details>
  )
}

/** ErrorConfirm shows the errors before Publish or Release: they never
 * block, so the button says "anyway". */
export function ErrorConfirm({ doc, verb }: { doc: Pick<Doc, 'problems'>; verb: string }) {
  const rules = useRules()
  const errs = confirmable(doc)
  if (errs.length === 0) return null
  return (
    <div className="confirm-box warn" role="alert">
      <p>
        kalka check reports {plural(errs.length, 'error')} in this document. {verb} it anyway, or fix {errs.length === 1 ? 'it' : 'them'} first.
      </p>
      <ul className="plain diag-list">
        {errs.map((p, i) => (
          <Item key={i} p={p} rules={rules} />
        ))}
      </ul>
    </div>
  )
}
