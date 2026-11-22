import { describe, expect, it } from 'vitest'
import { chooseStream, classHeight, MAX_REOPENS, QUALITY_KBPS, QUALITY_STEPS, RECOVERED_AFTER_SEC, ReopenBudget, reopenAt, resolutionLabel, smallerSteps, startQuality, StreamClock, streamEnded, type Mode } from './stream'

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

// The quality saved in Preferences is for saving data: it must not re-encode a file that
// is already that small. It used to: "1080p" turned every 1080p file into a transcode.
describe('startQuality', () => {
  it('plays a file no taller than the preference as it is', () => {
    expect(startQuality(1080, 1080)).toBe('original')
    expect(startQuality(1080, 720)).toBe('original')
    expect(startQuality(720, 480)).toBe('original')
    expect(startQuality(480, 480)).toBe('original')
  })
  it('counts a file a few lines taller as that size (the gear menu does not offer it either)', () => {
    expect(startQuality(1080, 1088)).toBe('original')
    expect(startQuality(720, 760)).toBe('original') // 720 >= 760 * 0.94
    expect(startQuality(720, 800)).toBe(720)
  })
  it('lowers a taller file', () => {
    expect(startQuality(1080, 2160)).toBe(1080)
    expect(startQuality(720, 1080)).toBe(720)
    expect(startQuality(480, 1080)).toBe(480)
  })
  it('keeps the preference when the height is unknown', () => {
    expect(startQuality(720, 0)).toBe(720)
  })
  // The web knew nothing above 1080p: a saved 1440p played a 4K file as it is.
  it('lowers a 4K file to 1440p, and a larger one to 4K', () => {
    expect(startQuality(1440, 2160)).toBe(1440)
    expect(startQuality(2160, 4320)).toBe(2160)
  })
  it('plays a file no larger than 4K or 1440p as it is', () => {
    expect(startQuality('original', 2160)).toBe('original')
    expect(startQuality(2160, 2160)).toBe('original')
    expect(startQuality(2160, 1080)).toBe('original')
    expect(startQuality(1440, 1440)).toBe('original')
    expect(startQuality(1440, 1080)).toBe('original')
  })
  it('takes a size that is not offered as the file itself', () => {
    expect(startQuality(900, 2160)).toBe('original')
    expect(startQuality(4320, 4320)).toBe('original')
  })
})

// A film wider than 16:9 has fewer lines than its size's name says: 3840 x 1604 is 4K. By
// height alone it read as "1604p" and was offered nothing between itself and 1080p.
describe('classHeight', () => {
  it('is the height of a 16:9 picture of that width for wide films', () => {
    expect(classHeight(3840, 1604)).toBe(2160)
    expect(classHeight(1920, 802)).toBe(1080)
    expect(classHeight(1920, 1038)).toBe(1080)
    expect(classHeight(2560, 1072)).toBe(1440)
  })
  it('is the height itself for 16:9 and narrower pictures', () => {
    expect(classHeight(1920, 1080)).toBe(1080)
    expect(classHeight(1440, 1080)).toBe(1080)
    expect(classHeight(960, 720)).toBe(720)
  })
  it('is the height when the width is not known, and 0 when nothing is', () => {
    expect(classHeight(undefined, 1080)).toBe(1080)
    expect(classHeight(0, 720)).toBe(720)
    expect(classHeight(1920, undefined)).toBe(0)
    expect(classHeight(undefined, undefined)).toBe(0)
  })
})

describe('resolutionLabel', () => {
  it('names a size as people do', () => {
    expect(resolutionLabel(2160)).toBe('4K')
    expect(resolutionLabel(4320)).toBe('8K')
    expect(resolutionLabel(1440)).toBe('1440p')
    expect(resolutionLabel(1080)).toBe('1080p')
    expect(resolutionLabel(480)).toBe('480p')
  })
  it('counts a few lines short as that size', () => {
    expect(resolutionLabel(1038)).toBe('1080p')
    expect(resolutionLabel(2076)).toBe('4K')
  })
  it('says the lines for a size with no name, and nothing for none', () => {
    expect(resolutionLabel(240)).toBe('240p')
    expect(resolutionLabel(0)).toBe('')
  })
})

