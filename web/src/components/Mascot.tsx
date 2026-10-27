// Mochi, a cat-shaped blob who sits on the prompts and empty screens so
// that asking something of a person (sign in, pick a gender) feels like a
// friend asking, not a gate. Drawn in the logo's style: a thick dark
// outline, flat pink fills. The moods only change the face and one prop.
export type Mood = 'wave' | 'shy' | 'sleepy' | 'love' | 'search'

const INK = '#16101c'
const BODY = '#ffb3d4'
const SOFT = '#ffd6e8'
const HOT = '#ff5fa2'
const stroke = { stroke: INK, strokeWidth: 4, strokeLinecap: 'round', strokeLinejoin: 'round' } as const
const line = { ...stroke, fill: 'none' } as const

export function Mascot({ mood = 'wave', size = 120 }: { mood?: Mood; size?: number }) {
  const happyEyes = mood === 'wave' || mood === 'shy' || mood === 'love'
  return (
    <svg className="mascot" width={size} height={size} viewBox="0 0 120 120" role="img" aria-label="Mochi, Flamingo's friendly mascot">
      {/* ears, behind the body */}
      <path d="M31 46 27 18 54 33Z" fill={BODY} {...stroke} />
      <path d="M89 46 93 18 66 33Z" fill={BODY} {...stroke} />
      <path d="M34 38 32 26 43 32Z" fill={HOT} stroke="none" />
      <path d="M86 38 88 26 77 32Z" fill={HOT} stroke="none" />

      {/* body and belly */}
      <path d="M22 72C22 44 38 30 60 30s38 14 38 42c0 22-14 34-38 34S22 94 22 72Z" fill={BODY} {...stroke} />
      <ellipse cx="60" cy="93" rx="19" ry="10" fill={SOFT} />
      <ellipse cx="44" cy="106" rx="9" ry="5.5" fill={BODY} {...stroke} />
      <ellipse cx="76" cy="106" rx="9" ry="5.5" fill={BODY} {...stroke} />

      {/* face */}
      {happyEyes && (
        <>
          <path d="M42 68Q47 60 52 68" {...line} strokeWidth={3.6} />
          <path d="M68 68Q73 60 78 68" {...line} strokeWidth={3.6} />
        </>
      )}
      {mood === 'sleepy' && (
        <>
          <path d="M42 68H53" {...line} strokeWidth={3.6} />
          <path d="M67 68H78" {...line} strokeWidth={3.6} />
        </>
      )}
      {mood === 'search' && (
        <>
          <circle cx="47" cy="66" r="4" fill={INK} />
          <circle cx="73" cy="66" r="4" fill={INK} />
        </>
      )}
      <ellipse cx="36" cy="77" rx="6" ry="4" fill={HOT} opacity="0.55" />
      <ellipse cx="84" cy="77" rx="6" ry="4" fill={HOT} opacity="0.55" />
      {mood !== 'shy' && <path d={mood === 'sleepy' || mood === 'search' ? 'M56 77Q60 80 64 77' : 'M54 76Q57 81 60 76Q63 81 66 76'} {...line} strokeWidth={3.2} />}

      {/* the one prop that gives the mood away */}
      {mood === 'wave' && (
        <>
          <path d="M96 80Q112 72 108 54" {...line} />
          <circle cx="108" cy="50" r="7" fill={BODY} {...stroke} />
        </>
      )}
      {mood === 'shy' && (
        <>
          <circle cx="51" cy="87" r="7" fill={BODY} {...stroke} />
          <circle cx="69" cy="87" r="7" fill={BODY} {...stroke} />
        </>
      )}
      {mood === 'sleepy' && (
        <>
          <text x="94" y="36" fontSize="17" fontWeight="900" fill={INK}>
            z
          </text>
          <text x="104" y="22" fontSize="11" fontWeight="900" fill={INK}>
            z
          </text>
        </>
      )}
      {mood === 'love' && <path d="M100 42C92 35 92 25 98 25c3 0 3 2 3 3 0-1 0-3 3-3 6 0 6 10-4 17Z" fill={HOT} stroke={INK} strokeWidth={3} strokeLinejoin="round" />}
      {mood === 'search' && (
        <>
          <circle cx="98" cy="68" r="13" fill="#fff" fillOpacity="0.8" {...stroke} />
          <path d="M107 78 116 91" {...line} strokeWidth={5} />
        </>
      )}
    </svg>
  )
}
