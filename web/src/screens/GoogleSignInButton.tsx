import { useEffect, useRef } from 'react'

// The public client ID identity's own Helm values already hardcode
// (deploy/helm/service/values/identity.yaml, COLLEGE_OAUTH_CLIENT_ID) -
// not a secret (DEC-0027/DEC-0030), just configurable here too in case a
// different environment ever needs a different one.
const CLIENT_ID = import.meta.env.VITE_GOOGLE_CLIENT_ID ?? '155057367000-e6fvpmf6g3up2p20133i7jhnd0nc3hfn.apps.googleusercontent.com'

// Google's Identity Services SDK has no official npm types package;
// this declares just the surface this component actually calls.
declare global {
  interface Window {
    google?: {
      accounts: {
        id: {
          initialize: (config: { client_id: string; callback: (response: { credential: string }) => void }) => void
          renderButton: (parent: HTMLElement, options: { type: string; theme: string; size: string }) => void
        }
      }
    }
  }
}

let scriptLoadPromise: Promise<void> | null = null

function loadGoogleScript(): Promise<void> {
  if (window.google?.accounts?.id) return Promise.resolve()
  scriptLoadPromise ??= new Promise((resolve, reject) => {
    const script = document.createElement('script')
    script.src = 'https://accounts.google.com/gsi/client'
    script.async = true
    script.onload = () => resolve()
    script.onerror = () => reject(new Error('failed to load Google Identity Services'))
    document.head.appendChild(script)
  })
  return scriptLoadPromise
}

export function GoogleSignInButton({ onToken }: { onToken: (idToken: string) => void }) {
  const containerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let cancelled = false
    // Captured now, not read from containerRef.current inside cleanup -
    // by the time cleanup runs, the ref may already point elsewhere.
    const container = containerRef.current
    loadGoogleScript()
      .then(() => {
        if (cancelled || !container || !window.google) return
        window.google.accounts.id.initialize({
          client_id: CLIENT_ID,
          callback: (response) => onToken(response.credential),
        })
        window.google.accounts.id.renderButton(container, { type: 'standard', theme: 'outline', size: 'large' })
      })
      .catch((err) => console.error(err))
    // renderButton appends into the container rather than replacing its
    // content - without clearing it here, React StrictMode's deliberate
    // dev-mode double-invoke (mount, cleanup, mount) leaves two buttons
    // stacked in the same container, and so would any real remount.
    return () => {
      cancelled = true
      if (container) container.innerHTML = ''
    }
  }, [onToken])

  return <div ref={containerRef} className="google-signin-button" />
}
