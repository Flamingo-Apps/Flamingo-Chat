export type Screen = 'random' | 'channels' | 'inbox'

export function BottomNav({ screen, onChange }: { screen: Screen; onChange: (s: Screen) => void }) {
  return (
    <nav className="bottom-nav">
      <button className={screen === 'random' ? 'active' : ''} onClick={() => onChange('random')}>
        Random
      </button>
      <button className={screen === 'channels' ? 'active' : ''} onClick={() => onChange('channels')}>
        Channels
      </button>
      <button className={screen === 'inbox' ? 'active' : ''} onClick={() => onChange('inbox')}>
        Inbox
      </button>
    </nav>
  )
}
