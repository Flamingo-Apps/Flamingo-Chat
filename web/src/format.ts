// formatCount renders the live online number with thousands separators
// (Indian grouping, since this is a campus app: 1,23,456). Plain digits
// below a thousand.
export function formatCount(n: number): string {
  return n.toLocaleString('en-IN')
}

// One emoji per animal in pseudonym.ts's word list, so an anonymous name
// has a face without us storing or asking for anything. A name whose
// last word isn't in the table (the backend generates its own for Google
// sign-ups) falls back to its first letter. Placeholder art: the
// maintainer plans to replace these with their own blobs.
const ANIMALS: Record<string, string> = {
  Otter: '🦦', Falcon: '🦅', Fox: '🦊', Panda: '🐼', Tiger: '🐯', Wolf: '🐺',
  Hawk: '🦅', Heron: '🐦', Lynx: '🐱', Raven: '🐦‍⬛', Badger: '🦡', Dolphin: '🐬',
  Eagle: '🦅', Gecko: '🦎', Ibis: '🐦', Jay: '🐦',
}

export interface AvatarLook {
  glyph: string
  hue: number
}

// avatarFor is deterministic: the same pseudonym always gets the same
// glyph and background hue, on every device and for every viewer, which
// is what lets two people recognise "that's Swift Fox" across a chat.
export function avatarFor(pseudonym: string): AvatarLook {
  const last = pseudonym.trim().split(/\s+/).pop() ?? ''
  const glyph = ANIMALS[last] ?? (pseudonym.trim().charAt(0).toUpperCase() || '?')
  let hash = 0
  for (const ch of pseudonym) hash = (hash * 31 + ch.charCodeAt(0)) >>> 0
  return { glyph, hue: hash % 360 }
}

export function clockTime(unixSeconds: number): string {
  return new Date(unixSeconds * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
