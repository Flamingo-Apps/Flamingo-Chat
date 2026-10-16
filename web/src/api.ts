import type { Account, ApiError, Message, Room } from './types'

// VITE_GATEWAY_URL defaults to the gateway's own dev port (DEC-0071's
// values/gateway.yaml HTTP_PORT, forwarded to localhost by Tilt).
const BASE_URL = import.meta.env.VITE_GATEWAY_URL ?? 'http://localhost:8080'

export class ApiRequestError extends Error {
  code: string
  constructor(code: string, message: string) {
    super(message)
    this.code = code
  }
}

async function request<T>(method: string, path: string, token?: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (token) headers['Authorization'] = `Bearer ${token}`

  const res = await fetch(`${BASE_URL}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (res.status === 204) return undefined as T
  const data = await res.json()
  if (!res.ok) {
    const err = data as ApiError
    throw new ApiRequestError(err.error?.code ?? 'unknown', err.error?.message ?? res.statusText)
  }
  return data as T
}

export function signUpGuest(pseudonym: string): Promise<{ session_token: string; account: Account }> {
  return request('POST', '/v1/auth/guest', undefined, { pseudonym })
}

export function signInGoogle(
  idToken: string,
  guestToken?: string,
): Promise<{ session_token: string; account: Account; needs_gender: boolean }> {
  return request('POST', '/v1/auth/google', guestToken, { id_token: idToken })
}

export function getMe(token: string): Promise<Account> {
  return request('GET', '/v1/me', token)
}

export function setGender(token: string, gender: string): Promise<Account> {
  return request('POST', '/v1/me/gender', token, { gender })
}

export function getPresence(): Promise<{ total: number; guests: number; male: number; female: number }> {
  return request('GET', '/v1/presence')
}

export function listRooms(token: string): Promise<{ rooms: Room[] }> {
  return request('GET', '/v1/rooms', token)
}

export function createGroupRoom(token: string, name: string): Promise<Room> {
  return request('POST', '/v1/rooms', token, { name })
}

export function joinRoomByInviteCode(token: string, inviteCode: string): Promise<Room> {
  return request('POST', '/v1/rooms/join', token, { invite_code: inviteCode })
}

export function leaveRoom(token: string, roomId: string): Promise<void> {
  return request('POST', `/v1/rooms/${roomId}/leave`, token)
}

export function listMessages(token: string, roomId: string, before?: string): Promise<{ messages: Message[] }> {
  const query = before ? `?before=${encodeURIComponent(before)}` : ''
  return request('GET', `/v1/rooms/${roomId}/messages${query}`, token)
}

export function fileReport(token: string, reportedAccountId: string, reason: string, roomId?: string): Promise<{ report_id: string }> {
  return request('POST', '/v1/reports', token, { reported_account_id: reportedAccountId, reason, room_id: roomId })
}

export function listBlocks(token: string): Promise<{ blocked_account_ids: string[] }> {
  return request('GET', '/v1/blocks', token)
}

export function blockUser(token: string, blockedAccountId: string): Promise<{ blocked: boolean }> {
  return request('POST', '/v1/blocks', token, { blocked_account_id: blockedAccountId })
}
