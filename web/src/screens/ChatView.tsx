import { useEffect, useRef, useState } from 'react'
import { useGateway } from '../gateway'

interface ChatViewProps {
  roomId: string
  title: string
  onReport?: () => void
  onBlock?: () => void
  headerExtra?: React.ReactNode
}

// ChatView renders one room's messages and a send box - shared by the
// active match (Random) and a selected group/kept room (Channels/Inbox).
// Direct-match-only actions (Next, Keep, End) are passed in as
// headerExtra by the caller rather than built in here, since a group
// room has none of them.
export function ChatView({ roomId, title, onReport, onBlock, headerExtra }: ChatViewProps) {
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
    <div className="chat-view">
      <div className="chat-view__header">
        <h2>{title}</h2>
        <div className="chat-view__actions">
          {headerExtra}
          {onReport && <button onClick={onReport}>Report</button>}
          {onBlock && <button onClick={onBlock}>Block</button>}
        </div>
      </div>
      <div className="chat-view__messages">
        {messages.map((m) => (
          <div key={m.message_id} className={m.sender_account_id === state.account?.id ? 'message message--mine' : 'message'}>
            <span className="message__sender">{m.sender_pseudonym}</span>
            <span className="message__body">{m.body}</span>
          </div>
        ))}
        <div ref={bottomRef} />
      </div>
      <form className="chat-view__composer" onSubmit={sendMessage}>
        <input value={body} onChange={(e) => setBody(e.target.value)} placeholder="Message" />
        <button type="submit">Send</button>
      </form>
    </div>
  )
}
