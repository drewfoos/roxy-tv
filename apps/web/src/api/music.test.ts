import { describe, expect, it } from 'vitest'
import { clock, codecName, isHiRes, longDuration, music, qualityLabel, qualityMark } from './music'

const file = (over: Partial<Parameters<typeof qualityLabel>[0]>) => ({ codec: 'mp3', container: 'mp3', lossless: false, ...over })

describe('how a song is encoded, in words', () => {
  it('names a lossless file by its resolution', () => {
    expect(qualityLabel(file({ codec: 'flac', container: 'flac', lossless: true, bitDepth: 24, sampleRate: 96000 }))).toBe('FLAC · 24-bit / 96 kHz')
    expect(qualityLabel(file({ codec: 'flac', container: 'flac', lossless: true, bitDepth: 16, sampleRate: 44100 }))).toBe('FLAC · 16-bit / 44.1 kHz')
    expect(qualityLabel(file({ codec: 'flac', container: 'flac', lossless: true }))).toBe('FLAC')
    expect(qualityLabel(file({ codec: 'pcm_s16le', container: 'wav', lossless: true, bitDepth: 16, sampleRate: 48000 }))).toBe('WAV · 16-bit / 48 kHz')
  })
  it('names the rest by its bitrate', () => {
    expect(qualityLabel(file({ bitrate: 320000 }))).toBe('MP3 · 320 kbps')
    expect(qualityLabel(file({ bitrate: 281457 }))).toBe('MP3 · 281 kbps')
    expect(qualityLabel(file({ codec: 'aac', container: 'm4a' }))).toBe('AAC')
    expect(codecName({ codec: 'somethingnew', container: 'x' })).toBe('SOMETHINGNEW')
  })
  it('marks what is better than a CD', () => {
    expect(isHiRes({ lossless: true, bitDepth: 24, sampleRate: 44100 })).toBe(true)
    expect(isHiRes({ lossless: true, bitDepth: 16, sampleRate: 96000 })).toBe(true)
    expect(isHiRes({ lossless: true, bitDepth: 16, sampleRate: 48000 })).toBe(false)
    expect(isHiRes({ lossless: false, bitDepth: 24, sampleRate: 96000 })).toBe(false) // a lossy file is never hi-res
    expect(qualityMark({ lossless: true, bitDepth: 24, sampleRate: 96000 })).toBe('Hi-Res')
    expect(qualityMark({ lossless: true, bitDepth: 16, sampleRate: 44100 })).toBe('Lossless')
    expect(qualityMark({ lossless: false })).toBe('')
  })
})

describe('times', () => {
  it('reads as a clock', () => {
    expect(clock(0)).toBe('0:00')
    expect(clock(187000)).toBe('3:07')
    expect(clock(3765000)).toBe('1:02:45')
    expect(clock(-5)).toBe('0:00')
  })
  it('reads as a length', () => {
    expect(longDuration(20000)).toBe('1 min')
    expect(longDuration(42 * 60000)).toBe('42 min')
    expect(longDuration(72 * 60000)).toBe('1 hr 12 min')
    expect(longDuration(120 * 60000)).toBe('2 hr')
  })
})

describe('addresses', () => {
  it("builds a cover's, with its size when one is asked for", () => {
    expect(music.artUrl('abc', 320)).toBe('/v1/music/art/abc?w=320')
    expect(music.artUrl('abc')).toBe('/v1/music/art/abc')
    expect(music.artUrl(undefined, 320)).toBeNull()
    expect(music.artUrl('', 320)).toBeNull()
  })
  it("builds a song's", () => {
    expect(music.streamUrl('a b')).toBe('/v1/music/tracks/a%20b/stream')
  })
})
