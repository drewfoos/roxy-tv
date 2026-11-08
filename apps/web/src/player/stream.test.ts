import { describe, expect, it } from 'vitest'
import { chooseStream, MAX_REOPENS, reopenAt, StreamClock, type Mode } from './stream'

const aac = { index: 1, codec: 'aac' }
const ac3 = { index: 1, codec: 'AC3' }
const opus2 = { index: 2, codec: 'opus' }

describe('chooseStream', () => {
  it('plays the file itself when the browser decodes its first audio track', () => {
    expect(chooseStream('original', [aac, opus2], null)).toBe('direct')
    expect(chooseStream('original', [aac, opus2], 1)).toBe('direct')
    expect(chooseStream('original', [], null)).toBe('direct') // no audio at all
  })
  it('remuxes when only the audio is the problem', () => {
    expect(chooseStream('original', [ac3], null)).toBe('remux') // AC3: no browser plays it
    expect(chooseStream('original', [aac, opus2], 2)).toBe('remux') // another track: the browser cannot switch MKV audio
  })
  it('falls back to the first track when the asked one does not exist', () => {
    expect(chooseStream('original', [aac, opus2], 9)).toBe('direct')
  })
  it('transcodes for any quality but the original', () => {
    expect(chooseStream(720, [aac], null)).toBe('transcode')
    expect(chooseStream(1080, [ac3], null)).toBe('transcode')
  })
})

// A <video> as the clock sees it.
const video = (currentTime: number, seeking = false, playbackRate = 1) => ({ currentTime, seeking, playbackRate })

// Plays from `from` to `to` in quarter-second time updates, as Chrome sends them.
function play(c: StreamClock, from: number, to: number, server = true, rate = 1) {
  for (let t = from; t <= to + 1e-9; t += 0.25 * rate) expect(c.tick(video(t, false, rate), server)).toBe(true)
}

describe('StreamClock', () => {
  it('follows normal playback', () => {
    const c = new StreamClock()
    play(c, 0, 13)
    expect(c.playedTo).toBeCloseTo(13)
  })

  // The case it exists for: a server stream that ran out at 13 s. Chrome moves the
  // playhead to the header's duration (600 s), pauses, and says it ended.
  it('does not take a cut stream\'s jump to the duration for the viewer\'s position', () => {
    const c = new StreamClock()
    play(c, 0, 13)
    expect(c.tick(video(600), true)).toBe(false) // not shown, not reported
    expect(c.playedTo).toBeCloseTo(13)
    expect(c.pauseIsReal(video(600), true)).toBe(false) // the pause there is not reported either
    expect(reopenAt({ kind: 'server', audio: null, offset: 0 }, c.playedTo, 600, 0)).toBeCloseTo(13)
  })

  it('takes a seek (and anything during it) as the new position', () => {
    const c = new StreamClock()
    play(c, 0, 5)
    expect(c.tick(video(300, true), true)).toBe(true) // seeking
    c.reset(video(300)) // seeked
    play(c, 300, 302)
    expect(c.playedTo).toBeCloseTo(302)
  })

  it('starts again at 0 with a new stream', () => {
    const c = new StreamClock()
    play(c, 0, 40)
    c.reset(video(0)) // loadstart: a reopened stream's clock starts again
    play(c, 0, 2)
    expect(c.playedTo).toBeCloseTo(2)
  })

  it('allows the bigger steps of faster playback', () => {
    const c = new StreamClock()
    play(c, 0, 20, true, 4)
    expect(c.playedTo).toBeGreaterThan(19)
  })

  it('never second-guesses direct play', () => {
    const c = new StreamClock()
    play(c, 0, 5, false)
    expect(c.tick(video(500), false)).toBe(true)
    expect(c.playedTo).toBe(500)
    expect(c.pauseIsReal(video(500), false)).toBe(true)
  })

  it('reports a pause where the viewer is', () => {
    const c = new StreamClock()
    play(c, 0, 13)
    expect(c.pauseIsReal(video(13), true)).toBe(true)
  })
})

describe('reopenAt', () => {
  const server: Mode = { kind: 'server', audio: 2, offset: 120 }
  it('reopens a server stream that stopped well before the end, at its absolute time', () => {
    expect(reopenAt(server, 30, 1440, 0)).toBe(150) // offset 120 + 30 s played
  })
  it('lets the real end be the end', () => {
    expect(reopenAt(server, 1312, 1440, 0)).toBeNull() // 8 s from the end
  })
  it('gives up after a few tries in a row', () => {
    expect(reopenAt(server, 30, 1440, MAX_REOPENS - 1)).toBe(150)
    expect(reopenAt(server, 30, 1440, MAX_REOPENS)).toBeNull()
  })
  it('leaves direct play and unknown durations alone', () => {
    expect(reopenAt({ kind: 'direct' }, 30, 1440, 0)).toBeNull()
    expect(reopenAt(server, 30, 0, 0)).toBeNull()
  })
})
