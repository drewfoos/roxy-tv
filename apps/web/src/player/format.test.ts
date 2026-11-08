import { describe, expect, it } from 'vitest'
import { fmt, trackLabel } from './format'
import type { MediaStream } from '../api/types'

describe('fmt', () => {
  it('formats under and over an hour', () => {
    expect(fmt(0)).toBe('0:00')
    expect(fmt(309)).toBe('5:09')
    expect(fmt(3909)).toBe('1:05:09')
  })
  it('treats nonsense as zero', () => {
    for (const v of [NaN, -5, Infinity]) expect(fmt(v)).toBe('0:00')
  })
})

describe('trackLabel', () => {
  const s = (p: Partial<MediaStream>) => ({ index: 3, type: 'audio', codec: 'aac', ...p }) as MediaStream
  it('names the language, the title and the channels', () => {
    expect(trackLabel(s({ language: 'jpn', title: 'Commentary', channels: 2 }))).toBe('Japanese · Commentary (stereo)')
    expect(trackLabel(s({ language: 'eng', channels: 6 }))).toBe('English (5.1)')
    expect(trackLabel(s({ language: 'eng', channels: 1 }))).toBe('English (mono)')
  })
  it('prefers the detected language, and drops "und" titles', () => {
    expect(trackLabel(s({ language: 'und', detectedLanguage: 'eng', title: 'und', type: 'subtitle' }))).toBe('English')
  })
  it('falls back to the track number', () => {
    expect(trackLabel(s({ type: 'subtitle' }))).toBe('Track 3')
  })
})
