// The music player: one for the whole app, alive across pages. It owns the audio
// elements and the queue; the bar and the pages read it and tell it what to do.
//
// Two <audio> elements take turns. While one plays, the other loads the next song a few
// seconds before the end, so the change of song is a hand-over between two elements that
// are both ready, not a wait for a new file to start arriving.
import { music, type MusicTrack } from '../api/music'
import { session } from '../api/session'
import { append, emptyQueue, heardEnough, insertNext, nextIndex, previousIndex, removeAt, replaceTrack, setShuffle, startQueue, type Queue, type Repeat } from './queue'

export interface PlayerState {
  queue: Queue
  /** What the listener asked for: playing or paused. */
  playing: boolean
  /** Playing was asked for and the sound has not started (or stopped to load more). */
  waiting: boolean
  shuffle: boolean
  repeat: Repeat
  /** 0 to 1. */
  volume: number
  muted: boolean
  /** The song that could not be played, in words; null when all is well. */
  error: string | null
}

export interface Progress {
  /** Seconds into the song. */
  position: number
  /** The song's length in seconds (the file's own once known, else the library's). */
  duration: number
}

/** How long before a song's end the next one starts loading. */
export const PRELOAD_SECONDS = 20
/** Previous restarts the song when it is further in than this; before it, the song before. */
export const RESTART_AFTER_SECONDS = 3
/** The longest stretch between two time reports that is taken as playing, not as a jump (seconds). */
const MAX_HEARD_STEP = 5
/** Songs in a row that would not play before the player stops trying the next. */
const MAX_FAILURES = 3
/** The most songs remembered across a reload. */
const SAVED_MAX = 500

let state: PlayerState = { queue: emptyQueue, playing: false, waiting: false, shuffle: false, repeat: 'off', volume: 1, muted: false, error: null }
let progress: Progress = { position: 0, duration: 0 }
const stateListeners = new Set<() => void>()
const progressListeners = new Set<() => void>()

function set(patch: Partial<PlayerState>) {
  state = { ...state, ...patch }
  stateListeners.forEach((l) => l())
}

function setProgress(next: Progress) {
  if (next.position === progress.position && next.duration === progress.duration) return
  progress = next
  progressListeners.forEach((l) => l())
}

export function currentTrack(s: PlayerState = state): MusicTrack | null {
  return s.queue.index >= 0 ? (s.queue.tracks[s.queue.index] ?? null) : null
}

// ---- the audio elements ----

let active: HTMLAudioElement | null = null
let standby: HTMLAudioElement | null = null
/** The song the standby element has loaded (its id), if any. */
let standbyFor: string | null = null
/** Seconds of the current song actually heard, and whether its listen was counted. */
let heard = 0
let counted = false
let lastTime = 0
let failures = 0

function makeAudio(): HTMLAudioElement {
  const a = new Audio()
  a.preload = 'auto'
  a.addEventListener('timeupdate', () => a === active && onTime(a))
  a.addEventListener('durationchange', () => a === active && onTime(a))
  a.addEventListener('playing', () => {
    if (a !== active) return
    failures = 0
    set({ waiting: false })
    mediaState()
  })
  a.addEventListener('waiting', () => a === active && state.playing && set({ waiting: true }))
  a.addEventListener('ended', () => a === active && onEnded())
  a.addEventListener('error', () => a === active && onError())
  // Paused by something other than the app (the keyboard's media key before Media Session
  // took it, a headset, the browser): the bar must not go on showing "playing".
  a.addEventListener('pause', () => {
    if (a === active && state.playing && !a.ended && !changingSong()) set({ playing: false, waiting: false })
    mediaState()
  })
  a.addEventListener('play', () => {
    if (a === active && !state.playing) set({ playing: true })
  })
  return a
}

/**
 * When the player last changed songs by itself. A browser may report the old song as
 * paused a moment after its element was given another file: that is the player's doing,
 * not the listener's.
 */
let changedAt = -Infinity
const changingSong = () => performance.now() - changedAt < 300

