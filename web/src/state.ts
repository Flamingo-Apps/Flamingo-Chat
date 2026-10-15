import type { Account, Message, Room, ServerFrame } from './types'
import type { ConnectionStatus } from './ws'

export interface CurrentMatch {
  roomId: string
  partnerPseudonym: string
  partnerAccountId: string
  youKept: boolean
  partnerKept: boolean
  kept: boolean
}

export interface AppState {
  account: Account | null
  token: string | null
  connectionStatus: ConnectionStatus
  rooms: Room[]
  messagesByRoom: Record<string, Message[]>
  currentMatch: CurrentMatch | null
  matching: boolean
  presence: { total: number; guests: number; male: number; female: number } | null
  lastError: string | null
  kicked: string | null
}

export const initialState: AppState = {
  account: null,
  token: null,
  connectionStatus: 'closed',
  rooms: [],
  messagesByRoom: {},
  currentMatch: null,
  matching: false,
  presence: null,
  lastError: null,
  kicked: null,
}

export type Action =
  | { type: 'session'; account: Account; token: string }
  | { type: 'logout' }
  | { type: 'connectionStatus'; status: ConnectionStatus }
  | { type: 'roomsLoaded'; rooms: Room[] }
  | { type: 'messagesLoaded'; roomId: string; messages: Message[] }
  | { type: 'accountUpdated'; account: Account }
  | { type: 'frame'; frame: ServerFrame }
  | { type: 'clearError' }

// addMessageDeduped appends msg to list unless a message with the same
// message_id is already present - docs/ARCHITECTURE.md §5.2 calls for
// duplicate removal because a reconnect reloads history that can
// already include messages this list saw live.
function addMessageDeduped(list: Message[], msg: Message): Message[] {
  if (list.some((m) => m.message_id === msg.message_id)) return list
  return [...list, msg].sort((a, b) => a.sent_at_unix - b.sent_at_unix)
}

function withRoomMessages(state: AppState, roomId: string, update: (list: Message[]) => Message[]): AppState {
  const existing = state.messagesByRoom[roomId] ?? []
  return { ...state, messagesByRoom: { ...state.messagesByRoom, [roomId]: update(existing) } }
}

function applyFrame(state: AppState, frame: ServerFrame): AppState {
  switch (frame.type) {
    case 'message':
      return withRoomMessages(state, frame.room_id, (list) =>
        addMessageDeduped(list, {
          message_id: frame.message_id,
          room_id: frame.room_id,
          sender_account_id: frame.sender_account_id,
          sender_pseudonym: frame.sender_pseudonym,
          body: frame.body,
          sent_at_unix: frame.sent_at,
        }),
      )

    case 'match_queued':
      return { ...state, matching: true, currentMatch: null }

    case 'match_found':
      return {
        ...state,
        matching: false,
        currentMatch: {
          roomId: frame.room_id,
          partnerPseudonym: frame.partner_pseudonym,
          partnerAccountId: frame.partner_account_id,
          youKept: false,
          partnerKept: false,
          kept: false,
        },
      }

    case 'match_ended':
      if (state.currentMatch?.roomId !== frame.room_id) return state
      return { ...state, currentMatch: null }

    case 'keep_status':
      if (state.currentMatch?.roomId !== frame.room_id) return state
      return {
        ...state,
        currentMatch: { ...state.currentMatch, youKept: frame.you, partnerKept: frame.partner, kept: frame.kept },
      }

    case 'presence':
      return { ...state, presence: { total: frame.total, guests: frame.guests, male: frame.male, female: frame.female } }

    case 'kicked':
      return { ...initialState, kicked: frame.reason }

    case 'error':
      return { ...state, lastError: frame.message }

    // room_joined/room_left: the HTTP rooms list (fetched when the
    // Channels/Inbox screens are visited) is the source of truth here,
    // the same "re-fetch rather than live-patch" choice DEC-0073 already
    // made for gateway's own room subscriptions - no action needed.
    // message_ack, auth_ok, pong: message_ack needs no UI effect since
    // the sender also gets their own "message" frame back (handled
    // above); auth_ok and pong are handled by the connection layer, not
    // app state.
    default:
      return state
  }
}

export function reducer(state: AppState, action: Action): AppState {
  switch (action.type) {
    case 'session':
      return { ...state, account: action.account, token: action.token, kicked: null }
    case 'logout':
      return { ...initialState }
    case 'connectionStatus':
      return { ...state, connectionStatus: action.status }
    case 'roomsLoaded':
      return { ...state, rooms: action.rooms }
    case 'messagesLoaded':
      return withRoomMessages(state, action.roomId, (list) => {
        let next = list
        for (const m of action.messages) next = addMessageDeduped(next, m)
        return next
      })
    case 'accountUpdated':
      return { ...state, account: action.account }
    case 'frame':
      return applyFrame(state, action.frame)
    case 'clearError':
      return { ...state, lastError: null }
    default:
      return state
  }
}
