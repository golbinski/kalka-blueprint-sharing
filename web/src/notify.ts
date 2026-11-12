// Desktop notifications from the page (127.0.0.1 is a secure origin). They
// cover the case the daemon cannot see: a kalka tab is open, so the daemon
// shows no popup of its own, but the tab is in the background and the
// prompt's modal goes unseen.
import type { Prompt } from './api'

const shown = new Map<string, Notification>()

export function notificationsSupported(): boolean {
  return typeof window !== 'undefined' && 'Notification' in window
}

export function permission(): NotificationPermission | 'unsupported' {
  return notificationsSupported() ? Notification.permission : 'unsupported'
}

export async function requestPermission(): Promise<NotificationPermission | 'unsupported'> {
  if (!notificationsSupported()) return 'unsupported'
  return Notification.requestPermission()
}

// promptSummary says what waits, without the question the modal asks.
export function promptSummary(p: Prompt): { title: string; body: string } {
  switch (p.kind) {
    case 'verify-code':
      return { title: `${p.peer} is syncing with you`, body: 'First sync: compare the verification code with them.' }
    case 'accept-new':
      return { title: `New blueprint from ${p.peer}`, body: `“${p.title ?? ''}” is waiting for your answer.` }
    case 'accept-handoff':
      return { title: `Handoff from ${p.peer}`, body: `${p.peer} wants to hand “${p.title ?? ''}” over to you.` }
    default:
      return { title: `${p.peer} is waiting`, body: p.message }
  }
}

// notifyPrompt shows a notification for p if the page is hidden and the
// user allowed notifications. Clicking it brings the tab forward.
export function notifyPrompt(p: Prompt): void {
  if (permission() !== 'granted' || document.visibilityState === 'visible' || shown.has(p.id)) return
  const { title, body } = promptSummary(p)
  try {
    const n = new Notification(title, { body, tag: 'kalka-' + p.id, requireInteraction: true })
    n.onclick = () => {
      window.focus()
      n.close()
    }
    n.onclose = () => shown.delete(p.id)
    shown.set(p.id, n)
  } catch {
    /* some browsers allow notifications only from a service worker */
  }
}

// closePrompt removes the notification once the prompt is answered or gone.
export function closePrompt(id: string): void {
  shown.get(id)?.close()
  shown.delete(id)
}
