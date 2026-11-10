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
  // `Show.en.hi.srt` beside `Show.en.srt`: without the mark the menu lists two "English".
  it('marks subtitles for the hard of hearing', () => {
    const sub = (p: Partial<MediaStream>) => s({ type: 'subtitle', language: 'eng', ...p })
    expect(trackLabel(sub({ isHearingImpaired: true }))).toBe('English · SDH')
    expect(trackLabel(sub({ isHearingImpaired: true, title: 'Full' }))).toBe('English · Full · SDH')
    expect(trackLabel(sub({ isHearingImpaired: true, title: 'English [SDH]' }))).toBe('English · English [SDH]')
    expect(trackLabel(sub({ isHearingImpaired: true, title: 'Closed captions' }))).toBe('English · Closed captions')
    expect(trackLabel(sub({ isHearingImpaired: false, title: 'Signs' }))).toBe('English · Signs')
    expect(trackLabel(s({ type: 'subtitle', isHearingImpaired: true }))).toBe('SDH')
  })
})
