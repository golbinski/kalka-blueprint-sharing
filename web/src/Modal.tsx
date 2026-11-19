// Modal is the one dialog of the page: a native <dialog> opened with
// showModal(), so the page behind it is inert, Tab stays inside, focus
// moves to the first field, Escape closes it (unless busy), and focus goes
// back to what opened it. wide is for code and other large content.
import { useEffect, useId, useRef } from 'react'

export function Modal({ title, busy, wide, onClose, children }: { title: string; busy?: boolean; wide?: boolean; onClose: () => void; children: React.ReactNode }) {
  const ref = useRef<HTMLDialogElement>(null)
  const id = useId()
  const busyRef = useRef(busy)
  busyRef.current = busy
  const closeRef = useRef(onClose)
  closeRef.current = onClose
  useEffect(() => {
    const d = ref.current
    const opener = document.activeElement as HTMLElement | null
    if (d && !d.open) {
      try {
        d.showModal()
      } catch {
        d.setAttribute('open', '')
      }
      const first = d.querySelector<HTMLElement>('[autofocus], input:not([type=hidden]), textarea, select, button.primary, button')
      first?.focus()
    }
    return () => {
      d?.open && d.close()
      opener?.focus?.()
    }
  }, [])
  return (
    <dialog
      ref={ref}
      className={wide ? 'modal wide' : 'modal'}
      aria-labelledby={id}
      aria-modal="true"
      onCancel={(e) => {
        e.preventDefault()
        if (!busyRef.current) closeRef.current()
      }}
      onClick={(e) => e.target === e.currentTarget && !busyRef.current && closeRef.current()}
    >
      <h2 id={id}>{title}</h2>
      {children}
    </dialog>
  )
}
