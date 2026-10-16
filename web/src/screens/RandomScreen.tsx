import { useEffect, useState } from 'react'
import * as api from '../api'
import { useGateway } from '../gateway'
import { GRACE_WINDOW_MS } from '../state'
import type { Preference } from '../types'
import { ChatView } from './ChatView'

export function RandomScreen() {
  const { state, send } = useGateway()
  const [preference, setPreference] = useState<Preference>('any')
  const [reportOpen, setReportOpen] = useState(false)
  const account = state.account

  // Gender-filtered matching needs a verified account with a chosen
  // gender (services/matching/internal/server/server.go's
  // checkGenderGate) - locked out here rather than letting the request
  // round-trip to a PermissionDenied error.
  const canFilterByGender = Boolean(account?.google_linked && account?.gender_chosen)

  if (state.currentMatch?.partnerLeftAt) {
    return <PartnerLeftPrompt roomId={state.currentMatch.roomId} partnerLeftAt={state.currentMatch.partnerLeftAt} kept={state.currentMatch.kept} />
  }

  if (state.currentMatch) {
    const match = state.currentMatch
    return (
      <ChatView
        roomId={match.roomId}
        title={match.partnerPseudonym}
        onReport={() => setReportOpen(true)}
        onBlock={() => send({ type: 'match_end' })}
        headerExtra={
          <>
            <button onClick={() => send({ type: 'keep_chat', room_id: match.roomId })}>
              {match.kept ? 'Kept' : 'Keep chatting'}
            </button>
            <button onClick={() => send({ type: 'match_next' })}>Next</button>
            <button onClick={() => send({ type: 'match_end' })}>End</button>
          </>
        }
      />
    )
  }

  return (
    <div className="random-screen">
      <h2>Random chat</h2>
      <div className="preference-picker">
        <button className={preference === 'any' ? 'active' : ''} onClick={() => setPreference('any')}>
          Anyone
        </button>
        <button
          className={preference === 'male' ? 'active' : ''}
          disabled={!canFilterByGender}
          onClick={() => setPreference('male')}
        >
          Male
        </button>
        <button
          className={preference === 'female' ? 'active' : ''}
          disabled={!canFilterByGender}
          onClick={() => setPreference('female')}
        >
          Female
        </button>
      </div>
      {!canFilterByGender && <p className="hint">Get verified and choose a gender to filter matches.</p>}

      {state.matching ? (
        <>
          <p>Looking for a match...</p>
          <button onClick={() => send({ type: 'match_cancel' })}>Cancel</button>
        </>
      ) : (
        <button onClick={() => send({ type: 'match_request', preference })}>Find a match</button>
      )}

      {reportOpen && <ReportDialog onClose={() => setReportOpen(false)} />}
    </div>
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
    <div className="dialog">
      {submitted ? (
        <p>Report filed. Thank you.</p>
      ) : (
        <form onSubmit={submit}>
          <textarea value={reason} onChange={(e) => setReason(e.target.value)} placeholder="What happened?" required />
          <button type="submit">Submit report</button>
        </form>
      )}
      <button onClick={onClose}>Close</button>
    </div>
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
      <div className="partner-left-prompt">
        <p>Saved. You can find this conversation in Inbox.</p>
        <button onClick={() => expireGraceWindow(roomId)}>Back to Random</button>
      </div>
    )
  }

  return (
    <div className="partner-left-prompt">
      <p>Your partner left this chat.</p>
      <p>Keep it anyway? ({Math.ceil(remainingMs / 1000)}s)</p>
      <button onClick={() => send({ type: 'keep_chat', room_id: roomId })}>Keep this chat</button>
      <button onClick={() => expireGraceWindow(roomId)}>Discard</button>
    </div>
  )
}
