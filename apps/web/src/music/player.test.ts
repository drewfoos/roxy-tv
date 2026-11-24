// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { MusicTrack } from '../api/music'
import { session } from '../api/session'
import { ERROR_SHOWN_MS, currentTrack, player, resetPlayerForTests, restorePlayer } from './player'
import { FakeAudio } from './fakeAudio'

const song = (id: string, seconds = 200): MusicTrack => ({ id, title: 'Song ' + id, artist: 'A', artistId: 'a', durationMs: seconds * 1000, container: 'mp3', codec: 'mp3', lossless: false, size: 1, favorite: false, playCount: 0, addedAt: '' })
const songs = (...ids: string[]) => ids.map((id) => song(id))
const S = { token: 't', userId: 'u', userName: 'drew', isAdmin: false, profileId: 'p', profileName: 'Drew', profileHasAvatar: false, profileAvatarKey: '' }

/** The element that is sounding: the one whose file is the current song's. */
const sounding = () => FakeAudio.made.find((a) => a.src.endsWith(`/v1/music/tracks/${currentTrack()!.id}/stream`) && !a.paused) ?? null
const playing = () => currentTrack()?.id
let calls: { method: string; url: string }[]
let library: MusicTrack[]

beforeEach(() => {
  FakeAudio.made = []
  FakeAudio.broken = false
  calls = []
  library = songs('a', 'b', 'c', 'd')
  vi.stubGlobal('Audio', FakeAudio)
  vi.stubGlobal(
    'fetch',
    vi.fn((url: string, init?: { method?: string }) => {
      calls.push({ method: init?.method ?? 'GET', url })
      const body = url.startsWith('/v1/music/tracks?') ? { total: library.length, tracks: library.filter((t) => decodeURIComponent(url).includes(t.id)) } : { playCount: 1 }
      return Promise.resolve({ ok: true, status: 200, headers: { get: () => 'application/json' }, text: async () => JSON.stringify(body) })
    }),
  )
  localStorage.clear()
  session.set(S)
  resetPlayerForTests()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

const counted = () => calls.filter((c) => c.method === 'POST' && c.url.endsWith('/played')).map((c) => c.url.split('/')[4])

describe('playing', () => {
  it('plays the list from the chosen song', () => {
    player.playTracks(library, 1, { shuffle: false })
    expect(playing()).toBe('b')
    expect(player.get().playing).toBe(true)
    expect(sounding()?.src).toBe('/v1/music/tracks/b/stream')
    expect(player.progress().duration).toBe(200) // the library's length until the file says its own
  })

  it('goes on to the next song when one ends, and stops at the end of the queue', () => {
    player.playTracks(library, 2, { shuffle: false })
    sounding()!.finish()
    expect(playing()).toBe('d')
    expect(player.get().playing).toBe(true)
    sounding()!.finish()
    expect(playing()).toBe('d') // still the last song, ready to play again
    expect(player.get().playing).toBe(false)
    expect(player.progress().position).toBe(0)
  })

  it('has the next song loaded before the current one ends, and hands over to it', () => {
    player.playTracks(library, 0, { shuffle: false })
    const first = sounding()!
    first.playTo(175, 200)
    expect(FakeAudio.made.some((a) => a.src.endsWith('/b/stream'))).toBe(false) // 25 s to go: not yet
    first.playTo(185)
    const second = FakeAudio.made.find((a) => a.src.endsWith('/b/stream'))!
    expect(second).toBeTruthy()
    expect(second.paused).toBe(true) // loaded, silent
    const loads = second.loads
    first.finish()
    expect(playing()).toBe('b')
    expect(second.paused).toBe(false)
    expect(second.loads).toBe(loads) // the same element, not fetched again
    // What the first element says from now on is no longer the song's.
    first.currentTime = 199
    first.emit('timeupdate')
    expect(player.progress().position).toBe(0)
  })

  it('pauses and resumes', () => {
    player.playTracks(library, 0, { shuffle: false })
    const el = sounding()!
    player.toggle()
    expect(el.paused).toBe(true)
    expect(player.get().playing).toBe(false)
    player.toggle()
    expect(el.paused).toBe(false)
    expect(player.get().playing).toBe(true)
  })

  it('shows paused when something else paused the sound (a headset, the browser)', () => {
    player.playTracks(library, 0, { shuffle: false })
    const el = sounding()!
    vi.spyOn(performance, 'now').mockReturnValue(performance.now() + 5000) // well after the song started
    el.pause()
    expect(player.get().playing).toBe(false)
  })

  it('Previous restarts a song that is under way, and goes back from its first seconds', () => {
    player.playTracks(library, 1, { shuffle: false })
    sounding()!.playTo(30, 200)
    player.previous()
    expect(playing()).toBe('b')
    expect(player.progress().position).toBe(0)
    player.previous()
    expect(playing()).toBe('a')
    player.previous() // the first song: nothing before it
    expect(playing()).toBe('a')
  })

  it('repeats one song, or the whole queue', () => {
    player.playTracks(songs('a', 'b'), 1, { shuffle: false })
    player.cycleRepeat() // all
    sounding()!.finish()
    expect(playing()).toBe('a')
    player.cycleRepeat() // one
    const el = sounding()!
    el.playTo(150, 200)
    el.finish()
    expect(playing()).toBe('a')
    expect(el.paused).toBe(false)
    expect(el.currentTime).toBe(0)
    player.next() // Next still moves on
    expect(playing()).toBe('b')
    player.cycleRepeat() // off
    expect(player.get().repeat).toBe('off')
  })

  it('seeks, within the song', () => {
    player.playTracks(library, 0, { shuffle: false })
    const el = sounding()!
    player.seek(90)
    expect(el.currentTime).toBe(90)
    expect(player.progress().position).toBe(90)
    player.seek(9999)
    expect(el.currentTime).toBe(200)
    player.seek(-5)
    expect(el.currentTime).toBe(0)
  })

  it('sets the volume on whichever element plays next too', () => {
    player.playTracks(library, 0, { shuffle: false })
    player.setVolume(0.4)
    player.toggleMute()
    for (const el of FakeAudio.made) {
      expect(el.volume).toBe(0.4)
      expect(el.muted).toBe(true)
    }
    player.setVolume(0.6) // moving the volume un-mutes
    expect(player.get().muted).toBe(false)
  })
})

describe('counting a listen', () => {
  it('counts a song once, when half of it has been heard', () => {
    player.playTracks(library, 0, { shuffle: false })
    const el = sounding()!
    el.playTo(99, 200)
    expect(counted()).toEqual([])
    el.playTo(101)
    expect(counted()).toEqual(['a'])
    el.playTo(190)
    expect(counted()).toEqual(['a'])
  })

  it('does not count time that was skipped over', () => {
    player.playTracks(library, 0, { shuffle: false })
    const el = sounding()!
    el.playTo(10, 200)
    player.seek(180)
    el.playTo(195)
    expect(counted()).toEqual([]) // 25 seconds heard of 200
  })

  it('does not count a jump the song made by itself, and does count slow reports of the time', () => {
    player.playTracks(library, 0, { shuffle: false })
    const el = sounding()!
    el.duration = 200
    // The time is reported every three seconds (a tab the browser has slowed down).
    for (const t of [3, 6, 9, 12]) {
      el.currentTime = t
      el.emit('timeupdate')
    }
    // Then it is suddenly far ahead, by nothing the player did.
    el.currentTime = 150
    el.emit('timeupdate')
    el.playTo(190)
    expect(counted()).toEqual([]) // 12 + 40 seconds heard of 200
    el.playTo(200)
    el.currentTime = 0 // (not a new play: the same one, wound back by hand)
    el.emit('timeupdate')
    el.playTo(40)
    expect(counted()).toEqual(['a']) // 12 + 50 + 40: half the song by now
  })

  it('counts a long song after four minutes', () => {
    player.playTracks([song('long', 1200)], 0, { shuffle: false })
    const el = sounding()!
    el.playTo(239, 1200)
    expect(counted()).toEqual([])
    el.playTo(241)
    expect(counted()).toEqual(['long'])
  })

  it('counts each time round when a song repeats', () => {
    player.playTracks([song('a')], 0, { shuffle: false })
    player.cycleRepeat()
    player.cycleRepeat() // one
    const el = sounding()!
    el.playTo(150, 200)
    el.finish()
    el.playTo(150)
    expect(counted()).toEqual(['a', 'a'])
  })
})

describe('songs that will not play', () => {
  it('skips one bad file and plays the next', () => {
    vi.useFakeTimers()
    player.playTracks(library, 0, { shuffle: false })
    sounding()!.fail()
    expect(playing()).toBe('b')
    expect(player.get().playing).toBe(true)
    // The note says why the song changed, while the next one plays; then it goes.
    expect(player.get().error).toContain('Song a')
    vi.advanceTimersByTime(ERROR_SHOWN_MS + 100)
    expect(player.get().error).toBeNull()
    // The next song played, so a later bad file is one bad file again, not the third in a row.
    sounding()!.fail()
    sounding()!.fail()
    expect(player.get().playing).toBe(true)
    expect(playing()).toBe('d')
  })

  it('stops after a few in a row instead of racing through the queue', () => {
    FakeAudio.broken = true
    player.playTracks(songs('a', 'b', 'c', 'd', 'e', 'f'), 0, { shuffle: false })
    for (let i = 0; i < 6 && player.get().playing; i++) {
      FakeAudio.made.find((a) => a.src.endsWith(`/${playing()}/stream`))!.fail()
    }
    expect(player.get().playing).toBe(false)
    expect(playing()).toBe('c') // tried three, stopped there
  })

  it('shows paused when the browser will not start sound by itself', async () => {
    player.playTracks(library, 0, { shuffle: false })
    player.pause()
    const el = FakeAudio.made[0]
    el.refuse = 'NotAllowedError'
    player.toggle()
    await Promise.resolve()
    await Promise.resolve()
    expect(player.get().playing).toBe(false)
    expect(player.get().waiting).toBe(false)
  })
})

describe('the queue while it plays', () => {
  it('takes songs to play next and at the end', () => {
    player.playTracks(songs('a', 'b'), 0, { shuffle: false })
    player.playNext(songs('x'))
    player.addToQueue(songs('y'))
    expect(player.get().queue.tracks.map((t) => t.id).join('')).toBe('axby')
    expect(playing()).toBe('a')
  })

  it('starts playing when the first song is added to nothing', () => {
    player.addToQueue(songs('a'))
    expect(playing()).toBe('a')
    expect(player.get().playing).toBe(true)
  })

  it('moves on when the song playing is removed, and empties when the last one is', () => {
    player.playTracks(songs('a', 'b'), 0, { shuffle: false })
    player.remove(0)
    expect(playing()).toBe('b')
    expect(player.get().playing).toBe(true)
    player.remove(0)
    expect(currentTrack()).toBeNull()
    expect(player.get().playing).toBe(false)
  })

  it('does not hand over to a song that is no longer next', () => {
    player.playTracks(songs('a', 'b', 'c'), 0, { shuffle: false })
    const first = sounding()!
    first.playTo(185, 200) // b is loaded in the other element
    player.remove(1) // and then b leaves the queue
    first.finish()
    expect(playing()).toBe('c')
    expect(sounding()?.src).toBe('/v1/music/tracks/c/stream')
  })

  it('clears', () => {
    player.playTracks(library, 0, { shuffle: false })
    player.clear()
    expect(currentTrack()).toBeNull()
    expect(FakeAudio.made.every((a) => a.paused && a.src === '')).toBe(true)
  })
})

describe('across a reload', () => {
  it('remembers the queue, the place in it and the settings; comes back paused', async () => {
    vi.useFakeTimers()
    player.playTracks(library, 2, { shuffle: false })
    sounding()!.playTo(42, 200)
    player.cycleRepeat()
    player.setVolume(0.5)
    vi.advanceTimersByTime(1000) // the save waits a moment
    vi.useRealTimers()

    resetPlayerForTests() // a new page
    FakeAudio.made = []
    await restorePlayer()
    expect(playing()).toBe('c')
    expect(player.get().queue.tracks.map((t) => t.id).join('')).toBe('abcd')
    expect(player.get().playing).toBe(false)
    expect(player.get().repeat).toBe('all')
    expect(player.get().volume).toBe(0.5)
    expect(player.progress().position).toBe(42)
    expect(FakeAudio.made.length).toBe(0) // nothing is fetched until Play is pressed
    // Play: from where it was.
    player.toggle()
    const el = FakeAudio.made.find((a) => a.src.endsWith('/c/stream'))!
    expect(el.currentTime).toBe(42)
    expect(el.paused).toBe(false)
  })

  it('leaves out songs that are gone from the library', async () => {
    vi.useFakeTimers()
    player.playTracks(library, 1, { shuffle: false })
    vi.advanceTimersByTime(1000)
    vi.useRealTimers()
    resetPlayerForTests()
    library = songs('a', 'c', 'd') // b was removed meanwhile
    await restorePlayer()
    expect(player.get().queue.tracks.map((t) => t.id).join('')).toBe('acd')
    expect(playing()).toBe('a') // its song is gone: the first, from the start
    expect(player.progress().position).toBe(0)
  })

  it("does not replace what was started while the saved queue was being read", async () => {
    localStorage.setItem('roxy.music.p', JSON.stringify({ ids: ['a', 'b'], index: 0, position: 10 }))
    const restoring = restorePlayer()
    player.playTracks(songs('z'), 0, { shuffle: false })
    await restoring
    expect(playing()).toBe('z')
  })

  it('survives a saved queue that is not what it should be', async () => {
    for (const junk of ['not json', '{"ids":"nope"}', '{"ids":[1,2],"index":"x","volume":7,"repeat":"sideways"}', 'null']) {
      resetPlayerForTests()
      localStorage.setItem('roxy.music.p', junk)
      await restorePlayer()
      expect(currentTrack()).toBeNull()
      expect(player.get().volume).toBe(1)
      expect(player.get().repeat).toBe('off')
    }
  })

  it("stops and forgets the queue when another profile takes over", () => {
    player.playTracks(library, 0, { shuffle: false })
    const el = sounding()!
    session.set({ ...S, profileId: 'someone-else' })
    expect(currentTrack()).toBeNull()
    expect(player.get().playing).toBe(false)
    expect(el.paused).toBe(true)
  })
})
