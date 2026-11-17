// A small action menu (spec 11.1): "More" on a blueprint, "⋯" on a team
// member. A button that opens a list of actions, each with an optional line
// of consequence; Up and Down move, Escape closes and returns to the
// button, a click outside closes it. Destructive items come last, in red,
// after a separator.
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { act, type ActionId } from './actions'

export interface MenuItem {
  label: string
  sub?: string
  danger?: boolean
  /** Draw a separator above the item. */
  sep?: boolean
  /** The action this item carries, for the parity check. */
  action?: ActionId
  onSelect: () => void
}

export function Menu({ label, items, title, className = '' }: { label: ReactNode; items: MenuItem[]; title?: string; className?: string }) {
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  const button = useRef<HTMLButtonElement>(null)
  const list = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const outside = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', outside)
    list.current?.querySelector<HTMLElement>('button')?.focus()
    return () => document.removeEventListener('mousedown', outside)
  }, [open])
  if (items.length === 0) return null
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape' && open) {
      e.preventDefault()
      e.stopPropagation()
      setOpen(false)
      button.current?.focus()
      return
    }
    if (!open || (e.key !== 'ArrowDown' && e.key !== 'ArrowUp')) return
    const all = [...(list.current?.querySelectorAll<HTMLElement>('button') ?? [])]
    e.preventDefault()
    const i = all.indexOf(document.activeElement as HTMLElement)
    all[e.key === 'ArrowDown' ? (i + 1) % all.length : (i - 1 + all.length) % all.length]?.focus()
  }
  return (
    <div className={'menu ' + className} ref={box} onKeyDown={onKey}>
      <button ref={button} type="button" aria-expanded={open} aria-haspopup="true" title={title} onClick={() => setOpen(!open)}>
        {label}
      </button>
      {open && (
        <div className="menu-list" ref={list}>
          {items.map((it, i) => (
            <div key={i}>
              {it.sep && <hr />}
              <button
                type="button"
                className={'menu-item' + (it.danger ? ' danger-text' : '')}
                {...(it.action ? act(it.action) : {})}
                onClick={() => {
                  setOpen(false)
                  it.onSelect()
                }}
              >
                {it.label}
                {it.sub && <span className="menu-sub">{it.sub}</span>}
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
