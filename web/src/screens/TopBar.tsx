import { useState } from 'react'
import * as api from '../api'
import { useGateway } from '../gateway'
import { GoogleSignInButton } from './GoogleSignInButton'

export function TopBar() {
  const { state, login, logout } = useGateway()
  const [verifying, setVerifying] = useState(false)
  const account = state.account
  if (!account) return null

  async function onGoogleToken(idToken: string) {
    try {
      // The existing guest token upgrades the same account in place
      // (docs/ARCHITECTURE.md §4.2's case B) rather than creating a
      // second one.
      const resp = await api.signInGoogle(idToken, state.token ?? undefined)
      login(resp.account, resp.session_token)
    } finally {
      setVerifying(false)
    }
  }

  return (
    <header className="top-bar">
      <span className="top-bar__pseudonym">{account.pseudonym}</span>
      <span className={`top-bar__status top-bar__status--${state.connectionStatus}`} title={state.connectionStatus} />
      {!account.google_linked && (
        <button onClick={() => setVerifying((v) => !v)}>Get verified</button>
      )}
      <button onClick={logout}>Sign out</button>
      {verifying && !account.google_linked && (
        <div className="top-bar__verify-popover">
          <GoogleSignInButton onToken={onGoogleToken} />
        </div>
      )}
    </header>
  )
}
