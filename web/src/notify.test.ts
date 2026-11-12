import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Prompt } from './api'
import { closePrompt, notifyPrompt, promptSummary } from './notify'

const prompt = (id: string, kind: Prompt['kind'] = 'accept-new'): Prompt => ({
  id,
  created: '',
  kind,
  peer: 'alice@corp.com',
  title: 'Import pipeline refactor',
  message: 'alice@corp.com sends you a new blueprint. Accept it?',
})

class FakeNotification {
  static permission = 'granted'
  static made: FakeNotification[] = []
  closed = false
  onclick: (() => void) | null = null
  onclose: (() => void) | null = null
  constructor(
    public title: string,
    public opts: NotificationOptions,
  ) {
    FakeNotification.made.push(this)
  }
  close() {
    this.closed = true
    this.onclose?.()
  }
}

function hidden(h: boolean) {
  Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => (h ? 'hidden' : 'visible') })
}

describe('page notifications', () => {
  afterEach(() => {
    FakeNotification.made = []
    vi.unstubAllGlobals()
  })

  it('says what waits without asking the question', () => {
    const s = promptSummary(prompt('1', 'accept-handoff'))
    expect(s.title).toContain('alice@corp.com')
    expect(s.body).toContain('“Import pipeline refactor”')
    expect(s.body).not.toContain('?')
  })

  it('notifies only while the page is hidden, once per prompt, and closes with the prompt', () => {
    vi.stubGlobal('Notification', FakeNotification)
    hidden(false)
    notifyPrompt(prompt('a'))
    expect(FakeNotification.made).toHaveLength(0)

    hidden(true)
    notifyPrompt(prompt('a'))
    notifyPrompt(prompt('a'))
    expect(FakeNotification.made).toHaveLength(1)
    closePrompt('a')
    expect(FakeNotification.made[0].closed).toBe(true)
  })

  it('does nothing without permission', () => {
    FakeNotification.permission = 'denied'
    vi.stubGlobal('Notification', FakeNotification)
    hidden(true)
    notifyPrompt(prompt('b'))
    expect(FakeNotification.made).toHaveLength(0)
    FakeNotification.permission = 'granted'
  })
})
