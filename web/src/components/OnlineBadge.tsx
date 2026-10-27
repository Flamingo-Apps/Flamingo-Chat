import { formatCount } from '../format'
import { useGateway } from '../gateway'
import { useOnlineCount } from '../presence'

// The green dot with the live number beside it. The dot doubles as the
// connection light: green when the WebSocket is up, yellow while it
// connects, red when it dropped and is retrying.
export function OnlineBadge() {
  const { state } = useGateway()
  const count = useOnlineCount()
  const status = state.account ? state.connectionStatus : 'open'

  let text = 'Connecting...'
  if (status === 'closed') text = 'Reconnecting...'
  else if (count !== null) text = ''

  return (
    <div className={`online online--${status}`} title={status === 'open' ? 'Connected' : text}>
      <span className="online__dot" />
      {count !== null && status === 'open' ? (
        <span>
          <b>{formatCount(count)}</b> online
        </span>
      ) : (
        <span>{text || 'Connecting...'}</span>
      )}
    </div>
  )
}
