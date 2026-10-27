import { describe, expect, it } from 'vitest'
import { avatarFor, dayLabel, formatCount } from './format'

describe('formatCount', () => {
  it('writes small numbers plainly and groups thousands', () => {
    expect(formatCount(128)).toBe('128')
    expect(formatCount(1234)).toBe('1,234')
  })
})

describe('avatarFor', () => {
  it('gives the same look to the same pseudonym every time', () => {
    expect(avatarFor('Swift Fox')).toEqual(avatarFor('Swift Fox'))
  })

  it('picks the animal from the last word', () => {
    expect(avatarFor('Swift Fox').glyph).toBe('🦊')
    expect(avatarFor('Quiet Otter').glyph).toBe('🦦')
  })

  it('falls back to the first letter for a name it has no animal for', () => {
    expect(avatarFor('Brave Toucan').glyph).toBe('B')
    expect(avatarFor('').glyph).toBe('?')
  })

  it('keeps the hue inside the colour wheel', () => {
    for (const name of ['Swift Fox', 'Quiet Otter', 'A', 'Calm Wolf', '']) {
      const { hue } = avatarFor(name)
      expect(hue).toBeGreaterThanOrEqual(0)
      expect(hue).toBeLessThan(360)
    }
  })
})

describe('dayLabel', () => {
  const now = new Date(2026, 9, 27, 15, 0) // 27 Oct 2026, 15:00 local
  const at = (y: number, m: number, d: number, h = 12) => new Date(y, m, d, h).getTime() / 1000

  it('says Today for any time earlier the same day', () => {
    expect(dayLabel(at(2026, 9, 27, 0), now)).toBe('Today')
    expect(dayLabel(at(2026, 9, 27, 14), now)).toBe('Today')
  })

  it('says Yesterday for the previous calendar day, even late at night', () => {
    expect(dayLabel(at(2026, 9, 26, 23), now)).toBe('Yesterday')
  })

  it('writes the date for older messages, with the year only when it differs', () => {
    // The order of day and month follows the viewer's locale, so check the parts.
    const sameYear = dayLabel(at(2026, 9, 20), now)
    expect(sameYear).toContain('20')
    expect(sameYear).toContain('Oct')
    expect(sameYear).not.toContain('2026')
    expect(dayLabel(at(2025, 11, 31), now)).toContain('2025')
  })
})
