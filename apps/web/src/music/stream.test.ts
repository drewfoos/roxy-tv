import { describe, expect, it } from 'vitest'
import type { MusicTrack } from '../api/music'
import { startQueue } from './queue'
import { chooseStream, inAlbumRun, levelGain, nativeMime, runsOn, TARGET_LUFS } from './stream'

const song = (over: Partial<MusicTrack> = {}): MusicTrack => ({ id: 's', title: 'S', artist: 'A', artistId: 'a', durationMs: 200000, container: 'mp3', codec: 'mp3', bitrate: 320000, lossless: false, size: 1, favorite: false, playCount: 0, addedAt: '', ...over })
const flac = (over: Partial<MusicTrack> = {}) => song({ container: 'flac', codec: 'flac', lossless: true, bitrate: 2800000, bitDepth: 24, sampleRate: 96000, ...over })
const everything = () => true
const only = (...kinds: string[]) => (mime: string) => kinds.some((k) => mime.includes(k))

describe('what is asked of the server', () => {
  it('is the file itself when the browser plays it and nothing less was asked for', () => {
    expect(chooseStream(song(), 'original', everything)).toEqual({ url: '/v1/music/tracks/s/stream' })
    expect(chooseStream(flac(), 'original', everything).converted).toBeUndefined()
  })

  it('is a lossless conversion for a lossless file the browser cannot play', () => {
    const alac = song({ container: 'm4a', codec: 'alac', lossless: true })
    const s = chooseStream(alac, 'original', only('flac', 'mpeg', 'opus'))
    expect(s.url).toBe('/v1/music/tracks/s/stream?format=flac')
    expect(s.converted).toEqual({ codec: 'flac', kbps: 0 })
    // No FLAC either: the best lossy format it has.
    expect(chooseStream(alac, 'original', only('opus')).url).toBe('/v1/music/tracks/s/stream?format=opus&bitrate=256')
    expect(chooseStream(alac, 'original', only('mpeg')).url).toBe('/v1/music/tracks/s/stream?format=mp3&bitrate=320')
  })

  it('is a conversion for a file no browser plays (WMA, APE, DSD)', () => {
    const wma = song({ container: 'wma', codec: 'wmav2' })
    expect(nativeMime(wma)).toBe('')
    expect(chooseStream(wma, 'original', everything).converted).toEqual({ codec: 'opus', kbps: 256 })
    const dsd = song({ container: 'dsf', codec: 'dsd_lsbf_planar', lossless: true })
    expect(chooseStream(dsd, 'original', everything).converted?.codec).toBe('flac')
  })

  it('is a smaller stream when one is asked for, but never a larger one than the file', () => {
    expect(chooseStream(flac(), 128, everything).url).toBe('/v1/music/tracks/s/stream?format=opus&bitrate=128')
    expect(chooseStream(song({ bitrate: 320000 }), 192, everything).converted).toEqual({ codec: 'opus', kbps: 192 })
    // A 128 kbit/s MP3 asked for at 192: the file itself (converting it up gains nothing).
    expect(chooseStream(song({ bitrate: 128000 }), 192, everything).converted).toBeUndefined()
    expect(chooseStream(song({ bitrate: 200000 }), 192, everything).converted).toBeUndefined() // about what was asked for
    // The format follows the browser.
    expect(chooseStream(flac(), 320, only('mp4a', 'mpeg')).url).toBe('/v1/music/tracks/s/stream?format=aac&bitrate=320')
    expect(chooseStream(flac(), 320, everything).url).toBe('/v1/music/tracks/s/stream?format=opus&bitrate=256')
  })
})

describe('levelling', () => {
  it('brings a song to the common loudness', () => {
    expect(levelGain(song({ loudness: -8 }), 'on', false)).toBe(TARGET_LUFS + 8) // a loud master comes down 6 dB
    expect(levelGain(song({ loudness: -20, peak: -9 }), 'on', false)).toBe(6) // a quiet one goes up
    expect(levelGain(song({ loudness: -8 }), 'off', false)).toBe(0)
    expect(levelGain(song(), 'on', false)).toBe(0) // not measured, not tagged: left alone
  })

  it('never turns a song up into clipping, nor by more than a limit either way', () => {
    expect(levelGain(song({ loudness: -20, peak: -2 }), 'on', false)).toBe(2) // its peaks leave 2 dB
    expect(levelGain(song({ loudness: -20, peak: 0.5 }), 'on', false)).toBe(-0.5) // already over: brought under
    expect(levelGain(song({ loudness: -40, peak: -30 }), 'on', false)).toBe(9)
    expect(levelGain(song({ loudness: 10 }), 'on', false)).toBe(-18)
    expect(levelGain(song({ loudness: -70 }), 'on', false)).toBe(0) // silence
  })

  it("uses the album's loudness among the album's songs", () => {
    const quiet = song({ loudness: -22, albumLoudness: -10 })
    expect(levelGain(quiet, 'on', true)).toBe(-4) // as the album: the quiet song stays quiet
    expect(levelGain(quiet, 'on', false)).toBe(8) // by itself: brought up
    expect(levelGain(song({ loudness: -9 }), 'on', true)).toBe(-5) // album not measured yet: the song's own
  })

  it('falls back on the ReplayGain tag', () => {
    expect(levelGain(song({ gainTrack: -7 }), 'on', false)).toBe(-3) // -18 - (-7) = -11 LUFS
    expect(levelGain(song({ gainTrack: -7, gainAlbum: -5 }), 'on', true)).toBe(-1)
    expect(levelGain(song({ gainTrack: -7, loudness: -14 }), 'on', false)).toBe(0) // the measurement wins
  })
})

describe('songs that run into each other', () => {
  const t = (id: string, album: string, track: number, disc = 1) => song({ id, albumId: album, track, disc })
  it('are neighbours on one album in its order', () => {
    const q = startQueue([t('a', 'X', 1), t('b', 'X', 2), t('c', 'X', 4), t('d', 'Y', 5), t('e', 'Y', 6, 2), song({ id: 'f' })], 0, false)
    expect([0, 1, 2, 3, 4, 5].map((i) => runsOn(q, i))).toEqual([true, true, false, false, false, false])
    // Among its album's songs (for levelling), whatever the order.
    expect([0, 1, 2, 3, 4, 5].map((i) => inAlbumRun(q, i))).toEqual([true, true, true, true, true, false])
  })
  it('are not two files of one song, nor songs without a track number', () => {
    const q = startQueue([t('a', 'X', 0), t('b', 'X', 1), t('b', 'X', 1)], 0, false)
    expect(runsOn(q, 0)).toBe(false)
    expect(inAlbumRun(q, 2)).toBe(false)
  })
})
