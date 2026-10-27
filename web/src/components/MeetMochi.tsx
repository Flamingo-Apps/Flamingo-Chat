import { Mascot } from './Mascot'
import { Modal } from './Modal'

// Shown once per device, the first time someone gets into the app, so the
// friendly face on every later prompt is not a stranger. Three lines, no
// more: nobody reads a long welcome.
export function MeetMochi({ onClose }: { onClose: () => void }) {
  return (
    <Modal onClose={onClose} label="Meet Mochi">
      <Mascot mood="cheer" size={130} />
      <h2>Hi, I'm Mochi!</h2>
      <p>I live here. Here's all you need to know:</p>
      <ul className="tips">
        <li>
          <b>Find a match</b> to chat with a random student.
        </li>
        <li>
          You're <b>anonymous</b>. Say hi and be kind.
        </li>
        <li>
          Someone not kind? <b>Report</b> them and our safety team will look.
        </li>
      </ul>
      <div className="modal__actions">
        <button className="btn" onClick={onClose}>
          Got it
        </button>
      </div>
    </Modal>
  )
}
