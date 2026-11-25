// Context for an agent (spec 6.18, 11.1): the focused context, the same
// as kalka context, with Copy and a Full toggle.
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api, type Doc } from './api'
import { AgentContextDialog } from './DocHeader'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

let host: HTMLDivElement
let root: Root
beforeEach(() => {
  HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
    this.open = true
  }
  HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
    this.open = false
  }
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
  vi.restoreAllMocks()
})

describe('Context for an agent', () => {
  it('shows the focused context, and the full one on Full', async () => {
    const get = vi.spyOn(api, 'agentContext').mockImplementation(async (_p, full) => ({ context: {}, markdown: full ? '# full' : '# focused' }))
    const doc = { rel: 'parcels/redelivery.md' } as Doc
    await act(async () => root.render(<AgentContextDialog doc={doc} onClose={() => {}} />))
    expect(get).toHaveBeenLastCalledWith('parcels/redelivery.md', false)
    expect(document.querySelector('.agent-context')!.textContent).toBe('# focused')
    expect(document.body.textContent).toContain('kalka context parcels/redelivery.md.')
    const box = [...document.querySelectorAll('label')].find((l) => l.textContent?.includes('Full'))!.querySelector('input')!
    await act(async () => box.click())
    expect(get).toHaveBeenLastCalledWith('parcels/redelivery.md', true)
    expect(document.querySelector('.agent-context')!.textContent).toBe('# full')
    expect(document.body.textContent).toContain('kalka context parcels/redelivery.md --full.')
    expect([...document.querySelectorAll('button')].map((b) => b.textContent)).toContain('Copy')
  })
})
