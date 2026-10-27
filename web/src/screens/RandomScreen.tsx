import { useEffect, useState } from 'react'
import * as api from '../api'
import { FlagIcon, HeartIcon, LockIcon, PowerIcon, SkipIcon } from '../components/Icons'
import { Mascot } from '../components/Mascot'
import { Modal } from '../components/Modal'
import { EmptyChat, Workspace } from '../components/Workspace'
import { useGateway } from '../gateway'
import { GRACE_WINDOW_MS } from '../state'
import type { Preference } from '../types'
import { useUi } from '../ui'
import { ChatView } from './ChatView'

const CHOICES: { value: Preference; label: string }[] = [
  { value: 'any', label: 'Anyone' },
  { value: 'male', label: 'Male' },
  { value: 'female', label: 'Female' },
]

export function RandomScreen() {
  const { state, send } = useGateway()
  const { askForIdentity } = useUi()
  const [preference, setPreference] = useState<Preference>('any')
  const [reportOpen, setReportOpen] = useState(false)
  const account = state.account
  const match = state.currentMatch

  // Gender-filtered matching needs a verified account with a chosen
  // gender (services/matching/internal/server/server.go's
  // checkGenderGate). Until then the filters are visibly locked, and
  // reaching for one opens the card that explains how to get access
  // rather than letting the request round-trip to a PermissionDenied.
  const canFilterByGender = Boolean(account?.google_linked && account?.gender_chosen)

  function choose(value: Preference) {
    if (value !== 'any' && !canFilterByGender) return askForIdentity()
    setPreference(value)
  }

  const list = (
    <div className="panel">
      <h1 className="panel__title">Random chat</h1>
      <p className="panel__sub">Talk to a random student. You both stay anonymous.</p>

      <h3 className="panel__label">Who do you want to talk to?</h3>
      <div className="chips">
        {CHOICES.map((c) => {
          const locked = c.value !== 'any' && !canFilterByGender
          return (
            <button key={c.value} className={`chip ${preference === c.value ? 'chip--on' : ''} ${locked ? 'chip--locked' : ''}`} onClick={() => choose(c.value)}>
              {locked && <LockIcon />}
              {c.label}
            </button>
          )
        })}
      </div>
      {!canFilterByGender && <p className="hint">Filtering by gender needs a verified identity. Tap one to see how.</p>}

      {state.matching ? (
        <div className="searching">
          <div className="searching__row">
            <span className="spinner" />
            <span>Looking for someone...</span>
          </div>
          <button className="btn btn--ghost" onClick={() => send({ type: 'match_cancel' })}>
            Cancel
          </button>
        </div>
      ) : (
        <button className="btn btn--big" disabled={Boolean(match)} onClick={() => send({ type: 'match_request', preference })}>
          Find a match
        </button>
      )}
      {state.lastError && (
        <p className="error-text" role="alert">
          {state.lastError}
        </p>
      )}
    </div>
  )

  let chat
  if (match?.partnerLeftAt) {
    chat = <PartnerLeftPrompt roomId={match.roomId} partnerLeftAt={match.partnerLeftAt} kept={match.kept} />
  } else if (match) {
    chat = (
      <ChatView
        roomId={match.roomId}
        title={match.partnerPseudonym}
        subtitle="Anonymous student"
        headerExtra={
          <>
            <button className="btn btn--sm btn--ghost" onClick={() => send({ type: 'keep_chat', room_id: match.roomId })}>
              <HeartIcon /> {match.kept ? 'Kept' : 'Keep'}
            </button>
            <button className="btn btn--sm btn--ghost" onClick={() => send({ type: 'match_next' })}>
              <SkipIcon /> Next
            </button>
            <button className="btn btn--sm btn--ghost" onClick={() => send({ type: 'match_end' })}>
              <PowerIcon /> End
            </button>
            <button className="btn btn--sm btn--ghost" onClick={() => setReportOpen(true)}>
              <FlagIcon /> Report
            </button>
          </>
        }
      />
    )
  } else if (state.matching) {
    chat = (
      <EmptyChat mood="search" title="Finding someone for you" text="Hang tight, this only takes a few seconds.">
        <div className="pulse" />
      </EmptyChat>
    )
  } else {
    chat = <EmptyChat mood="hello" title="Say hi to someone new" text="Pick who you'd like to meet, then tap Find a match." />
  }

  return (
    <>
      <Workspace list={list} chat={chat} hasChat={Boolean(match) || state.matching} />
      {reportOpen && match && <ReportDialog onClose={() => setReportOpen(false)} />}
    </>
  )
}

function ReportDialog({ onClose }: { onClose: () => void }) {
  const { state } = useGateway()
  const [reason, setReason] = useState('')
  const [submitted, setSubmitted] = useState(false)
  const match = state.currentMatch

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!state.token || !match) return
    await api.fileReport(state.token, match.partnerAccountId, reason, match.roomId)
    setSubmitted(true)
  }

  return (
    <Modal onClose={onClose} label="Report">
      {submitted ? (
        <>
          <Mascot mood="cheer" size={110} />
          <h2>Thank you</h2>
          <p>You're helping keep this place kind.</p>
        </>
      ) : (
        <form onSubmit={submit}>
          <Mascot mood="peek" size={110} />
          <h2>Report this person</h2>
          <p>Tell us what happened. Only our safety team sees this.</p>
          <textarea value={reason} onChange={(e) => setReason(e.target.value)} placeholder="What happened?" required rows={4} />
          <div className="modal__actions">
            <button className="btn" type="submit">
              Send report
            </button>
          </div>
        </form>
      )}
      <div className="modal__actions">
        <button className="link-button" onClick={onClose}>
          Close
        </button>
      </div>
    </Modal>
  )
}

// PartnerLeftPrompt is shown instead of instantly dropping the chat when
// the partner leaves/skips before both sides voted to keep (DEC-0091).
// Rather than the chat just vanishing, the person who stayed gets a
// short window to vote keep on their own - the partner is confirmed
// gone and can never complete a mutual vote otherwise, so without this
// "keep" would be permanently impossible from this point on.
function PartnerLeftPrompt({ roomId, partnerLeftAt, kept }: { roomId: string; partnerLeftAt: number; kept: boolean }) {
  const { send, expireGraceWindow } = useGateway()
  const [remainingMs, setRemainingMs] = useState(() => Math.max(0, partnerLeftAt + GRACE_WINDOW_MS - Date.now()))

  useEffect(() => {
    if (kept) return
    const interval = setInterval(() => {
      const left = partnerLeftAt + GRACE_WINDOW_MS - Date.now()
      setRemainingMs(Math.max(0, left))
      if (left <= 0) {
        clearInterval(interval)
        expireGraceWindow(roomId)
      }
    }, 250)
    return () => clearInterval(interval)
  }, [kept, partnerLeftAt, roomId, expireGraceWindow])

  if (kept) {
    return (
      <EmptyChat mood="cheer" title="Saved!" text="You can find this chat in your Inbox.">
        <button className="btn" onClick={() => expireGraceWindow(roomId)}>
          Back to Random
        </button>
      </EmptyChat>
    )
  }

  return (
    <EmptyChat mood="worried" title="Your partner left" text={`Want to keep this chat anyway? You have ${Math.ceil(remainingMs / 1000)}s.`}>
      <div className="modal__actions">
        <button className="btn" onClick={() => send({ type: 'keep_chat', room_id: roomId })}>
          Keep this chat
        </button>
        <button className="btn btn--ghost" onClick={() => expireGraceWindow(roomId)}>
          Discard
        </button>
      </div>
    </EmptyChat>
  )
}
