import type { Account, Message, Room, ServerFrame } from './types'
import type { ConnectionStatus } from './ws'

// GRACE_WINDOW_MS mirrors the backend's store.VoteKeepGraceWindow
// (services/chat/internal/store/store.go) - how long after the partner
// leaves a still-undecided match the person who stayed can still vote
// to keep it on their own (DEC-0091). Not shareable code across the Go/
// TypeScript boundary, so keep these two values in sync by hand.
export const GRACE_WINDOW_MS = 30_000

export interface CurrentMatch {
  roomId: string
  partnerPseudonym: string
  partnerAccountId: string
  youKept: boolean
  partnerKept: boolean
  kept: boolean
  // Set the moment a match_ended frame says the partner left (not us) -
  // RandomScreen uses it to show a "keep this chat?" countdown instead
  // of silently dropping the conversation (DEC-0091).
  partnerLeftAt: number | null
  // Where this chat falls in the order of things on this screen (see
  // AppState.seq). match_queued is only the answer to a request; if the chat
  // appeared after the request was made, the request has already been answered
  // by a match and a late match_queued must not undo it (see match_queued).
  foundSeq: number
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
  // The other person asked to keep the current chat and has not been answered.
  keepRequest: { roomId: string } | null
  // A counter that orders what happens on this screen without a clock: each
  // request for a match and each match found takes the next number. Two events
  // in the same millisecond still have an order.
  seq: number
  // The number the person's latest request for a match (Find a match or Next) took.
  searchSeq: number
  // A chat that was just saved, shown once as a card where the chat was.
  // readOnly is true when the other person had already left, so it can be
  // read but not continued (DEC-0106).
  savedMatch: { roomId: string; partnerPseudonym: string; readOnly: boolean } | null
  lastError: string | null
  kicked: string | null
}

// What to tell someone when the Google redirect (DEC-0092) comes back with an
// #auth_error reason instead of a session. The reasons are the gateway's own
// (oauth_redirect.go); anything unknown gets the same generic advice.
export function signInErrorMessage(reason: string): string {
  switch (reason) {
    case 'session_expired':
      return 'Your session has ended. Please start again.'
    case 'account_blocked':
      return "This account can't be used any more. If you think that is a mistake, please get in touch."

    case 'sign_in_failed':
      return 'That Google account could not be verified. Pick your KIIT Google account and try again.'
    case 'missing_code_or_state':
    case 'invalid_or_expired_state':
    case 'google_exchange_failed':
      return 'Google sign-in was interrupted or took too long. Please try again.'
    default:
      return 'Google sign-in did not work. Please try again.'
  }
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
  keepRequest: null,
  seq: 0,
  searchSeq: 0,
  savedMatch: null,
  lastError: null,
  kicked: null,
}

// activeRandomChat is the random chat the server says a person is in: an open,
// unsaved direct room, newest first. The server is the only place that knows;
// the browser's own memory is lost on a refresh and on every reconnect.
export function activeRandomChat(rooms: Room[]): Room | null {
  let best: Room | null = null
  for (const room of rooms) {
    if (room.type !== 'direct' || room.kept || room.ended_at_unix) continue
    if (best === null || room.created_at_unix > best.created_at_unix) best = room
  }
  return best
}

