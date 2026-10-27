import { createContext, useContext } from 'react'

// What a screen can ask the app shell to do without owning any modal
// itself. askForIdentity opens the right prompt for where the person is:
// the "get an identity" card for a guest, or the one-time gender choice
// for someone who signed in with Google but hasn't picked yet.
export interface UiValue {
  askForIdentity: () => void
}

export const UiContext = createContext<UiValue | null>(null)

export function useUi(): UiValue {
  const ctx = useContext(UiContext)
  if (!ctx) throw new Error('useUi must be used inside the app shell')
  return ctx
}
