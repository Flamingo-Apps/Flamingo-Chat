import { avatarFor } from '../format'

// The face of an anonymous name (see avatarFor). Swapping this one
// component is how the maintainer's own blob art will replace the emoji.
export function Avatar({ name, size = 44 }: { name: string; size?: number }) {
  const { glyph, hue } = avatarFor(name)
  return (
    <span
      className="avatar"
      aria-hidden="true"
      style={{
        width: size,
        height: size,
        fontSize: size * 0.5,
        background: `linear-gradient(145deg, hsl(${hue} 80% 62%), hsl(${(hue + 40) % 360} 75% 48%))`,
      }}
    >
      {glyph}
    </span>
  )
}
