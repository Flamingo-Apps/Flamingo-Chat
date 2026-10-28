import { useMemo, useState } from 'react'
import './app.css'
import { GenderPrompt } from './components/GenderPrompt'
import { IdentityCard } from './components/IdentityCard'
import { Mascot } from './components/Mascot'
import { MeetMochi } from './components/MeetMochi'
import { Modal } from './components/Modal'
import { Splash } from './components/Splash'
import { BottomNav, MobileTopBar, Sidebar, type Screen } from './components/Nav'
import { GatewayProvider, useGateway } from './gateway'
import { ChannelsScreen } from './screens/ChannelsScreen'
import { InboxScreen } from './screens/InboxScreen'
import { RandomScreen } from './screens/RandomScreen'
import { WelcomeScreen } from './screens/WelcomeScreen'
import { UiContext } from './ui'

type Dialog = 'identity' | 'gender' | 'signout' | null

// Whether this browser has already met Mochi. Storage can be blocked
// (private windows), in which case the card just shows each visit.
const MET_KEY = 'flamingo_met_mochi'
function hasMetMochi(): boolean {
  try {
    return localStorage.getItem(MET_KEY) === '1'
  } catch {
    return false
  }
}

function Main() {
  const { state, restoring, holding, retryRestore, logout } = useGateway()
  const [screen, setScreen] = useState<Screen>('random')
  const [dialog, setDialog] = useState<Dialog>(null)
  const [genderSnoozed, setGenderSnoozed] = useState(false)
  const [metMochi, setMetMochi] = useState(hasMetMochi)
  const [inboxRoom, setInboxRoom] = useState<string | null>(null)
  const account = state.account

  // Right after a first Google sign-in the account is verified but has no
  // gender yet: ask once, straight away. "Decide later" snoozes it for this
  // visit; reaching for a gender filter asks again.
  const needsGender = Boolean(account?.google_linked && !account.gender_chosen)
  const shown: Dialog = dialog ?? (needsGender && !genderSnoozed ? 'gender' : null)

  const ui = useMemo(
    () => ({
      askForIdentity: () => setDialog(account?.google_linked ? 'gender' : 'identity'),
      openInbox: (roomId?: string) => {
        setInboxRoom(roomId ?? null)
        setScreen('inbox')
      },
    }),
    [account?.google_linked],
  )

  if (!account) return restoring ? <Splash holding={holding} onRetry={retryRestore} /> : <WelcomeScreen />

  const closeGender = () => {
    setGenderSnoozed(true)
    setDialog(null)
  }
  const close = () => setDialog(null)

  return (
    <UiContext.Provider value={ui}>
      <div className="shell">
        <Sidebar screen={screen} onChange={setScreen} onGetIdentity={() => setDialog('identity')} onSignOut={() => setDialog('signout')} />
        <MobileTopBar onProfile={() => setDialog(account.google_linked ? 'signout' : 'identity')} />
        <main className="shell__main">
          {screen === 'random' && <RandomScreen />}
          {screen === 'channels' && <ChannelsScreen />}
          {screen === 'inbox' && <InboxScreen key={inboxRoom ?? 'none'} initialRoomId={inboxRoom} />}
        </main>
        <BottomNav screen={screen} onChange={setScreen} />
      </div>

      {shown === null && !metMochi && (
        <MeetMochi
          onClose={() => {
            setMetMochi(true)
            try {
              localStorage.setItem(MET_KEY, '1')
            } catch {
              /* storage blocked: the card will simply show again next visit */
            }
          }}
        />
      )}
      {shown === 'identity' && <IdentityCard onClose={close} />}
      {shown === 'gender' && <GenderPrompt onClose={closeGender} />}
      {shown === 'signout' && (
        <Modal onClose={close} label="Sign out">
          <Mascot mood="worried" size={108} />
          <h2>Leaving already?</h2>
          <p>{account.google_linked ? 'Come back any time. Just sign in with Google.' : "Heads up: as a guest, your name and chats can't be brought back once you sign out."}</p>
          <div className="modal__actions">
            <button
              className="btn"
              onClick={() => {
                close()
                logout()
              }}
            >
              Sign out
            </button>
            <button className="link-button" onClick={close}>
              Stay
            </button>
          </div>
        </Modal>
      )}
    </UiContext.Provider>
  )
}

export default function App() {
  return (
    <GatewayProvider>
      <Main />
    </GatewayProvider>
  )
}
