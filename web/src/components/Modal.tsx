import { useEffect, type ReactNode } from 'react'

// A centred card over a dimmed page. `onClose` is optional: a modal that
// must be answered (nothing to dismiss to) simply leaves it out.
export function Modal({ children, onClose, label }: { children: ReactNode; onClose?: () => void; label: string }) {
  useEffect(() => {
    if (!onClose) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div className="modal" onMouseDown={(e) => e.target === e.currentTarget && onClose?.()}>
      <div className="modal__card" role="dialog" aria-modal="true" aria-label={label}>
        {children}
      </div>
    </div>
  )
}
