import { Component, StrictMode, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import './styles.css'

// Boundary keeps a rendering bug from leaving a blank page.
class Boundary extends Component<{ children: ReactNode }, { error?: Error }> {
  state: { error?: Error } = {}
  static getDerivedStateFromError(error: Error) {
    return { error }
  }
  render() {
    if (!this.state.error) return this.props.children
    return (
      <div className="page narrow">
        <h1>Something went wrong in the page</h1>
        <p className="muted">Your files are not affected. Reload the page; if it happens again, the details below help.</p>
        <pre className="mono small">{String(this.state.error.stack ?? this.state.error)}</pre>
        <button type="button" onClick={() => location.reload()}>
          Reload
        </button>
      </div>
    )
  }
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Boundary>
      <App />
    </Boundary>
  </StrictMode>,
)
