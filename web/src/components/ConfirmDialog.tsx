import { Mascot, type Mood } from './Mascot'
import { Modal } from './Modal'

// A question with a clear "do it" and an easy "never mind", for actions that
// cannot be taken back. Escape and clicking outside both mean "never mind".
export function ConfirmDialog({
  mood = 'worried',
  title,
  text,
  confirmLabel,
  onConfirm,
  onCancel,
  busy,
}: {
  mood?: Mood
  title: string
  text: string
  confirmLabel: string
  onConfirm: () => void
  onCancel: () => void
  busy?: boolean
}) {
  return (
    <Modal onClose={onCancel} label={title}>
      <Mascot mood={mood} size={104} />
      <h2>{title}</h2>
      <p>{text}</p>
      <div className="modal__actions">
        <button className="btn" onClick={onConfirm} disabled={busy}>
          {busy ? 'One moment...' : confirmLabel}
        </button>
        <button className="link-button" onClick={onCancel}>
          Never mind
        </button>
      </div>
    </Modal>
  )
}
