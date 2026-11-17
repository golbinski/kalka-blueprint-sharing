// Draws one ```mermaid block inside a sandboxed frame (opaque origin, see
// MermaidPath in internal/daemon/web.go). The page sends the source with
// postMessage; this frame answers with its height or an error.
'use strict'
;(function () {
  var mermaid = window.mermaid
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: 'strict',
    // The frame follows the system's dark mode, as the page does.
    theme: window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'neutral',
    fontFamily: "system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif",
  })
  var out = document.getElementById('out')
  var n = 0
  function send(m) {
    m.kalka = 'mermaid'
    window.parent.postMessage(m, '*')
  }
  window.addEventListener('message', function (ev) {
    if (ev.source !== window.parent) return
    var m = ev.data
    if (!m || m.kalka !== 'mermaid' || m.type !== 'render' || typeof m.source !== 'string') return
    n++
    mermaid.render('kalka-m' + n, m.source).then(
      function (r) {
        out.innerHTML = r.svg
        send({ type: 'size', id: m.id, height: Math.ceil(document.documentElement.scrollHeight) })
      },
      function (err) {
        send({ type: 'error', id: m.id, message: String((err && err.message) || err).slice(0, 500) })
      },
    )
  })
  send({ type: 'ready' })
})()
