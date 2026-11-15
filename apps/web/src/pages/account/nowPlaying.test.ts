import { describe, expect, it } from 'vitest'
import { audioText, livePositionMs, outputText, resolution, subtitleText, tally, timeLeft, videoText } from './nowPlaying'

describe('now playing words', () => {
  it('names the video by its width, so a cropped film is still 1080p', () => {
    expect(resolution(1920, 804)).toBe('1080p')
    expect(resolution(1280, 720)).toBe('720p')
    expect(resolution(3840, 1600)).toBe('2160p')
    expect(resolution(undefined, 480)).toBe('480p')
    expect(resolution()).toBeNull()
  })
  it('describes the tracks', () => {
    expect(videoText({ codec: 'hevc', width: 1920, height: 1080, bitDepth: 10, range: 'SDR' })).toBe('HEVC · 1080p · 10-bit')
    expect(videoText({ codec: 'h264', width: 3840, height: 2160, bitDepth: 8, range: 'HDR10' })).toBe('H.264 · 2160p · HDR10')
    expect(audioText({ codec: 'eac3', channels: 6, language: 'jpn' })).toBe('Japanese · E-AC3 5.1')
    expect(audioText({ codec: 'aac', channels: 2 })).toBe('AAC stereo')
    expect(subtitleText({ codec: 'ass', language: 'eng', title: 'Dialogue' })).toBe('English · Dialogue · ASS')
    expect(subtitleText({ codec: 'subrip', language: 'eng', sdh: true })).toBe('English · SDH · SRT')
  })
  it('describes what a track is converted to', () => {
    expect(outputText({ codec: 'h264', height: 720, bitrateK: 4000 })).toBe('H.264 · 720p · 4 Mbps')
    expect(outputText({ codec: 'h264', height: 480, bitrateK: 1500 })).toBe('H.264 · 480p · 1.5 Mbps')
    expect(outputText({ codec: 'aac', channels: 6 })).toBe('AAC 5.1')
  })
  it('counts a playing session on from its last report, and a paused one not at all', () => {
    const s = { state: 'playing' as const, positionMs: 80_000, durationMs: 100_000, ageMs: 4_000 }
    expect(livePositionMs(s, 3_000)).toBe(87_000)
    expect(livePositionMs(s, 60_000)).toBe(100_000) // never past the end
    expect(livePositionMs({ ...s, state: 'paused' }, 3_000)).toBe(80_000)
    expect(livePositionMs({ ...s, durationMs: 0 }, 1_000)).toBe(85_000)
  })
  it('says how long is left', () => {
    expect(timeLeft(0, 24 * 60_000)).toBe('24 min left')
    expect(timeLeft(30_000, 95 * 60_000)).toBe('1 h 35 min left')
    expect(timeLeft(10, 0)).toBeNull()
    expect(timeLeft(5000, 5000)).toBeNull()
  })
  it('tallies the sessions', () => {
    expect(tally([{ state: 'playing' }, { state: 'playing' }, { state: 'paused' }])).toBe('2 playing, 1 paused')
    expect(tally([{ state: 'paused' }])).toBe('1 paused')
    expect(tally([])).toBe('')
  })
})
