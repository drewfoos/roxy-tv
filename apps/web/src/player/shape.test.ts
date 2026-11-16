import { describe, expect, it } from 'vitest'
import { pictureShape } from './shape'

describe('pictureShape', () => {
  it('is the picture\'s own shape', () => {
    expect(pictureShape(1920, 1080)).toBe('1920 / 1080')
    expect(pictureShape(1920, 804)).toBe('1920 / 804')
    expect(pictureShape(1440, 1080)).toBe('1440 / 1080')
  })
  it('is unknown without a size', () => {
    expect(pictureShape()).toBeNull()
    expect(pictureShape(0, 0)).toBeNull()
    expect(pictureShape(1920, 0)).toBeNull()
    expect(pictureShape(NaN, 1080)).toBeNull()
  })
})
