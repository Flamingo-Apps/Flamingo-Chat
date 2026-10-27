import { useMemo, useState } from 'react'
import './app.css'
import { GenderPrompt } from './components/GenderPrompt'
import { IdentityCard } from './components/IdentityCard'
import { Modal } from './components/Modal'
import { BottomNav, MobileTopBar, Sidebar, type Screen } from './components/Nav'
import { GatewayProvider, useGateway } from './gateway'
import { ChannelsScreen } from './screens/ChannelsScreen'
import { InboxScreen } from './screens/InboxScreen'
import { RandomScreen } from './screens/RandomScreen'
import { WelcomeScreen } from './screens/WelcomeScreen'
import { UiContext } from './ui'

type Dialog = 'identity' | 'gender' | 'signout' | null

function Main() {
  const { state, logout } = useGateway()
  const [screen, setScreen] = useState<Screen>('random')
  const [dialog, setDialog] = useState<Dialog>(null)
  const [genderSnoozed, setGenderSnoozed] = useState(false)
  const account = state.account

  // Right after a first Google sign-in the account is verified but has no
  // gender yet: ask once, straight away. "Decide later" snoozes it for this
  // visit; reaching for a gender filter asks again.
  const needsGender = Boolean(account?.google_linked && !account.gender_chosen)
  const shown: Dialog = dialog ?? (needsGender && !genderSnoozed ? 'gender' : null)

  const ui = useMemo(
    () => ({
      askForIdentity: () => setDialog(account?.google_linked ? 'gender' : 'identity'),
    }),
    [account?.google_linked],
  )

  if (!account) return <WelcomeScreen />

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
          {screen === 'inbox' && <InboxScreen />}
        </main>
        <BottomNav screen={screen} onChange={setScreen} />
      </div>

      {shown === 'identity' && <IdentityCard onClose={close} />}
      {shown === 'gender' && <GenderPrompt onClose={closeGender} />}
      {shown === 'signout' && (
        <Modal onClose={close} label="Sign out">
          <div className="modal__emoji">👋</div>
          <h2>Sign out?</h2>
          <p>{account.google_linked ? 'You can sign back in any time with Google.' : "You're a guest, so this name and your chats can't be recovered once you sign out."}</p>
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
