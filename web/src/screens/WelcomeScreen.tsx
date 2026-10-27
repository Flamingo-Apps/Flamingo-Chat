import { useState } from 'react'
import * as api from '../api'
import { Avatar } from '../components/Avatar'
import { OnlineBadge } from '../components/OnlineBadge'
import { useGateway } from '../gateway'
import { generatePseudonym } from '../pseudonym'
import { GoogleSignInLink } from './GoogleSignInLink'

// The first thing anyone sees. One question, two answers: someone new goes
// straight into the app as a guest with a made-up name (no form, no
// sign-in, that is the default), and someone who has been here before
// signs back in with Google.
export function WelcomeScreen() {
  const { state, login } = useGateway()
  const [returning, setReturning] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function startAsGuest() {
    setLoading(true)
    setError(null)
    try {
      const resp = await api.signUpGuest(generatePseudonym())
      login(resp.account, resp.session_token)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      setLoading(false)
    }
  }

  return (
    <div className="welcome">
      <div className="welcome__main">
        <div className="brand brand--big">
          <span className="brand__mark">🦩</span>
          <span className="brand__name">Flamingo</span>
        </div>
        <h1>Talk to anyone at KIIT. Nobody knows it's you.</h1>
        <p className="welcome__lede">Random chats and group channels with a made-up name. You only share who you are if you choose to.</p>
        <OnlineBadge />

        {state.kicked && <p className="error-text">You were removed: {state.kicked}</p>}

        <h2 className="welcome__question">Have you been here before?</h2>
        <div className="welcome__options">
          <button className="option option--new" onClick={startAsGuest} disabled={loading}>
            <span className="option__emoji">✨</span>
            <span className="option__title">{loading ? 'Getting you in...' : "Nope, I'm new"}</span>
            <span className="option__sub">Jump straight in. No sign-up.</span>
          </button>
          <button className={`option ${returning ? 'option--on' : ''}`} onClick={() => setReturning(true)} disabled={loading}>
            <span className="option__emoji">👋</span>
            <span className="option__title">Yes, sign me in</span>
            <span className="option__sub">Use the Google account you verified with.</span>
          </button>
        </div>

        {returning && (
          <div className="welcome__returning">
            <p>Welcome back! Sign in with the same Google account as before.</p>
            <GoogleSignInLink href={api.googleSignInURL()} />
          </div>
        )}
        {error && <p className="error-text">{error}</p>}
      </div>

      <div className="welcome__art" aria-hidden="true">
        <div className="art-bubble art-bubble--a"><Avatar name="Swift Fox" size={34} /> hey, anyone up?</div>
        <div className="art-bubble art-bubble--b">which hostel are you in? <Avatar name="Quiet Otter" size={34} /></div>
        <div className="art-bubble art-bubble--c"><Avatar name="Lucky Panda" size={34} /> mess food again 😭</div>
        <div className="art-bubble art-bubble--d">same 😂 <Avatar name="Witty Lynx" size={34} /></div>
      </div>
    </div>
  )
}
