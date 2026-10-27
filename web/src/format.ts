// formatCount renders the live online number with thousands separators
// (Indian grouping, since this is a campus app: 1,23,456). Plain digits
// below a thousand.
export function formatCount(n: number): string {
  return n.toLocaleString('en-IN')
}

// One emoji per animal the server can put in a name
// (services/identity/internal/server/pseudonym.go), so an anonymous name has
// a face without us storing or asking for anything. A name whose animal
// isn't in the table falls back to its first letter. Placeholder art: the
// maintainer plans to replace these with their own blobs.
const ANIMALS: Record<string, string> = {
  Otter: '🦦', Falcon: '🦅', Fox: '🦊', Panda: '🐼', Tiger: '🐯', Wolf: '🐺',
  Hawk: '🦅', Heron: '🐦', Lynx: '🐱', Raven: '🐦‍⬛', Badger: '🦡', Dolphin: '🐬',
  Eagle: '🦅', Gecko: '🦎', Ibis: '🐦', Jay: '🐦',
  Koala: '🐨', Lemur: '🐒', Moose: '🦌', Newt: '🦎', Owl: '🦉', Puffin: '🐧', Quokka: '🐹',
  Robin: '🐦', Seal: '🦭', Toucan: '🦜', Urchin: '🦔', Viper: '🐍', Walrus: '🦭', Yak: '🐂',
}

export interface AvatarLook {
  glyph: string
  hue: number
}

// avatarFor is deterministic: the same pseudonym always gets the same
// glyph and background hue, on every device and for every viewer, which
// is what lets two people recognise "that's Swift Fox" across a chat.
export function avatarFor(pseudonym: string): AvatarLook {
  // A trailing number ("Swift Fox 42") only keeps the name unique.
  const words = pseudonym.trim().split(/\s+/).filter((w) => !/^\d+$/.test(w))
  const last = words.pop() ?? ''
  const glyph = ANIMALS[last] ?? (pseudonym.trim().charAt(0).toUpperCase() || '?')
  let hash = 0
  for (const ch of pseudonym) hash = (hash * 31 + ch.charCodeAt(0)) >>> 0
  return { glyph, hue: hash % 360 }
}

export function clockTime(unixSeconds: number): string {
  return new Date(unixSeconds * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

// dayLabel names the day a message was sent for the separators in a chat:
// "Today", "Yesterday", then the plain date. `now` is a parameter so it can
// be tested without waiting for midnight.
export function dayLabel(unixSeconds: number, now: Date = new Date()): string {
  const sent = new Date(unixSeconds * 1000)
  const midnight = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
  const daysAgo = Math.round((midnight(now) - midnight(sent)) / 86_400_000)
  if (daysAgo === 0) return 'Today'
  if (daysAgo === 1) return 'Yesterday'
  return sent.toLocaleDateString([], { day: 'numeric', month: 'short', year: sent.getFullYear() === now.getFullYear() ? undefined : 'numeric' })
}
