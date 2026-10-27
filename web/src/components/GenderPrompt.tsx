import { useState } from 'react'
import * as api from '../api'
import { useGateway } from '../gateway'
import type { Gender } from '../types'
import { Modal } from './Modal'

const CHOICES: { value: Gender; label: string; emoji: string }[] = [
  { value: 'male', label: 'Male', emoji: '👦' },
  { value: 'female', label: 'Female', emoji: '👧' },
  { value: 'prefer_not_to_say', label: 'Prefer not to say', emoji: '🤐' },
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
      setError(err instanceof Error ? err.message : String(err))
      setSaving(false)
    }
  }

  return (
    <Modal onClose={onClose} label="Choose your gender">
      <div className="modal__emoji">🔒</div>
      <h2>One last thing</h2>
      <p>
        You can choose this <b>only once</b>, so please don't fake it and choose carefully. It's what makes gender-filtered matching fair for everyone.
      </p>
      <div className="choices" role="radiogroup" aria-label="Gender">
        {CHOICES.map((c) => (
          <button key={c.value} role="radio" aria-checked={choice === c.value} className={`choice ${choice === c.value ? 'choice--on' : ''}`} onClick={() => setChoice(c.value)}>
            <span className="choice__emoji">{c.emoji}</span>
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