export type Action =
  | { type: 'session'; account: Account; token: string }
  | { type: 'matchReconciled'; rooms: Room[] }
  | { type: 'logout' }
  | { type: 'connectionStatus'; status: ConnectionStatus }
  | { type: 'roomsLoaded'; rooms: Room[] }
  | { type: 'messagesLoaded'; roomId: string; messages: Message[] }
  | { type: 'accountUpdated'; account: Account }
  | { type: 'frame'; frame: ServerFrame }
  | { type: 'clearError' }
  | { type: 'signInFailed'; reason: string }
  | { type: 'searchRequested' }
  | { type: 'graceWindowExpired'; roomId: string }
  | { type: 'keepRequestDismissed' }
  | { type: 'savedMatchDismissed' }

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

    // The answer to "find me a match": searching. It must not undo a match
    // that has already arrived. The match itself travels on a different path
    // (Kafka and Redis) from this acknowledgement, and in a small share of
    // pairings it reaches the screen first; wiping it left the person on
    // "Looking for someone" while the other side was already chatting with
    // them (INC-0030).
    case 'match_queued':
      if (state.currentMatch && state.currentMatch.foundSeq > state.searchSeq) return state
      return { ...state, matching: true, currentMatch: null }

    // The server's confirmation that we are no longer searching
    // (DEC-0103). Only the searching flag changes: a match_found that
    // raced ahead of the cancel has already set currentMatch, and that
    // chat should stay.
    case 'match_cancelled':
      return { ...state, matching: false }

    case 'match_found':
      return {
        ...state,
        seq: state.seq + 1,
        matching: false,
        currentMatch: {
          roomId: frame.room_id,
          partnerPseudonym: frame.partner_pseudonym,
          partnerAccountId: frame.partner_account_id,
          youKept: false,
          partnerKept: false,
          kept: false,
          partnerLeftAt: null,
          foundSeq: state.seq + 1,
        },
      }

    case 'match_ended':
      if (state.currentMatch?.roomId !== frame.room_id) return state
      // We ended it ourselves (End/Next) - our own client already acts
      // on that locally, so just drop the match as before. If the
      // partner left, don't drop it yet: RandomScreen shows a grace-
      // window "keep this chat?" prompt instead (DEC-0091), and this
      // action only marks when that window started.
      if (frame.reason === 'you_left') return { ...state, currentMatch: null, keepRequest: null }
      return { ...state, keepRequest: null, currentMatch: { ...state.currentMatch, partnerLeftAt: Date.now() } }

    // The other person asked to keep this chat (DEC-0106). Not shown to
    // someone who has already said yes themselves.
    case 'keep_requested':
      if (state.currentMatch?.roomId !== frame.room_id || state.currentMatch.youKept) return state
      return { ...state, keepRequest: { roomId: frame.room_id } }

    case 'keep_status':
      if (state.currentMatch?.roomId !== frame.room_id) return state
      // Saved: the chat leaves Random for the Inbox. If the other person
      // had already left, it is saved read-only (DEC-0106).
      if (frame.kept) {
        return {
          ...state,
          currentMatch: null,
          keepRequest: null,
          savedMatch: {
            roomId: frame.room_id,
            partnerPseudonym: state.currentMatch.partnerPseudonym,
            readOnly: state.currentMatch.partnerLeftAt !== null,
          },
        }
      }
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
      // A new sign-in starts clean: an old "your session has ended" must not
      // stay on screen once the person is back in.
      return { ...state, account: action.account, token: action.token, kicked: null, lastError: null }
    case 'searchRequested':
      return { ...state, seq: state.seq + 1, searchSeq: state.seq + 1 }
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
    // The server's list of rooms is the truth about which random chat this
    // person is in. After a refresh or a reconnect it restores the chat, or
    // clears one that ended while they were away. A chat already on screen
    // is kept as it is, so live details (who voted to keep) are not lost.
    case 'matchReconciled': {
      const withRooms = { ...state, rooms: action.rooms }
      const active = activeRandomChat(action.rooms)
      if (active) {
        if (state.currentMatch?.roomId === active.id) return withRooms
        return {
          ...withRooms,
          matching: false,
          keepRequest: null,
          currentMatch: {
            roomId: active.id,
            partnerPseudonym: active.partner_pseudonym || 'Anonymous student',
            partnerAccountId: active.partner_account_id ?? '',
            youKept: false,
            partnerKept: false,
            kept: false,
            partnerLeftAt: null,
            foundSeq: state.seq + 1,
          },
          seq: state.seq + 1,
        }
      }
      // No open chat on the server. One whose partner has just left is in its
      // short "keep this chat?" window, which has its own timer: leave it.
      if (state.currentMatch && state.currentMatch.partnerLeftAt === null) {
        return { ...withRooms, currentMatch: null, keepRequest: null, lastError: 'That chat has ended.' }
      }
      return withRooms
    }
    case 'signInFailed':
      return { ...state, lastError: signInErrorMessage(action.reason) }
    case 'keepRequestDismissed':
      return { ...state, keepRequest: null }
    case 'savedMatchDismissed':
      return { ...state, savedMatch: null }
    case 'graceWindowExpired':
      // Unconditional once the roomId matches - RandomScreen is what
      // decides whether this should fire at all (it cancels its own
      // countdown timer once the solo keep vote lands), and also calls
      // this directly when the user dismisses a "Saved!" confirmation.
      if (state.currentMatch?.roomId !== action.roomId) return state
      return { ...state, currentMatch: null }
    default:
      return state
  }
}