function elements(): { a: HTMLAudioElement; b: HTMLAudioElement } {
  if (!active) active = makeAudio()
  if (!standby) standby = makeAudio()
  applyVolume()
  return { a: active, b: standby }
}

function applyVolume() {
  for (const el of [active, standby]) {
    if (!el) continue
    el.volume = state.volume
    el.muted = state.muted
  }
}

function onTime(a: HTMLAudioElement) {
  const t = a.currentTime
  // Only time that passed by playing counts as heard: a jump is not listening. (A seek
  // made here moves lastTime with it; this is for a jump made by anything else. The
  // browser reports the time a few times a second, and seconds apart in a tab it has
  // slowed down, which still counts.)
  const step = t - lastTime
  if (step > 0 && step < MAX_HEARD_STEP && !a.paused) heard += step
  lastTime = t
  const track = currentTrack()
  const duration = Number.isFinite(a.duration) && a.duration > 0 ? a.duration : (track?.durationMs ?? 0) / 1000
  setProgress({ position: t, duration })
  if (track && !counted && heard >= heardEnough(duration)) {
    counted = true
    music.played(track.id).catch(() => undefined)
  }
  if (duration > 0 && duration - t <= PRELOAD_SECONDS) preloadNext()
}

/** Gets the song after this one ready in the other element. */
function preloadNext() {
  const at = nextIndex(state.queue, state.repeat, true)
  if (at == null || at === state.queue.index) return
  const next = state.queue.tracks[at]
  if (!next || standbyFor === next.id || !standby) return
  standbyFor = next.id
  standby.src = music.streamUrl(next.id)
  standby.load()
}

function onEnded() {
  const at = nextIndex(state.queue, state.repeat, true)
  if (at == null) {
    // The end of the queue: stop on the last song, ready to play it again.
    set({ playing: false, waiting: false })
    if (active) active.currentTime = 0
    setProgress({ position: 0, duration: progress.duration })
    mediaState()
    return
  }
  load(at, true)
}

function onError() {
  const track = currentTrack()
  failures++
  set({ waiting: false })
  showError(track ? `"${track.title}" could not be played.` : 'That song could not be played.')
  const at = nextIndex(state.queue, state.repeat === 'one' ? 'off' : state.repeat, false)
  // One bad file is skipped; a run of them (the server gone, a format the browser has no
  // decoder for) stops the player instead of racing through the whole queue.
  if (state.playing && at != null && at !== state.queue.index && failures < MAX_FAILURES) load(at, true)
  else set({ playing: false })
}

/** How long the note about a song that would not play stays up. */
export const ERROR_SHOWN_MS = 6000
let errorTimer: ReturnType<typeof setTimeout> | undefined

// The note stays while the next song starts (it says why the song changed) and goes by
// itself after a moment.
function showError(message: string) {
  set({ error: message })
  clearTimeout(errorTimer)
  errorTimer = setTimeout(() => {
    if (state.error === message) set({ error: null })
  }, ERROR_SHOWN_MS)
}

/** Makes the song at `index` the current one, playing it when `play` is set. */
function load(index: number, play: boolean, startAt = 0) {
  const track = state.queue.tracks[index]
  if (!track) return
  const { a, b } = elements()
  changedAt = performance.now()
  heard = 0
  counted = false
  lastTime = startAt
  let el = a
  if (index === state.queue.index && a.src && a.src.endsWith(music.streamUrl(track.id)) && !a.error) {
    // The same song again (repeat one, or the only song of a queue that repeats): it is
    // already loaded.
  } else if (standbyFor === track.id && b.src) {
    // The other element already has this song: it becomes the one that plays.
    a.pause()
    active = b
    standby = a
    el = b
  } else {
    a.src = music.streamUrl(track.id)
    a.load()
  }
  standbyFor = null
  if (startAt > 0) {
    try {
      el.currentTime = startAt
    } catch {
      /* not seekable yet: it starts from the beginning */
    }
  } else if (el.currentTime > 0) el.currentTime = 0
  set({ queue: { ...state.queue, index }, playing: play, waiting: play, error: failures > 0 ? state.error : null })
  setProgress({ position: startAt, duration: track.durationMs / 1000 })
  mediaTrack(track)
  if (play) start(el)
  else el.pause()
  save()
}

