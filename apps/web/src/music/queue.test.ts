import { describe, expect, it } from 'vitest'
import type { MusicTrack } from '../api/music'
import { append, emptyQueue, heardEnough, insertNext, lyricIndexAt, nextIndex, previousIndex, removeAt, replaceTrack, setShuffle, shuffled, startQueue, type Queue } from './queue'

const song = (id: string): MusicTrack => ({ id, title: 'Song ' + id, artist: 'A', artistId: 'a', durationMs: 180000, container: 'mp3', codec: 'mp3', lossless: false, size: 1, favorite: false, playCount: 0, addedAt: '' })
const songs = (...ids: string[]) => ids.map(song)
const ids = (q: Queue) => q.tracks.map((t) => t.id).join('')
/** A "random" that always picks the first of what is left: the shuffle reverses nothing and rotates predictably. */
const first = () => 0

describe('the queue', () => {
  it('starts at the chosen song, in order', () => {
    const q = startQueue(songs('a', 'b', 'c', 'd'), 2, false)
    expect(ids(q)).toBe('abcd')
    expect(q.index).toBe(2)
    expect(q.unshuffled).toBeNull()
    expect(startQueue([], 0, false)).toBe(emptyQueue)
    expect(startQueue(songs('a', 'b'), 9, false).index).toBe(1) // past the end: the last
  })

  it('shuffled, still plays the chosen song first and every song once', () => {
    const q = startQueue(songs('a', 'b', 'c', 'd', 'e'), 2, true)
    expect(q.tracks[0].id).toBe('c')
    expect(q.index).toBe(0)
    expect(ids(q).split('').sort().join('')).toBe('abcde')
    expect(q.unshuffled?.map((t) => t.id).join('')).toBe('abcde')
  })

  it('shuffle on mid-queue leaves what was played and mixes what is to come; off puts the order back at the same song', () => {
    const q = startQueue(songs('a', 'b', 'c', 'd', 'e'), 1, false)
    const on = setShuffle(q, true, first)
    expect(ids(on).slice(0, 2)).toBe('ab')
    expect(ids(on).slice(2).split('').sort().join('')).toBe('cde')
    expect(on.index).toBe(1)
    // Moved on by two songs, then shuffle off: the old order, on the song that is playing.
    const later = { ...on, index: 3 }
    const off = setShuffle(later, false)
    expect(ids(off)).toBe('abcde')
    expect(off.tracks[off.index].id).toBe(later.tracks[3].id)
    expect(off.unshuffled).toBeNull()
    // Turning it on twice, or off when it is off, changes nothing.
    expect(setShuffle(on, true)).toBe(on)
    expect(setShuffle(q, false)).toBe(q)
  })

  it('shuffle off after the queue was edited keeps the order as it is', () => {
    const on = setShuffle(startQueue(songs('a', 'b', 'c'), 0, false), true, first)
    const edited = removeAt(append(on, songs('x')), 1).queue
    // One was added and one removed while shuffled: the two lists still fit, so the order goes back.
    expect(setShuffle(edited, false).unshuffled).toBeNull()
    const longer = { ...on, unshuffled: [...on.unshuffled!, song('zz')] }
    const off = setShuffle(longer, false)
    expect(ids(off)).toBe(ids(on)) // the lists disagree: nothing is reordered
  })

  it('knows what follows', () => {
    const q = startQueue(songs('a', 'b', 'c'), 0, false)
    expect(nextIndex(q, 'off', true)).toBe(1)
    const last = { ...q, index: 2 }
    expect(nextIndex(last, 'off', true)).toBeNull() // the end
    expect(nextIndex(last, 'all', true)).toBe(0)
    expect(nextIndex(last, 'one', true)).toBe(2) // the same again, by itself
    expect(nextIndex(last, 'one', false)).toBe(0) // Next pressed: on, around the end
    expect(nextIndex(q, 'one', false)).toBe(1)
    expect(nextIndex(emptyQueue, 'all', true)).toBeNull()
  })

  it('knows what came before', () => {
    const q = startQueue(songs('a', 'b', 'c'), 1, false)
    expect(previousIndex(q, 'off')).toBe(0)
    const firstSong = { ...q, index: 0 }
    expect(previousIndex(firstSong, 'off')).toBeNull()
    expect(previousIndex(firstSong, 'all')).toBe(2)
    expect(previousIndex(emptyQueue, 'all')).toBeNull()
  })

  it('"play next" goes right after the song playing; "add to queue" at the end', () => {
    const q = startQueue(songs('a', 'b', 'c'), 1, false)
    expect(ids(insertNext(q, songs('x', 'y')))).toBe('abxyc')
    expect(insertNext(q, songs('x')).index).toBe(1)
    expect(ids(append(q, songs('x')))).toBe('abcx')
    // Into an empty queue: they are the queue.
    expect(ids(insertNext(emptyQueue, songs('x')))).toBe('x')
    expect(append(emptyQueue, songs('x', 'y')).index).toBe(0)
    expect(insertNext(q, [])).toBe(q)
  })

  it('removes a song without losing its place', () => {
    const q = startQueue(songs('a', 'b', 'c', 'd'), 2, false)
    const before = removeAt(q, 0)
    expect(ids(before.queue)).toBe('bcd')
    expect(before.queue.index).toBe(1) // still on c
    expect(before.changed).toBe(false)
    const after = removeAt(q, 3)
    expect(after.queue.index).toBe(2)
    expect(after.changed).toBe(false)
    // The one playing: the song after it takes its place.
    const current = removeAt(q, 2)
    expect(current.queue.tracks[current.queue.index].id).toBe('d')
    expect(current.changed).toBe(true)
    // The last one playing: the one before it.
    const end = removeAt({ ...q, index: 3 }, 3)
    expect(end.queue.tracks[end.queue.index].id).toBe('c')
    // The only one: nothing is left.
    expect(removeAt(startQueue(songs('a'), 0, false), 0).queue).toBe(emptyQueue)
    expect(removeAt(q, 9).queue).toBe(q)
  })

  it('swaps in a song that changed', () => {
    const q = startQueue(songs('a', 'b'), 0, false)
    const liked = { ...song('b'), favorite: true }
    expect(replaceTrack(q, liked).tracks[1].favorite).toBe(true)
    expect(replaceTrack(q, song('zz'))).toBe(q) // not in the queue: the same queue
  })

  it('shuffles without losing or repeating anything', () => {
    const list = ['a', 'b', 'c', 'd', 'e', 'f']
    const out = shuffled(list)
    expect(out.slice().sort()).toEqual(list)
    expect(list).toEqual(['a', 'b', 'c', 'd', 'e', 'f']) // the list given is left alone
  })
})

describe('counting a listen', () => {
  it('needs half the song, or four minutes of a long one', () => {
    expect(heardEnough(200)).toBe(100)
    expect(heardEnough(600)).toBe(240)
    expect(heardEnough(0)).toBe(Infinity)
    expect(heardEnough(NaN)).toBe(Infinity)
  })
})

describe('the lyrics line being sung', () => {
  const lines = [{ ms: 1000 }, { ms: 5000 }, { ms: 5000 }, { ms: 9000 }]
  it('is the last line that has started', () => {
    expect(lyricIndexAt(lines, 0)).toBe(-1)
    expect(lyricIndexAt(lines, 1000)).toBe(0)
    expect(lyricIndexAt(lines, 4999)).toBe(0)
    expect(lyricIndexAt(lines, 5000)).toBe(2) // two lines at one time: the later of them
    expect(lyricIndexAt(lines, 99999)).toBe(3)
    expect(lyricIndexAt([], 5)).toBe(-1)
  })
})
