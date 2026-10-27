import { useState } from 'react'
import * as api from '../api'
import { useGateway } from '../gateway'
import { GoogleSignInLink } from '../screens/GoogleSignInLink'
import { Avatar } from './Avatar'
import { ChevronIcon } from './Icons'
import { Mascot } from './Mascot'
import { Modal } from './Modal'

// Shown when someone reaches for something that needs a verified
// identity (gender-filtered matching).
//
// The words are chosen to keep a person out of a defensive crouch. Asking
// someone to "prove" who they are, in a voice of authority, is the kind of
// phrasing that makes people push back; research on app permission
// requests found the wording of the request largely decides whether people
// agree, and that authoritative, non-transparent language raises
// resistance. So the card leads with what stays private, says why in one
// plain line, keeps the choice visibly theirs, and the way out is a
// neutral "Maybe later", never a guilt trip.
//
// The Google button sits inside a small panel that opens on tap, and it is
// Google's own button: Google requires that buttons or links that initiate
// an action on a Google product follow its branding guidelines
// (support.google.com/cloud/answer/13464321). Not opening the panel and
// saying "Maybe later" shows what others see of you, which is only your
// anonymous name.
export function IdentityCard({ onClose }: { onClose: () => void }) {
  const { state } = useGateway()
  const [step, setStep] = useState<'ask' | 'preview'>('ask')
  const [open, setOpen] = useState(false)
  const name = state.account?.pseudonym ?? 'Swift Fox'

  const panel = (
    <div className={`accordion ${open ? 'accordion--open' : ''}`}>
      <button className="accordion__head" aria-expanded={open} onClick={() => setOpen(!open)}>
        <span>Get your identity</span>
        <span className="accordion__chev">
          <ChevronIcon />
        </span>
      </button>
      {open && (
        <div className="accordion__body">
          <p>One tap with Google tells us you're a real KIIT student, so everyone here feels safe. That's all.</p>
          <GoogleSignInLink href={api.googleSignInURL(state.token ?? undefined)} />
        </div>
      )}
    </div>
  )

  return (
    <Modal onClose={onClose} label="Get your identity">
      {step === 'ask' ? (
        <>
          <Mascot mood="hello" size={116} />
          <h2>Want to unlock this?</h2>
          <p>
            You'll still be <b>{name}</b>. Nobody ever sees your email.
          </p>
          {panel}
          <button className="link-button" onClick={() => setStep('preview')}>
            Maybe later
          </button>
        </>
      ) : (
        <>
          <Mascot mood="think" size={116} />
          <h2>Here's how others see you</h2>
          <div className="preview-chat">
            <div className="preview-chat__row">
              <Avatar name={name} size={40} />
              <div>
                <div className="preview-chat__name">{name}</div>
                <div className="bubble bubble--theirs">hey, I'm new here</div>
              </div>
            </div>
          </div>
          <p className="muted">Just a name. Never your email, never your Google account.</p>
          {panel}
          <button className="link-button" onClick={onClose}>
            Close
          </button>
        </>
      )}
    </Modal>
  )
}
