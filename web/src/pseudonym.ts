// Gateway requires a non-empty pseudonym for guest signup
// (services/identity/internal/server/server.go's CreateAccount) with no
// server-side fallback for that path - unlike Google sign-in, which
// generates one itself (services/identity/internal/server/pseudonym.go).
// This mirrors that generator's two-word style independently, since Go
// code isn't importable from here.
const ADJECTIVES = [
  'Silent', 'Quiet', 'Brave', 'Calm', 'Curious', 'Gentle', 'Happy', 'Lucky',
  'Bold', 'Clever', 'Swift', 'Bright', 'Quick', 'Wild', 'Kind', 'Witty',
]
const NOUNS = [
  'Otter', 'Falcon', 'Fox', 'Panda', 'Tiger', 'Wolf', 'Hawk', 'Heron',
  'Lynx', 'Raven', 'Badger', 'Dolphin', 'Eagle', 'Gecko', 'Ibis', 'Jay',
]

export function generatePseudonym(): string {
  const adjective = ADJECTIVES[Math.floor(Math.random() * ADJECTIVES.length)]
  const noun = NOUNS[Math.floor(Math.random() * NOUNS.length)]
  return `${adjective} ${noun}`
}
