import { useState } from 'react'
import './app.css'
import { GatewayProvider, useGateway } from './gateway'
import { AuthScreen } from './screens/AuthScreen'
import { BottomNav, type Screen } from './screens/BottomNav'
import { ChannelsScreen } from './screens/ChannelsScreen'
import { InboxScreen } from './screens/InboxScreen'
import { RandomScreen } from './screens/RandomScreen'
import { TopBar } from './screens/TopBar'

function Main() {
  const { state } = useGateway()
  const [screen, setScreen] = useState<Screen>('random')

  if (!state.account) {
    return (
      <div className="app">
        {state.kicked && <p className="error-text">You were removed: {state.kicked}</p>}
        <AuthScreen />
      </div>
    )
  }

  return (
    <div className="app">
      <TopBar />
      <main className="app__content">
        {screen === 'random' && <RandomScreen />}
        {screen === 'channels' && <ChannelsScreen />}
        {screen === 'inbox' && <InboxScreen />}
      </main>
      <BottomNav screen={screen} onChange={setScreen} />
    </div>
  )
}

export default function App() {
  return (
    <GatewayProvider>
      <Main />
    </GatewayProvider>
  )
}
