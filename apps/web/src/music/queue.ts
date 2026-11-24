// The play queue's arithmetic, apart from the audio element and the browser, so it can
// be tested as plain values: what plays next, what shuffle and repeat do to the order.
import type { MusicTrack } from '../api/music'

export type Repeat = 'off' | 'all' | 'one'

export interface Queue {
  /** The songs in the order they will play. */
  tracks: MusicTrack[]
  /** The one playing (or paused on); -1 when the queue is empty. */
  index: number
  /** The order before shuffle was turned on (null while it is off). */
  unshuffled: MusicTrack[] | null
}

export const emptyQueue: Queue = { tracks: [], index: -1, unshuffled: null }

/** A stand-in for Math.random in tests. */
export type Random = () => number

/** Fisher-Yates, on a copy. */
export function shuffled<T>(list: T[], random: Random = Math.random): T[] {
  const out = list.slice()
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1))
    ;[out[i], out[j]] = [out[j], out[i]]
  }
  return out
}

/**
 * A new queue: `tracks` starting at `start`. With shuffle the chosen song still plays
 * first and the rest follow in a random order.
 */
export function startQueue(tracks: MusicTrack[], start: number, shuffle: boolean, random: Random = Math.random): Queue {
  if (!tracks.length) return emptyQueue
  const at = Math.min(Math.max(start, 0), tracks.length - 1)
  if (!shuffle) return { tracks, index: at, unshuffled: null }
  const rest = tracks.filter((_, i) => i !== at)
  return { tracks: [tracks[at], ...shuffled(rest, random)], index: 0, unshuffled: tracks }
}

/**
 * Shuffle turned on or off while a queue plays. On: what is still to come is shuffled,
 * what was played stays where it was. Off: the order from before, at the same song.
 */
export function setShuffle(q: Queue, on: boolean, random: Random = Math.random): Queue {
  if (q.index < 0) return q
  if (on) {
    if (q.unshuffled) return q
    const upcoming = shuffled(q.tracks.slice(q.index + 1), random)
    return { tracks: [...q.tracks.slice(0, q.index + 1), ...upcoming], index: q.index, unshuffled: q.tracks }
  }
  if (!q.unshuffled) return q
  const current = q.tracks[q.index]
  const at = q.unshuffled.findIndex((t) => t.id === current.id)
  // The queue was edited while shuffled (songs added or removed): the order from before
  // no longer says where everything goes, so the order as it is now stays.
  if (at < 0 || q.unshuffled.length !== q.tracks.length) return { ...q, unshuffled: null }
  return { tracks: q.unshuffled, index: at, unshuffled: null }
}

/**
 * The index to play after the current song ends by itself (`auto`) or Next is pressed.
 * null: nothing follows (the end of the queue with repeat off). Repeat-one holds only
 * for a song ending by itself: pressing Next always moves on.
 */
export function nextIndex(q: Queue, repeat: Repeat, auto: boolean): number | null {
  if (q.index < 0) return null
  if (auto && repeat === 'one') return q.index
  if (q.index + 1 < q.tracks.length) return q.index + 1
  return repeat === 'off' ? null : 0
}

/** The index Previous goes to (it wraps to the end only when the queue repeats). */
export function previousIndex(q: Queue, repeat: Repeat): number | null {
  if (q.index < 0) return null
  if (q.index > 0) return q.index - 1
  return repeat === 'all' ? q.tracks.length - 1 : null
}

/** Songs put right after the one playing ("Play next"), or as the queue when there is none. */
export function insertNext(q: Queue, tracks: MusicTrack[]): Queue {
  if (!tracks.length) return q
  if (q.index < 0) return { tracks, index: 0, unshuffled: null }
  const next = [...q.tracks.slice(0, q.index + 1), ...tracks, ...q.tracks.slice(q.index + 1)]
  return { tracks: next, index: q.index, unshuffled: q.unshuffled ? [...q.unshuffled, ...tracks] : null }
}

/** Songs put at the end ("Add to queue"). */
export function append(q: Queue, tracks: MusicTrack[]): Queue {
  if (!tracks.length) return q
  if (q.index < 0) return { tracks, index: 0, unshuffled: null }
  return { tracks: [...q.tracks, ...tracks], index: q.index, unshuffled: q.unshuffled ? [...q.unshuffled, ...tracks] : null }
}

/**
 * One song taken out. Taking out the one playing moves on to the song that followed it
 * (`changed`: the player must load another song); the last one leaves an empty queue.
 */
export function removeAt(q: Queue, at: number): { queue: Queue; changed: boolean } {
  if (at < 0 || at >= q.tracks.length) return { queue: q, changed: false }
  const gone = q.tracks[at]
  const tracks = q.tracks.filter((_, i) => i !== at)
  const unshuffled = q.unshuffled ? withoutOne(q.unshuffled, gone.id) : null
  if (!tracks.length) return { queue: emptyQueue, changed: true }
  if (at < q.index) return { queue: { tracks, index: q.index - 1, unshuffled }, changed: false }
  if (at > q.index) return { queue: { tracks, index: q.index, unshuffled }, changed: false }
  return { queue: { tracks, index: Math.min(q.index, tracks.length - 1), unshuffled }, changed: true }
}

function withoutOne(list: MusicTrack[], id: string): MusicTrack[] {
  const at = list.findIndex((t) => t.id === id)
  return at < 0 ? list : list.filter((_, i) => i !== at)
}

/**
 * A song moved from one place of the queue to another (`to` is where it ends up). The
 * song playing stays the song playing, wherever it or the others go.
 */
export function moveItem(q: Queue, from: number, to: number): Queue {
  const n = q.tracks.length
  if (from < 0 || from >= n || to < 0 || to >= n || from === to) return q
  const tracks = q.tracks.slice()
  const [moved] = tracks.splice(from, 1)
  tracks.splice(to, 0, moved)
  let index = q.index
  if (from === q.index) index = to
  else if (from < q.index && to >= q.index) index = q.index - 1
  else if (from > q.index && to <= q.index) index = q.index + 1
  // An order made by hand is the order: there is no "before shuffle" to go back to.
  return { tracks, index, unshuffled: null }
}

/** A song in the queue replaced by its newer self (its favourite mark changed). */
export function replaceTrack(q: Queue, track: MusicTrack): Queue {
  const swap = (list: MusicTrack[]) => (list.some((t) => t.id === track.id) ? list.map((t) => (t.id === track.id ? track : t)) : list)
  const tracks = swap(q.tracks)
  const unshuffled = q.unshuffled ? swap(q.unshuffled) : null
  return tracks === q.tracks && unshuffled === q.unshuffled ? q : { tracks, index: q.index, unshuffled }
}

/**
 * How much of a song must be heard for the listen to count: half of it, or four minutes
 * of a long one. (Seconds; a song of unknown length never counts.)
 */
export function heardEnough(durationSeconds: number): number {
  if (!(durationSeconds > 0)) return Infinity
  return Math.min(durationSeconds / 2, 240)
}

/** The line of synced lyrics being sung at `ms` (-1 before the first). */
export function lyricIndexAt(lines: { ms: number }[], ms: number): number {
  let lo = 0
  let hi = lines.length - 1
  let at = -1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    if (lines[mid].ms <= ms) {
      at = mid
      lo = mid + 1
    } else hi = mid - 1
  }
  return at
}
