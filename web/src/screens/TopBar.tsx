import { useState } from 'react'
import * as api from '../api'
import { useGateway } from '../gateway'

export function TopBar() {
  const { state, logout } = useGateway()
  const [verifying, setVerifying] = useState(false)
  const account = state.account
  if (!account) return null

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
          <a className="google-signin-link" href={api.googleSignInURL(state.token ?? undefined)}>
            Sign in with Google
          </a>
        </div>
      )}
    </header>
  )
}