function start(el: HTMLAudioElement) {
  const p = el.play()
  if (p && typeof p.catch === 'function')
    p.catch((e: unknown) => {
      // The browser refuses sound a person has not asked for (a restored queue on a fresh
      // page): the player shows paused and the first press plays.
      if (el !== active) return
      const name = (e as { name?: string } | null)?.name
      if (name === 'NotAllowedError') set({ playing: false, waiting: false })
    })
}

// ---- what the bar and the pages ask for ----

export interface PlayOptions {
  /** Shuffle the list (the chosen song still plays first). Left out: as the player is set. */
  shuffle?: boolean
}

export const player = {
  get: () => state,
  subscribe(l: () => void) {
    stateListeners.add(l)
    return () => {
      stateListeners.delete(l)
    }
  },
  progress: () => progress,
  subscribeProgress(l: () => void) {
    progressListeners.add(l)
    return () => {
      progressListeners.delete(l)
    }
  },

  /** Plays `tracks` from the one at `start`, replacing the queue. */
  playTracks(tracks: MusicTrack[], start = 0, options: PlayOptions = {}) {
    if (!tracks.length) return
    const shuffle = options.shuffle ?? state.shuffle
    failures = 0
    const queue = startQueue(tracks, start, shuffle)
    state = { ...state, queue, shuffle, error: null }
    load(queue.index, true)
  },
  playNext(tracks: MusicTrack[]) {
    const had = currentTrack()
    set({ queue: insertNext(state.queue, tracks) })
    if (!had) load(0, true)
    else save()
  },
  addToQueue(tracks: MusicTrack[]) {
    const had = currentTrack()
    set({ queue: append(state.queue, tracks) })
    if (!had) load(0, true)
    else save()
  },
  toggle() {
    const track = currentTrack()
    if (!track) return
    const { a } = elements()
    if (state.playing) {
      set({ playing: false, waiting: false })
      a.pause()
    } else {
      // A restored queue has no file loaded yet.
      if (!a.src) {
        load(state.queue.index, true, progress.position)
        return
      }
      set({ playing: true, waiting: a.readyState < 3 })
      start(a)
    }
    mediaState()
  },
  pause() {
    if (!state.playing) return
    set({ playing: false, waiting: false })
    active?.pause()
    mediaState()
  },
  next() {
    const at = nextIndex(state.queue, state.repeat, false)
    if (at != null) load(at, state.playing)
  },
  /** The song before; or, further into a song than a few seconds, the start of this one. */
  previous() {
    if (progress.position > RESTART_AFTER_SECONDS) {
      player.seek(0)
      return
    }
    const at = previousIndex(state.queue, state.repeat)
    if (at != null) load(at, state.playing)
    else player.seek(0)
  },
  seek(seconds: number) {
    const track = currentTrack()
    if (!track) return
    const to = Math.max(0, Math.min(seconds, progress.duration || seconds))
    lastTime = to
    if (active?.src) {
      try {
        active.currentTime = to
      } catch {
        /* nothing loaded to seek in */
      }
    }
    setProgress({ position: to, duration: progress.duration })
    mediaPosition()
  },
  /** Jumps to a song of the queue. */
  jump(index: number) {
    if (index >= 0 && index < state.queue.tracks.length) load(index, true)
  },
  remove(index: number) {
    const { queue, changed } = removeAt(state.queue, index)
    if (!changed) {
      set({ queue })
      save()
      return
    }
    if (queue.index < 0) {
      player.clear()
      return
    }
    state = { ...state, queue }
    load(queue.index, state.playing)
  },
  clear() {
    for (const el of [active, standby]) {
      if (!el) continue
      el.pause()
      el.removeAttribute('src')
      el.load()
    }
    standbyFor = null
    set({ queue: emptyQueue, playing: false, waiting: false, error: null })
    setProgress({ position: 0, duration: 0 })
    mediaTrack(null)
    save()
  },
  setShuffle(on: boolean) {
    set({ shuffle: on, queue: setShuffle(state.queue, on) })
    save()
  },
  cycleRepeat() {
    const order: Repeat[] = ['off', 'all', 'one']
    set({ repeat: order[(order.indexOf(state.repeat) + 1) % order.length] })
    save()
  },
  setVolume(v: number) {
    const volume = Math.max(0, Math.min(1, v))
    set({ volume, muted: volume === 0 ? state.muted : false })
    applyVolume()
    save()
  },
  toggleMute() {
    set({ muted: !state.muted })
    applyVolume()
    save()
  },
  /** A song's favourite mark changed (here or on a page): the queue shows it too. */
  updateTrack(track: MusicTrack) {
    const queue = replaceTrack(state.queue, track)
    if (queue !== state.queue) set({ queue })
  },
  dismissError() {
    if (state.error) set({ error: null })
  },
}

