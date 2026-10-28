import { Brand } from './Brand'
import { Mascot } from './Mascot'

// Shown instead of the welcome screen while a saved or just-returned session is
// being checked, so someone who is already signed in never sees "Have you been
// here before?" flash by on the way into the app. If the server cannot be
// reached it says so and offers a retry; it never offers to start a new
// account, which would replace the saved one.
export function Splash({ holding, onRetry }: { holding?: boolean; onRetry?: () => void }) {
  return (
    <div className="splash" role="status" aria-live="polite" aria-label={holding ? "Can't reach Flamingo" : 'Getting you in'}>
      <Brand big />
      <Mascot mood={holding ? 'worried' : 'hello'} size={96} />
      {holding ? (
        <>
          <p className="splash__text">{"Can't reach Flamingo right now. Your account is safe, we'll keep trying."}</p>
          <button className="btn" onClick={onRetry}>
            Try again
          </button>
        </>
      ) : (
        <p className="splash__text">Getting you in...</p>
      )}
    </div>
  )
}
