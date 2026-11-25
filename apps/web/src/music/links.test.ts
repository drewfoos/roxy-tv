import { describe, expect, it } from 'vitest'
import { isSongPath, onOpenNowPlaying, openNowPlaying, songLink, songPath } from './links'

describe("a song's own address", () => {
  it('is the same wherever it is made', () => {
    expect(songPath('8e0053e3')).toBe('/music/song/8e0053e3')
    expect(songPath('a b/c')).toBe('/music/song/a%20b%2Fc')
    expect(songLink('x', 'http://roxy.home:8097')).toBe('http://roxy.home:8097/music/song/x')
    expect(isSongPath('/music/song/x')).toBe(true)
    expect(isSongPath('/music')).toBe(false)
    expect(isSongPath('/music/album/song')).toBe(false)
  })

  it('opens "now playing" at once when the bar is there, and as soon as it is when it is not', () => {
    // Asked before the bar is there (a page loaded by a song's link): kept, once.
    openNowPlaying()
    let opened = 0
    const stop = onOpenNowPlaying(() => opened++)
    expect(opened).toBe(1)
    openNowPlaying()
    expect(opened).toBe(2)
    stop()
    // A bar that comes later is not handed a wish that was granted already.
    const again = onOpenNowPlaying(() => opened++)
    expect(opened).toBe(2)
    again()
  })
})
