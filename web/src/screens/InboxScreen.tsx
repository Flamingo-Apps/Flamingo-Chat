import { useEffect, useState } from 'react'
import * as api from '../api'
import { Avatar } from '../components/Avatar'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { EmptyChat, Workspace } from '../components/Workspace'
import { useGateway } from '../gateway'
import { ChatView } from './ChatView'

// Inbox is chats the person chose to save (docs/ARCHITECTURE.md §10): a
// direct room they voted to keep, listed under the other person's
// anonymous name. A chat saved after the other person left, or closed with
// "Stop messaging", is read-only. "Delete" removes it from the Inbox and
// closes it for the other person too (DEC-0106).
export function InboxScreen({ initialRoomId }: { initialRoomId?: string | null }) {
  const { state, loadRooms } = useGateway()
  const [selectedRoomId, setSelectedRoomId] = useState<string | null>(initialRoomId ?? null)
  const [confirm, setConfirm] = useState<'stop' | 'delete' | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    loadRooms().catch(() => {})
  }, [loadRooms])

  const keptRooms = state.rooms.filter((r) => r.type === 'direct' && r.kept)
  const selected = keptRooms.find((r) => r.id === selectedRoomId)
  const nameOf = (r: { partner_pseudonym?: string }) => r.partner_pseudonym || 'Anonymous student'
  const when = (unix: number) => new Date(unix * 1000).toLocaleDateString()
  const closed = Boolean(selected?.ended_at_unix)

  async function act(kind: 'stop' | 'delete') {
    if (!state.token || !selected) return
    setBusy(true)
    setError(null)
    try {
      if (kind === 'stop') await api.closeRoom(state.token, selected.id)
      else {
        await api.leaveRoom(state.token, selected.id)
        setSelectedRoomId(null)
      }
      await loadRooms()
      setConfirm(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setConfirm(null)
    } finally {
      setBusy(false)
    }
  }

  const list = (
    <div className="panel">
      <h1 className="panel__title">Inbox</h1>
      <p className="panel__sub">Chats you saved. Each one is listed under the name they had.</p>
      <ul className="room-list">
        {keptRooms.map((r) => (
          <li key={r.id}>
            <button className={`room ${r.id === selectedRoomId ? 'room--on' : ''}`} onClick={() => setSelectedRoomId(r.id)}>
              <Avatar name={nameOf(r)} size={44} />
              <span className="room__text">
                <span className="room__name">{nameOf(r)}</span>
                <span className="room__meta">{r.ended_at_unix ? 'Closed, read only' : `Saved ${when(r.created_at_unix)}`}</span>
              </span>
            </button>
          </li>
        ))}
        {keptRooms.length === 0 && <li className="hint">Nothing here yet. Press Keep during a random chat to save it.</li>}
      </ul>
      {error && (
        <p className="error-text" role="alert">
          {error}
        </p>
      )}
    </div>
  )

  const chat = selected ? (
    <ChatView
      roomId={selected.id}
      title={nameOf(selected)}
      subtitle={closed ? 'Closed, read only' : `Saved ${when(selected.created_at_unix)}`}
      readOnly={closed}
      onBack={() => setSelectedRoomId(null)}
      headerExtra={
        <>
          {!closed && (
            <button className="btn btn--sm btn--ghost" onClick={() => setConfirm('stop')}>
              Stop messaging
            </button>
          )}
          <button className="btn btn--sm btn--ghost" onClick={() => setConfirm('delete')}>
            Delete
          </button>
        </>
      }
    />
  ) : (
    <EmptyChat mood="read" title="Your saved chats live here" text="When you both press Keep, the chat moves to your Inbox." />
  )

  return (
    <>
      <Workspace list={list} chat={chat} hasChat={Boolean(selected)} />
      {confirm === 'stop' && selected && (
        <ConfirmDialog
          mood="think"
          title="Stop messaging?"
          text={`After this nobody can send messages in this chat, you included. You and ${nameOf(selected)} can both still read it.`}
          confirmLabel="Stop messaging"
          busy={busy}
          onConfirm={() => act('stop')}
          onCancel={() => setConfirm(null)}
        />
      )}
      {confirm === 'delete' && selected && (
        <ConfirmDialog
          title="Delete this chat?"
          text={`It disappears from your Inbox, and it closes for ${nameOf(selected)} too. This can't be undone.`}
          confirmLabel="Delete"
          busy={busy}
          onConfirm={() => act('delete')}
          onCancel={() => setConfirm(null)}
        />
      )}
    </>
  )
}
