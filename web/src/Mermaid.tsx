// A ```mermaid block, drawn by the bundled renderer in a sandboxed frame
// (no network, no access to this page; see web/public/mermaid/frame.js).
import { useEffect, useMemo, useRef, useState } from 'react'

export function Mermaid({ source }: { source: string }) {
  const frame = useRef<HTMLIFrameElement>(null)
  const id = useMemo(() => Math.random().toString(36).slice(2), [])
  const [height, setHeight] = useState(160)
  const [err, setErr] = useState('')
  useEffect(() => {
    setErr('')
    const on = (ev: MessageEvent) => {
      const w = frame.current?.contentWindow
      if (!w || ev.source !== w) return
      const m = ev.data as { kalka?: string; type?: string; id?: string; height?: number; message?: string }
      if (!m || m.kalka !== 'mermaid') return
      if (m.type === 'ready') w.postMessage({ kalka: 'mermaid', type: 'render', id, source }, '*')
      if (m.id !== id) return
      if (m.type === 'size' && typeof m.height === 'number') setHeight(Math.min(Math.max(m.height, 40), 4000))
      if (m.type === 'error') setErr(String(m.message ?? 'unknown error'))
    }
    window.addEventListener('message', on)
    return () => window.removeEventListener('message', on)
  }, [id, source])
  if (err) {
    return (
      <div className="tinvalid">
        <pre>
          <code>{source}</code>
        </pre>
        <div className="tinvalid-errors">
          <b>This diagram does not render</b>
          <div className="small">{err}</div>
        </div>
      </div>
    )
  }
  return <iframe key={source} ref={frame} className="mermaid-frame" sandbox="allow-scripts" src="/mermaid/frame.html" title="Diagram" style={{ height }} />
}
