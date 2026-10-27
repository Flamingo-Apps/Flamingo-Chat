// Mirrors docs/ARCHITECTURE.md §5.1 (HTTP) and §5.2 (WebSocket) - the
// gateway's own contract, not generated, since the gateway has no OpenAPI
// or similar schema to generate from.

export type Gender = 'unspecified' | 'male' | 'female' | 'prefer_not_to_say'

export interface Account {
  id: string
  pseudonym: string
  gender: Gender
  gender_chosen: boolean
  google_linked: boolean
  created_at_unix: number
}

export interface Room {
  id: string
  type: 'direct' | 'group'
  name?: string
  owner_account_id?: string
  invite_code?: string
  kept: boolean
  created_at_unix: number
  // Set once the room is closed: it can be read but nobody can send to it.
  ended_at_unix?: number
  // Direct rooms: the other person's anonymous name.
  partner_pseudonym?: string
}

export interface Message {
  message_id: string
  room_id: string
  sender_account_id: string
  sender_pseudonym: string
  body: string
  sent_at_unix: number
}

export interface ApiError {
  error: { code: string; message: string }
}

export type Preference = 'any' | 'male' | 'female'

// Client to server frames (§5.2's first table).
export type ClientFrame =
  | { type: 'auth'; token: string }
  | { type: 'send_message'; room_id: string; client_msg_id: string; body: string }
  | { type: 'match_request'; preference: Preference }
  | { type: 'match_cancel' }
  | { type: 'match_next' }
  | { type: 'match_end' }
  | { type: 'keep_chat'; room_id: string }
  // Check my rooms now: sent right after creating or joining one over HTTP.
  | { type: 'sync_rooms' }
  | { type: 'ping' }

// Server to client frames (§5.2's second table).
export type ServerFrame =
  | { type: 'auth_ok'; account: Account; token?: string }
  | {
      type: 'message'
      message_id: string
      room_id: string
      sender_account_id: string
      sender_pseudonym: string
      body: string
      sent_at: number
      client_msg_id?: string
    }
  | { type: 'message_ack'; client_msg_id: string; message_id: string; sent_at: number }
  | { type: 'match_queued' }
  | { type: 'match_cancelled' }
  | { type: 'match_found'; room_id: string; partner_pseudonym: string; partner_account_id: string }
  | { type: 'match_ended'; room_id: string; reason: 'you_left' | 'partner_left' }
  | { type: 'keep_status'; room_id: string; you: boolean; partner: boolean; kept: boolean }
  // The other person asked to keep this chat.
  | { type: 'keep_requested'; room_id: string }
  | { type: 'room_joined'; room_id: string }
  | { type: 'room_left'; room_id: string }
  | { type: 'presence'; total: number; guests: number; male: number; female: number }
  | { type: 'kicked'; reason: string }
  | { type: 'error'; code: string; message: string; client_msg_id?: string }
  | { type: 'pong' }
