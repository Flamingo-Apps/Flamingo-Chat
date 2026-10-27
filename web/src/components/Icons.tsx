// Small stroke icons, drawn here so the app needs no icon library.
import type { ReactNode } from 'react'

function Icon({ children, size = 22 }: { children: ReactNode; size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      {children}
    </svg>
  )
}

export const ShuffleIcon = () => (
  <Icon>
    <path d="M16 3h5v5M4 20 21 3M21 16v5h-5M15 15l6 6M4 4l5 5" />
  </Icon>
)
export const HashIcon = () => (
  <Icon>
    <path d="M4 9h16M4 15h16M10 3 8 21M16 3l-2 18" />
  </Icon>
)
export const InboxIcon = () => (
  <Icon>
    <path d="M22 12h-6l-2 3h-4l-2-3H2" />
    <path d="M5.5 5h13L22 12v6a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2v-6z" />
  </Icon>
)
export const LockIcon = ({ size }: { size?: number }) => (
  <Icon size={size ?? 16}>
    <rect x="4" y="11" width="16" height="10" rx="3" />
    <path d="M8 11V8a4 4 0 0 1 8 0v3" />
  </Icon>
)
export const CheckBadgeIcon = () => (
  <Icon size={16}>
    <path d="M20 6 9 17l-5-5" />
  </Icon>
)
export const SendIcon = () => (
  <Icon>
    <path d="m22 2-7 20-4-9-9-4zM22 2 11 13" />
  </Icon>
)
export const BackIcon = () => (
  <Icon>
    <path d="m15 18-6-6 6-6" />
  </Icon>
)
export const HeartIcon = () => (
  <Icon size={18}>
    <path d="M20.8 4.6a5.5 5.5 0 0 0-7.8 0L12 5.7l-1-1.1a5.5 5.5 0 0 0-7.8 7.8l1 1.1L12 21l7.8-7.5 1-1.1a5.5 5.5 0 0 0 0-7.8z" />
  </Icon>
)
export const SkipIcon = () => (
  <Icon size={18}>
    <path d="m5 4 10 8-10 8zM19 5v14" />
  </Icon>
)
export const FlagIcon = () => (
  <Icon size={18}>
    <path d="M4 22V4M4 4h13l-2 4 2 4H4" />
  </Icon>
)
export const PowerIcon = () => (
  <Icon size={18}>
    <path d="M18.4 6.6a9 9 0 1 1-12.8 0M12 2v10" />
  </Icon>
)
export const PlusIcon = () => (
  <Icon size={18}>
    <path d="M12 5v14M5 12h14" />
  </Icon>
)
