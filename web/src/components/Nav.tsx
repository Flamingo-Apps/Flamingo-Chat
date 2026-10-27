import { useGateway } from '../gateway'
import { Avatar } from './Avatar'
import { Brand } from './Brand'
import { CheckBadgeIcon, HashIcon, InboxIcon, ShuffleIcon } from './Icons'
import { OnlineBadge } from './OnlineBadge'

export type Screen = 'random' | 'channels' | 'inbox'

const ITEMS: { screen: Screen; label: string; icon: React.ReactNode }[] = [
  { screen: 'random', label: 'Random chat', icon: <ShuffleIcon /> },
  { screen: 'channels', label: 'Channels', icon: <HashIcon /> },
  { screen: 'inbox', label: 'Inbox', icon: <InboxIcon /> },
]

interface NavProps {
  screen: Screen
  onChange: (s: Screen) => void
}

// Desktop: everything the person can do lives in this left column, with
// the chat on the right.
export function Sidebar({ screen, onChange, onGetIdentity, onSignOut }: NavProps & { onGetIdentity: () => void; onSignOut: () => void }) {
  const { state } = useGateway()
  const account = state.account
  if (!account) return null

  return (
    <aside className="sidebar">
      <Brand />

      <nav className="sidebar__nav" aria-label="Main">
        {ITEMS.map((item) => (
          <button key={item.screen} className={`nav-item ${screen === item.screen ? 'nav-item--active' : ''}`} onClick={() => onChange(item.screen)}>
            {item.icon}
            <span>{item.label}</span>
          </button>
        ))}
      </nav>

      <div className="sidebar__foot">
        <OnlineBadge />
        <div className="profile">
          <Avatar name={account.pseudonym} size={48} />
          <div className="profile__text">
            <div className="profile__name">{account.pseudonym}</div>
            <div className="profile__tag">
              {account.google_linked ? (
                <>
                  <CheckBadgeIcon /> Verified
                </>
              ) : (
                'Guest'
              )}
            </div>
          </div>
        </div>
        {!account.google_linked && (
          <button className="btn btn--sm" onClick={onGetIdentity}>
            Get an identity
          </button>
        )}
        <button className="link-button" onClick={onSignOut}>
          Sign out
        </button>
      </div>
    </aside>
  )
}

// Phone: a slim header with the live count, and the three destinations
// along the bottom.
export function MobileTopBar({ onProfile }: { onProfile: () => void }) {
  const { state } = useGateway()
  if (!state.account) return null
  return (
    <header className="topbar">
      <Brand />
      <OnlineBadge />
      <button className="topbar__me" onClick={onProfile} aria-label="Your profile">
        <Avatar name={state.account.pseudonym} size={36} />
      </button>
    </header>
  )
}

export function BottomNav({ screen, onChange }: NavProps) {
  return (
    <nav className="bottom-nav" aria-label="Main">
      {ITEMS.map((item) => (
        <button key={item.screen} className={`bottom-nav__item ${screen === item.screen ? 'bottom-nav__item--active' : ''}`} onClick={() => onChange(item.screen)}>
          {item.icon}
          <span>{item.label}</span>
        </button>
      ))}
    </nav>
  )
}
