import { useEffect, useState } from 'react'
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

  if (selected) {
    return <ChatView roomId={selected.id} title="Kept conversation" />
  }

  return (
    <div className="inbox-screen">
      <h2>Inbox</h2>
      <ul className="room-list">
        {keptRooms.map((r) => (
          <li key={r.id}>
            <button onClick={() => setSelectedRoomId(r.id)}>Conversation from {new Date(r.created_at_unix * 1000).toLocaleDateString()}</button>
          </li>
        ))}
        {keptRooms.length === 0 && <li className="hint">No kept conversations yet.</li>}
      </ul>
    </div>
  )
}
