import { useEffect, useState } from 'react'
import * as api from '../api'
import { useGateway } from '../gateway'
import { ChatView } from './ChatView'

export function ChannelsScreen() {
  const { state, loadRooms } = useGateway()
  const [selectedRoomId, setSelectedRoomId] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [inviteCode, setInviteCode] = useState('')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    loadRooms().catch(() => {})
  }, [loadRooms])

  const groupRooms = state.rooms.filter((r) => r.type === 'group')
  const selected = groupRooms.find((r) => r.id === selectedRoomId)

  if (selected) {
    return (
      <ChatView
        roomId={selected.id}
        title={selected.name ?? 'Group'}
        headerExtra={
          <>
            <span className="invite-code">Invite: {selected.invite_code}</span>
            <button
              onClick={async () => {
                if (!state.token) return
                await api.leaveRoom(state.token, selected.id)
                setSelectedRoomId(null)
                loadRooms().catch(() => {})
              }}
            >
              Leave
            </button>
          </>
        }
      />
    )
  }

  async function createRoom(e: React.FormEvent) {
    e.preventDefault()
    if (!state.token || !name.trim()) return
    setError(null)
    try {
      const room = await api.createGroupRoom(state.token, name.trim())
      setName('')
      await loadRooms()
      setSelectedRoomId(room.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  async function joinRoom(e: React.FormEvent) {
    e.preventDefault()
    if (!state.token || !inviteCode.trim()) return
    setError(null)
    try {
      const room = await api.joinRoomByInviteCode(state.token, inviteCode.trim())
      setInviteCode('')
      await loadRooms()
      setSelectedRoomId(room.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }

  return (
    <div className="channels-screen">
      <h2>Channels</h2>
      <ul className="room-list">
        {groupRooms.map((r) => (
          <li key={r.id}>
            <button onClick={() => setSelectedRoomId(r.id)}>{r.name}</button>
          </li>
        ))}
        {groupRooms.length === 0 && <li className="hint">No channels yet.</li>}
      </ul>
      <form onSubmit={createRoom}>
        <input value={name} onChange={(e) => setName(e.target.value)} placeholder="New channel name" />
        <button type="submit">Create</button>
      </form>
      <form onSubmit={joinRoom}>
        <input value={inviteCode} onChange={(e) => setInviteCode(e.target.value)} placeholder="Invite code" />
        <button type="submit">Join</button>
      </form>
      {error && <p className="error-text">{error}</p>}
    </div>
  )
}
