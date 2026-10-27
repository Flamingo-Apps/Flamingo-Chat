import { useState } from 'react'
import * as api from '../api'
import { useGateway } from '../gateway'
import { GoogleSignInLink } from '../screens/GoogleSignInLink'
import { Avatar } from './Avatar'
import { Modal } from './Modal'

// Shown when someone reaches for something that needs a verified
// identity (gender-filtered matching). Step 1 asks; if they decline,
// step 2 shows them exactly what other people would see of them, which is
// only their anonymous name - the reassurance that makes saying yes easy.
//
// The yes button is Google's own sign-in button, not one of ours: Google
// requires that "buttons or links that initiate an action on a Google
// product must follow the Google branding guidelines"
// (support.google.com/cloud/answer/13464321), and this one starts Google
// sign-in. It is still one click straight to Google's page.
export function IdentityCard({ onClose }: { onClose: () => void }) {
  const { state } = useGateway()
  const [step, setStep] = useState<'ask' | 'preview'>('ask')
  const name = state.account?.pseudonym ?? 'Swift Fox'
  const signIn = api.googleSignInURL(state.token ?? undefined)

  return (
    <Modal onClose={onClose} label="Get an identity">
      {step === 'ask' ? (
        <>
          <div className="modal__emoji">🛡️</div>
          <h2>Get an identity?</h2>
          <p>
            We care about your safety and value your experience. Just like we value everyone else, to use this feature you need to prove to others that you are who you say you are.
          </p>
          <p>
            Don't worry, your identity stays completely anonymous to every other user unless you type it out yourself. It's a one-step process: you just need a platform identity.
          </p>
          <div className="modal__actions">
            <GoogleSignInLink href={signIn} />
            <button className="link-button" onClick={() => setStep('preview')}>
              No, thanks
            </button>
          </div>
        </>
      ) : (
        <>
          <div className="modal__emoji">👀</div>
          <h2>Unsure about identity?</h2>
          <p>Wanna see how others would see you in chat?</p>
          <div className="preview-chat">
            <div className="preview-chat__row">
              <Avatar name={name} size={40} />
              <div>
                <div className="preview-chat__name">{name}</div>
                <div className="bubble bubble--theirs">hey, I'm new here 👋</div>
              </div>
            </div>
          </div>
          <p className="muted">That's all anyone sees: this name. Never your email, never your Google account.</p>
          <div className="modal__actions">
            <GoogleSignInLink href={signIn} />
            <button className="link-button" onClick={onClose}>
              Maybe later
            </button>
          </div>
        </>
      )}
    </Modal>
  )
}