// ---- the keyboard's media keys and the system's "now playing" ----

function mediaTrack(track: MusicTrack | null) {
  const ms = typeof navigator !== 'undefined' ? navigator.mediaSession : undefined
  if (!ms || typeof MediaMetadata === 'undefined') return
  if (!track) {
    ms.metadata = null
    ms.playbackState = 'none'
    return
  }
  const art = [96, 256, 512].flatMap((w) => {
    const src = music.artUrl(track.art, w)
    return src ? [{ src, sizes: `${w}x${w}` }] : []
  })
  ms.metadata = new MediaMetadata({ title: track.title, artist: track.artist, album: track.album ?? '', artwork: art })
  mediaState()
}

function mediaState() {
  const ms = typeof navigator !== 'undefined' ? navigator.mediaSession : undefined
  if (!ms) return
  ms.playbackState = currentTrack() ? (state.playing ? 'playing' : 'paused') : 'none'
  mediaPosition()
}

function mediaPosition() {
  const ms = typeof navigator !== 'undefined' ? navigator.mediaSession : undefined
  if (!ms?.setPositionState || !(progress.duration > 0)) return
  try {
    ms.setPositionState({ duration: progress.duration, position: Math.min(progress.position, progress.duration), playbackRate: 1 })
  } catch {
    /* a position past the length, for a moment while the song changes */
  }
}

let mediaKeys = false
/** Takes the media keys (play/pause, next, previous) while there is a queue. Called by the bar. */
export function claimMediaKeys() {
  const ms = typeof navigator !== 'undefined' ? navigator.mediaSession : undefined
  if (!ms || mediaKeys) return
  mediaKeys = true
  const on = (action: MediaSessionAction, handler: MediaSessionActionHandler) => {
    try {
      ms.setActionHandler(action, handler)
    } catch {
      /* an action this browser does not know */
    }
  }
  on('play', () => !state.playing && player.toggle())
  on('pause', () => player.pause())
  on('previoustrack', () => player.previous())
  on('nexttrack', () => player.next())
  on('seekto', (d) => d.seekTime != null && player.seek(d.seekTime))
  on('seekbackward', (d) => player.seek(progress.position - (d.seekOffset ?? 10)))
  on('seekforward', (d) => player.seek(progress.position + (d.seekOffset ?? 10)))
}

// ---- remembered across a reload ----

interface Saved {
  ids: string[]
  index: number
  position: number
  shuffle: boolean
  repeat: Repeat
  volume: number
  muted: boolean
}

const storageKey = () => {
  const s = session.get()
  return s?.profileId ? 'roxy.music.' + s.profileId : null
}

let saveTimer: ReturnType<typeof setTimeout> | undefined
function save() {
  clearTimeout(saveTimer)
  saveTimer = setTimeout(saveNow, 400)
}

