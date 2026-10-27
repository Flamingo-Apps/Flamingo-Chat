import cheer from '../assets/mochi/mochi-cheer.webp'
import hello from '../assets/mochi/mochi-hello.webp'
import peek from '../assets/mochi/mochi-peek.webp'
import point from '../assets/mochi/mochi-point.webp'
import read from '../assets/mochi/mochi-read.webp'
import search from '../assets/mochi/mochi-search.webp'
import think from '../assets/mochi/mochi-think.webp'
import worried from '../assets/mochi/mochi-worried.webp'

// Mochi, a cat-shaped blob drawn by the maintainer, who sits on the
// prompts and empty screens so that being asked something (sign in, pick a
// gender) feels like a friend asking, not a gate. One pose per feeling:
//   hello   - greeting, the default
//   point   - "over here", for showing someone where to look
//   think   - weighing something up
//   search  - looking for someone
//   cheer   - something went well
//   worried - something went wrong, or someone is leaving
//   read    - quiet places: empty lists
//   peek    - looking into something (reports)
export type Mood = 'hello' | 'point' | 'think' | 'search' | 'cheer' | 'worried' | 'read' | 'peek'

const POSES: Record<Mood, string> = { hello, point, think, search, cheer, worried, read, peek }

// `size` is the height in pixels; each pose keeps its own proportions.
export function Mascot({ mood = 'hello', size = 120 }: { mood?: Mood; size?: number }) {
  return <img className="mascot" src={POSES[mood]} height={size} alt="Mochi, Flamingo's mascot" style={{ height: size, width: 'auto' }} />
}
