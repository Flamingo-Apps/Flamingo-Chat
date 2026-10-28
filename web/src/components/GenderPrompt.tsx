import { useState } from 'react'
import * as api from '../api'
import { friendlyError } from '../api'
import { useGateway } from '../gateway'
import type { Gender } from '../types'
import { Mascot } from './Mascot'
import { Modal } from './Modal'

const CHOICES: { value: Gender; label: string }[] = [
  { value: 'male', label: 'Male' },
  { value: 'female', label: 'Female' },
  { value: 'prefer_not_to_say', label: 'Prefer not to say' },
]

// The one-time gender choice after a first Google sign-in. The backend
// only lets it be set once (DEC-0028), so the card says so plainly and
// makes the person press a separate button to lock it in.
export function GenderPrompt({ onClose }: { onClose: () => void }) {
  const { state, updateAccount } = useGateway()
  const [choice, setChoice] = useState<Gender | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function lockIn() {
    if (!state.token || !choice) return
    setSaving(true)
    setError(null)
    try {
      updateAccount(await api.setGender(state.token, choice))
      onClose()
    } catch (err) {
      setError(friendlyError(err))
      setSaving(false)
    }
  }

  return (
    <Modal onClose={onClose} label="Choose your gender">
      <Mascot mood="hello" size={108} />
      <h2>Last step, promise</h2>
      <p>
        Pick what fits you. It helps everyone get matched fairly. You can only set it <b>once</b>, so take a second.
      </p>
      <div className="choices" role="radiogroup" aria-label="Gender">
        {CHOICES.map((c) => (
          <button key={c.value} role="radio" aria-checked={choice === c.value} className={`choice ${choice === c.value ? 'choice--on' : ''}`} onClick={() => setChoice(c.value)}>
            {c.label}
          </button>
        ))}
      </div>
      {error && <p className="error-text">{error}</p>}
      <div className="modal__actions">
        <button className="btn" disabled={!choice || saving} onClick={lockIn}>
          {saving ? 'Saving...' : 'Lock it in'}
        </button>
        <button className="link-button" onClick={onClose}>
          Decide later
        </button>
      </div>
    </Modal>
  )
}
