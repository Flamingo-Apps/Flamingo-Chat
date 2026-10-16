import { useState } from 'react'
import * as api from '../api'
import { useGateway } from '../gateway'
import { generatePseudonym } from '../pseudonym'
import { GoogleSignInButton } from './GoogleSignInButton'

export function AuthScreen() {
  const { login } = useGateway()
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  async function continueAsGuest() {
    setLoading(true)
    setError(null)
    try {
      const resp = await api.signUpGuest(generatePseudonym())
      login(resp.account, resp.session_token)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }

  async function onGoogleToken(idToken: string) {
    setLoading(true)
    setError(null)
    try {
      const resp = await api.signInGoogle(idToken)
      login(resp.account, resp.session_token)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="auth-screen">
      <h1>Flamingo</h1>
      <p>Anonymous chat for KIIT.</p>
      <button onClick={continueAsGuest} disabled={loading}>
        Continue as guest
      </button>
      <div className="auth-screen__divider">or</div>
      <GoogleSignInButton onToken={onGoogleToken} />
      {error && <p className="error-text">{error}</p>}
    </div>
  )
}
