import { Component, type ErrorInfo, type ReactNode } from 'react'
import { Brand } from './Brand'
import { Mascot } from './Mascot'

// If rendering ever throws, show this instead of a blank page. Reloading is
// safe: the session is saved in the browser and the server knows which chat
// the person is in.
export class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }

  static getDerivedStateFromError() {
    return { failed: true }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('render failed', error, info.componentStack)
  }

  render() {
    if (!this.state.failed) return this.props.children
    return (
      <div className="splash" role="alert">
        <Brand big />
        <Mascot mood="worried" size={96} />
        <p className="splash__text">Something went wrong on our side. Your account and chats are safe.</p>
        <button className="btn" onClick={() => window.location.reload()}>
          Reload
        </button>
      </div>
    )
  }
}
