import type { ReactNode } from 'react'

// The two-pane layout every main screen shares: a list or controls pane
// on the left and the conversation on the right. On a desktop both show
// side by side. On a phone only one does, so `hasChat` picks which.
export function Workspace({ list, chat, hasChat }: { list: ReactNode; chat: ReactNode; hasChat: boolean }) {
  return (
    <div className="workspace" data-view={hasChat ? 'chat' : 'list'}>
      <section className="workspace__list">{list}</section>
      <section className="workspace__chat">{chat}</section>
    </div>
  )
}

// What the right pane shows when no conversation is open.
export function EmptyChat({ emoji, title, text, children }: { emoji: string; title: string; text: string; children?: ReactNode }) {
  return (
    <div className="empty">
      <div className="empty__emoji">{emoji}</div>
      <h2>{title}</h2>
      <p>{text}</p>
      {children}
    </div>
  )
}
