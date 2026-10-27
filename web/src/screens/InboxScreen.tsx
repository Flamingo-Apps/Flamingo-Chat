import { useEffect, useState } from 'react'
import { EmptyChat, Workspace } from '../components/Workspace'
import { useGateway } from '../gateway'
import { ChatView } from './ChatView'

// Inbox is kept direct chats (docs/ARCHITECTURE.md §10) - a direct room
// that survived both sides voting keep_chat (DEC-0031).
export function InboxScreen() {
  const { state, loadRooms } = useGateway()
  const [selectedRoomId, setSelectedRoomId] = useState<string | null>(null)

  useEffect(() => {
    loadRooms().catch(() => {})
  }, [loadRooms])

  const keptRooms = state.rooms.filter((r) => r.type === 'direct' && r.kept)
  const selected = keptRooms.find((r) => r.id === selectedRoomId)
  const when = (unix: number) => new Date(unix * 1000).toLocaleDateString()

  const list = (
    <div className="panel">
      <h1 className="panel__title">Inbox</h1>
      <p className="panel__sub">Chats you and someone else both chose to keep.</p>
      <ul className="room-list">
        {keptRooms.map((r) => (
          <li key={r.id}>
            <button className={`room ${r.id === selectedRoomId ? 'room--on' : ''}`} onClick={() => setSelectedRoomId(r.id)}>
              <span className="avatar avatar--group">♥</span>
              <span className="room__name">Chat from {when(r.created_at_unix)}</span>
            </button>
          </li>
        ))}
        {keptRooms.length === 0 && <li className="hint">Nothing here yet. Press Keep during a random chat to save it.</li>}
      </ul>
    </div>
  )

  const chat = selected ? (
    <ChatView roomId={selected.id} title="Kept conversation" subtitle={when(selected.created_at_unix)} onBack={() => setSelectedRoomId(null)} />
  ) : (
    <EmptyChat mood="sleepy" title="Your kept chats live here" text="When you both press Keep, the chat moves to your Inbox." />
  )

  return <Workspace list={list} chat={chat} hasChat={Boolean(selected)} />
}
