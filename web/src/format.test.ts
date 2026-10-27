import { describe, expect, it } from 'vitest'
import { avatarFor, formatCount } from './format'

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