describe('smallerSteps', () => {
  it('offers a 4K file 1440p and everything under it, not 4K again', () => {
    expect(smallerSteps(2160)).toEqual([1440, 1080, 720, 480])
    expect(smallerSteps(classHeight(3840, 1604))).toEqual([1440, 1080, 720, 480])
  })
  it('offers a 1080p file 720p and 480p', () => {
    expect(smallerSteps(1080)).toEqual([720, 480])
    expect(smallerSteps(classHeight(1920, 802))).toEqual([720, 480])
  })
  it('does not offer a size a few lines under the file as a conversion', () => {
    expect(smallerSteps(1088)).toEqual([720, 480])
    expect(smallerSteps(760)).toEqual([480])
  })
  it('offers everything when the size is not known, and 4K only above 4K', () => {
    expect(smallerSteps(0)).toEqual([...QUALITY_STEPS])
    expect(smallerSteps(4320)).toEqual([2160, 1440, 1080, 720, 480])
  })
  it('asks for the bitrates the apps ask for', () => {
    expect(QUALITY_KBPS).toEqual({ 2160: 40000, 1440: 16000, 1080: 8000, 720: 4000, 480: 1500 })
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

// Found by an audit: out of reopenings, a cut stream was taken for the end (marked
// watched at the header's duration, autoplay moved on). It is 'lost' now, never 'end'.
describe('streamEnded', () => {
  const server: Mode = { kind: 'server', audio: 2, offset: 120 }
  it('reopens, then says lost (not the end) when cut too often in a row', () => {
    expect(streamEnded(server, 30, 1440, 0)).toEqual({ kind: 'reopen', at: 150 })
    expect(streamEnded(server, 30, 1440, MAX_REOPENS)).toEqual({ kind: 'lost', at: 150 })
  })
  it('takes the real end for the end, out of reopenings or not', () => {
    expect(streamEnded(server, 1312, 1440, 0)).toEqual({ kind: 'end' })
    expect(streamEnded(server, 1312, 1440, MAX_REOPENS)).toEqual({ kind: 'end' })
    expect(streamEnded({ kind: 'direct' }, 30, 1440, 0)).toEqual({ kind: 'end' })
  })
})

describe('ReopenBudget', () => {
  const server: Mode = { kind: 'server', audio: null, offset: 0 }

  // What Player.tsx does when a server stream stops: ask where to reopen, count it.
  const tryReopen = (b: ReopenBudget, playedTo: number) => {
    const at = reopenAt(server, playedTo, 1440, b.used)
    if (at !== null) b.reopened(at)
    return at
  }

  // Found by a review: the count used to be reset by the browser's 'playing' event, which
  // fires as soon as a reopened stream starts, so a stream dying 2 s after every
  // reopening was reopened for ever. Brief recoveries no longer reset it.
  it('gives up on a stream that keeps dying shortly after each reopening', () => {
    const b = new ReopenBudget()
    let pos = 100
    for (let i = 0; i < MAX_REOPENS; i++) {
      expect(tryReopen(b, pos)).toBe(pos)
      pos += 2 // plays 2 s ('playing' fires), then dies again
      b.progress(pos)
    }
    expect(tryReopen(b, pos)).toBeNull()
  })

  it('counts a stream that played on long enough as recovered', () => {
    const b = new ReopenBudget()
    expect(tryReopen(b, 100)).toBe(100)
    expect(tryReopen(b, 101)).toBe(101)
    b.progress(101 + RECOVERED_AFTER_SEC - 1)
    expect(b.used).toBe(2) // not yet
    b.progress(101 + RECOVERED_AFTER_SEC)
    expect(b.used).toBe(0) // recovered: a later drop gets its full allowance
    for (let i = 0; i < MAX_REOPENS; i++) expect(tryReopen(b, 500 + i)).not.toBeNull()
    expect(tryReopen(b, 510)).toBeNull()
  })

  it('starts afresh for a new title', () => {
    const b = new ReopenBudget()
    for (let i = 0; i < MAX_REOPENS; i++) tryReopen(b, 100 + i)
    expect(tryReopen(b, 200)).toBeNull()
    b.reset()
    expect(tryReopen(b, 10)).toBe(10)
  })
})
