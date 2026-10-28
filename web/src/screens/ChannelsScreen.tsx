import { useEffect, useState } from 'react'
import * as api from '../api'
import { friendlyError } from '../api'
import { EmptyChat, Workspace } from '../components/Workspace'
import { useGateway } from '../gateway'
import { ChatView } from './ChatView'

export function ChannelsScreen() {
  const { state, loadRooms, syncRooms } = useGateway()
  const [selectedRoomId, setSelectedRoomId] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [inviteCode, setInviteCode] = useState('')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    loadRooms().catch(() => {})
  }, [loadRooms])

  const groupRooms = state.rooms.filter((r) => r.type === 'group')
  const selected = groupRooms.find((r) => r.id === selectedRoomId)

  async function createRoom(e: React.FormEvent) {
    e.preventDefault()
    if (!state.token || !name.trim()) return
    setError(null)
    try {
      const room = await api.createGroupRoom(state.token, name.trim())
      syncRooms()
      setName('')
      await loadRooms()
      setSelectedRoomId(room.id)
    } catch (err) {
      setError(friendlyError(err))
    }
  }

  async function joinRoom(e: React.FormEvent) {
    e.preventDefault()
    if (!state.token || !inviteCode.trim()) return
    setError(null)
    try {
      const room = await api.joinRoomByInviteCode(state.token, inviteCode.trim())
      syncRooms()
      setInviteCode('')
      await loadRooms()
      setSelectedRoomId(room.id)
    } catch (err) {
      setError(friendlyError(err))
    }
  }

  const list = (
    <div className="panel">
      <h1 className="panel__title">Channels</h1>
      <p className="panel__sub">Group chats for your hostel, club or friends.</p>

      <ul className="room-list">
        {groupRooms.map((r) => (
          <li key={r.id}>
            <button className={`room ${r.id === selectedRoomId ? 'room--on' : ''}`} onClick={() => setSelectedRoomId(r.id)}>
              <span className="avatar avatar--group">#</span>
              <span className="room__name">{r.name}</span>
            </button>
          </li>
        ))}
        {groupRooms.length === 0 && <li className="hint">No channels yet. Make one below or join with a code.</li>}
      </ul>

      <form className="field" onSubmit={createRoom}>
        <label htmlFor="new-channel">Start a channel</label>
        <div className="field__row">
          <input id="new-channel" value={name} onChange={(e) => setName(e.target.value)} placeholder="Channel name" />
          <button className="btn btn--sm" type="submit" disabled={!name.trim()}>
            Create
          </button>
        </div>
      </form>
      <form className="field" onSubmit={joinRoom}>
        <label htmlFor="invite">Join with an invite code</label>
        <div className="field__row">
          <input id="invite" value={inviteCode} onChange={(e) => setInviteCode(e.target.value)} placeholder="e.g. K7Q2ZP" />
          <button className="btn btn--sm btn--ghost" type="submit" disabled={!inviteCode.trim()}>
            Join
          </button>
        </div>
      </form>
      {error && <p className="error-text">{error}</p>}
    </div>
  )

  const chat = selected ? (
    <ChatView
      roomId={selected.id}
      title={selected.name ?? 'Group'}
      subtitle={`Invite code: ${selected.invite_code}`}
      group
      onBack={() => setSelectedRoomId(null)}
      headerExtra={
        <button
          className="btn btn--sm btn--ghost"
          onClick={async () => {
            if (!state.token) return
            await api.leaveRoom(state.token, selected.id)
            setSelectedRoomId(null)
            loadRooms().catch(() => {})
          }}
        >
          Leave
        </button>
      }
    />
  ) : (
    <EmptyChat mood="read" title="Pick a channel" text="Or start your own and share the code with friends." />
  )

  return <Workspace list={list} chat={chat} hasChat={Boolean(selected)} />
}
