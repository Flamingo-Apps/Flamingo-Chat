import { useEffect, useRef, useState } from 'react'
import { Avatar } from '../components/Avatar'
import { BackIcon, SendIcon } from '../components/Icons'
import { clockTime } from '../format'
import { useGateway } from '../gateway'

interface ChatViewProps {
  roomId: string
  title: string
  subtitle?: string
  // A group room has no single anonymous face, so it shows "#" instead.
  group?: boolean
  onBack?: () => void
  headerExtra?: React.ReactNode
}

// ChatView renders one room's messages and a send box - shared by the
// active match (Random) and a selected group/kept room (Channels/Inbox).
// Direct-match-only actions (Next, Keep, End, Report) are passed in as
// headerExtra by the caller rather than built in here, since a group
// room has none of them.
export function ChatView({ roomId, title, subtitle, group, onBack, headerExtra }: ChatViewProps) {
  const { state, send, loadMessages } = useGateway()
  const [body, setBody] = useState('')
  const messages = state.messagesByRoom[roomId] ?? []
  const bottomRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    loadMessages(roomId).catch(() => {})
  }, [roomId, loadMessages])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: 'end' })
  }, [messages.length])

  function sendMessage(e: React.FormEvent) {
    e.preventDefault()
    const text = body.trim()
    if (!text) return
    send({ type: 'send_message', room_id: roomId, client_msg_id: crypto.randomUUID(), body: text })
    setBody('')
  }

  return (
    <div className="chat">
      <header className="chat__header">
        {onBack && (
          <button className="icon-button chat__back" onClick={onBack} aria-label="Back">
            <BackIcon />
          </button>
        )}
        {group ? <span className="avatar avatar--group">#</span> : <Avatar name={title} size={44} />}
        <div className="chat__who">
          <div className="chat__title">{title}</div>
          {subtitle && <div className="chat__subtitle">{subtitle}</div>}
        </div>
        <div className="chat__actions">{headerExtra}</div>
      </header>

      <div className="chat__messages">
        {messages.length === 0 && <p className="chat__hint">Say hi 👋 The first message is the hardest.</p>}
        {messages.map((m, i) => {
          const mine = m.sender_account_id === state.account?.id
          const startsRun = i === 0 || messages[i - 1].sender_account_id !== m.sender_account_id
          return (
            <div key={m.message_id} className={`msg ${mine ? 'msg--mine' : ''} ${startsRun ? 'msg--first' : ''}`}>
              {!mine && startsRun && <span className="msg__sender">{m.sender_pseudonym}</span>}
              <div className={`bubble ${mine ? 'bubble--mine' : 'bubble--theirs'}`}>
                <span>{m.body}</span>
                <time className="bubble__time">{clockTime(m.sent_at_unix)}</time>
              </div>
            </div>
          )
        })}
        <div ref={bottomRef} />
      </div>

      <form className="chat__composer" onSubmit={sendMessage}>
        <input value={body} onChange={(e) => setBody(e.target.value)} placeholder="Type a message" maxLength={2000} aria-label="Message" />
        <button className="btn btn--round" type="submit" aria-label="Send" disabled={!body.trim()}>
          <SendIcon />
        </button>
      </form>
    </div>
  )
}
