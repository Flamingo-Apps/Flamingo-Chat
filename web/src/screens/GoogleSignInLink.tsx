import googleSignInButton from '../assets/google-signin-dark.svg'

// The image is Google's own pre-approved button (dark theme, pill, from
// signin-assets.zip on developers.google.com/identity/branding-guidelines),
// used unmodified: Google's brand verification checks sign-in buttons
// against those guidelines, which fix the logo, colours, padding and font.
export function GoogleSignInLink({ href }: { href: string }) {
  return (
    <a className="google-signin" href={href}>
      <img src={googleSignInButton} alt="Sign in with Google" width={180} height={40} />
    </a>
  )
}
