// Desktop notifications from the page (127.0.0.1 is a secure origin). They
// cover the case the daemon cannot see: a kalka tab is open, so the daemon
// shows no notification of its own, but the tab is in the background.
import type { Notice } from './api'
import { inDesktopWindow } from './shell'

// In the desktop window the shell shows OS notifications instead.
export function notificationsSupported(): boolean {
  return typeof window !== 'undefined' && 'Notification' in window && !inDesktopWindow()
}

export function permission(): NotificationPermission | 'unsupported' {
  return notificationsSupported() ? Notification.permission : 'unsupported'
}

export async function requestPermission(): Promise<NotificationPermission | 'unsupported'> {
  if (!notificationsSupported()) return 'unsupported'
  return Notification.requestPermission()
}

// notifyNotice shows a notice (spec 22.4) as a browser notification when
// the page is hidden; it returns false when the page should show it itself.
export function notifyNotice(n: Notice): boolean {
  if (permission() !== 'granted' || document.visibilityState === 'visible') return false
  try {
    const note = new Notification(n.title ? `kalka · ${n.title}` : 'kalka', { body: n.message, tag: 'kalka-notice-' + n.kind + (n.path ?? '') })
    note.onclick = () => {
      window.focus()
      const href = noticeHref(n)
      if (href) location.hash = href
      note.close()
    }
    return true
  } catch {
    return false
  }
}

// noticeHref is where a notice leads: the document, the joins for a failed
// join, the team it is about, or Needs you. internal/desktop NoticeHash is
// the same mapping for the shell's notifications.
export function noticeHref(n: Notice): string {
  const team = '#/t/' + encodeURIComponent(n.team ?? '') + '/'
  if (n.path) return '#/doc/' + encodeURIComponent(n.path)
  if (n.kind === 'join-failed') return '#/join'
  if (n.kind === 'update') return '#/settings'
  if (!n.team) return ''
  if (n.kind === 'join-done') return team
  if (n.kind === 'transfer') return '#/needs'
  return team + 'team'
}