function saveNow() {
  const key = storageKey()
  if (!key) return
  // Around the song playing when the queue is longer than what is kept.
  const { tracks, index } = state.queue
  const from = Math.max(0, Math.min(index - 50, tracks.length - SAVED_MAX))
  const kept = tracks.slice(from, from + SAVED_MAX)
  const saved: Saved = {
    ids: kept.map((t) => t.id),
    index: index < 0 ? -1 : index - from,
    position: Math.floor(progress.position),
    shuffle: state.shuffle,
    repeat: state.repeat,
    volume: state.volume,
    muted: state.muted,
  }
  try {
    localStorage.setItem(key, JSON.stringify(saved))
  } catch {
    /* storage full or blocked: the queue is simply not remembered */
  }
}

function readSaved(): Saved | null {
  const key = storageKey()
  if (!key) return null
  try {
    const v = JSON.parse(localStorage.getItem(key) ?? 'null') as Partial<Saved> | null
    if (!v || !Array.isArray(v.ids)) return null
    const repeat: Repeat = v.repeat === 'all' || v.repeat === 'one' ? v.repeat : 'off'
    return {
      ids: v.ids.filter((x): x is string => typeof x === 'string').slice(0, SAVED_MAX),
      index: typeof v.index === 'number' ? v.index : -1,
      position: typeof v.position === 'number' && v.position > 0 ? v.position : 0,
      shuffle: v.shuffle === true,
      repeat,
      volume: typeof v.volume === 'number' && v.volume >= 0 && v.volume <= 1 ? v.volume : 1,
      muted: v.muted === true,
    }
  } catch {
    return null
  }
}

let restoredFor: string | null = null
/**
 * Brings back the queue this profile had when the page was last open: paused, at the
 * place it was. Songs that are gone from the library are left out. Once per profile.
 */
export async function restorePlayer(): Promise<void> {
  const key = storageKey()
  if (!key || restoredFor === key) return
  restoredFor = key
  const saved = readSaved()
  if (!saved) return
  set({ shuffle: saved.shuffle, repeat: saved.repeat, volume: saved.volume, muted: saved.muted })
  if (!saved.ids.length || currentTrack()) return
  let tracks: MusicTrack[]
  try {
    tracks = (await music.tracks({ ids: saved.ids.join(','), limit: SAVED_MAX })).tracks
  } catch {
    restoredFor = null // asked again at the next chance
    return
  }
  // Something was started while the saved queue was being read: that wins.
  if (currentTrack() || storageKey() !== key || !tracks.length) return
  const wanted = saved.ids[saved.index]
  const at = Math.max(0, tracks.findIndex((t) => t.id === wanted))
  const same = tracks[at]?.id === wanted
  set({ queue: { tracks, index: at, unshuffled: null } })
  setProgress({ position: same ? saved.position : 0, duration: tracks[at].durationMs / 1000 })
  mediaTrack(tracks[at])
}

// Another profile, or signed out: its music stops and its queue is not the next one's.
let owner = storageKey()
session.subscribe(() => {
  const now = storageKey()
  if (now === owner) return
  owner = now
  clearTimeout(saveTimer)
  for (const el of [active, standby]) {
    if (!el) continue
    el.pause()
    el.removeAttribute('src')
    el.load()
  }
  standbyFor = null
  restoredFor = null
  state = { ...state, queue: emptyQueue, playing: false, waiting: false, error: null }
  stateListeners.forEach((l) => l())
  setProgress({ position: 0, duration: 0 })
  mediaTrack(null)
})

if (typeof window !== 'undefined') window.addEventListener('pagehide', saveNow)

/** For tests: the player as a page that was just opened has it. */
export function resetPlayerForTests() {
  clearTimeout(saveTimer)
  clearTimeout(errorTimer)
  active = standby = null
  standbyFor = null
  heard = 0
  counted = false
  lastTime = 0
  failures = 0
  restoredFor = null
  owner = storageKey()
  state = { queue: emptyQueue, playing: false, waiting: false, shuffle: false, repeat: 'off', volume: 1, muted: false, error: null }
  progress = { position: 0, duration: 0 }
}
