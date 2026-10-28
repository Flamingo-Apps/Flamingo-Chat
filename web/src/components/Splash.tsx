import { Brand } from './Brand'
import { Mascot } from './Mascot'

// Shown instead of the welcome screen while a saved or just-returned session is
// being checked, so someone who is already signed in never sees "Have you been
// here before?" flash by on the way into the app.
export function Splash() {
  return (
    <div className="splash" role="status" aria-label="Getting you in">
      <Brand big />
      <Mascot mood="hello" size={96} />
      <p className="splash__text">Getting you in...</p>
    </div>
  )
}
