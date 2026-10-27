import { useEffect, useState } from 'react'
import * as api from './api'
import { useGateway } from './gateway'

// useOnlineCount is the number shown next to the green dot: how many
// people are active right now. Once connected it comes from the gateway's
// own "presence" frame (pushed every few seconds over the WebSocket,
// already in app state). Until the first one arrives, and on the welcome
// screen where there is no connection at all, it falls back to the public
// GET /v1/presence, polled slowly - so even the very first screen can say
// how alive the place is.
export function useOnlineCount(): number | null {
  const { state } = useGateway()
  const [polled, setPolled] = useState<number | null>(null)
  const live = state.presence !== null

  useEffect(() => {
    if (live) return
    let cancelled = false
    const load = () =>
      api
        .getPresence()
        .then((p) => !cancelled && setPolled(p.total))
        .catch(() => {})
    load()
    const timer = setInterval(load, 20_000)
    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [live])

  return state.presence?.total ?? polled
}
