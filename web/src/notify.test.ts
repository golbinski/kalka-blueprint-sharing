import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Notice } from './api'
import { noticeHref, notifyNotice } from './notify'

const notice: Notice = { kind: 'comments', title: 'Parcel lockers', message: 'new comment on “Parcel lockers” from courier@parcels.example' } as Notice

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

  it('notifies only while the page is hidden; the page shows it otherwise', () => {
    vi.stubGlobal('Notification', FakeNotification)
    hidden(false)
    expect(notifyNotice(notice)).toBe(false)
    expect(FakeNotification.made).toHaveLength(0)

    hidden(true)
    expect(notifyNotice(notice)).toBe(true)
    expect(FakeNotification.made).toHaveLength(1)
    expect(FakeNotification.made[0].title).toBe('kalka · Parcel lockers')
  })

  it('does nothing without permission', () => {
    FakeNotification.permission = 'denied'
    vi.stubGlobal('Notification', FakeNotification)
    hidden(true)
    expect(notifyNotice(notice)).toBe(false)
    expect(FakeNotification.made).toHaveLength(0)
    FakeNotification.permission = 'granted'
  })
})

describe('noticeHref', () => {
  it('leads where the shell does', () => {
    expect(noticeHref({ kind: 'comments', title: '', message: '', path: 'lending/loans.md', team: 'lending' })).toBe('#/doc/lending%2Floans.md')
    expect(noticeHref({ kind: 'join-failed', title: '', message: '', team: 'lending' })).toBe('#/join')
    expect(noticeHref({ kind: 'join-done', title: '', message: '', team: 'lending desk' })).toBe('#/t/lending%20desk/')
    expect(noticeHref({ kind: 'transfer', title: '', message: '', team: 'lending' })).toBe('#/needs')
    expect(noticeHref({ kind: 'joined', title: '', message: '', team: 'lending' })).toBe('#/t/lending/team')
    expect(noticeHref({ kind: 'joined', title: '', message: '' })).toBe('')
  })
})
