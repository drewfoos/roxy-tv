// The music player: one for the whole app, alive across pages. It owns the queue and
// says what plays; the sound itself is the engine's (engine.ts). The bar and the pages
// read it and tell it what to do.
import { music, type MusicTrack } from '../api/music'
import { session } from '../api/session'
import { connection } from '../api/connection'
import { Engine, type EngineEvent, type EngineTrack } from './engine'
import { musicPrefs } from './prefs'
import { append, emptyQueue, heardEnough, insertNext, moveItem, nextIndex, previousIndex, removeAt, replaceTrack, setShuffle, startQueue, type Queue, type Repeat } from './queue'
import { chooseStream, inAlbumRun, levelGain, runsOn, type Stream } from './stream'

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
  /** How the song playing is sent: the file itself, or converted. */
  stream: Stream | null
}

export interface Progress {
  /** Seconds into the song. */
  position: number
  /** The song's length in seconds (the file's own once known, else the library's). */
  duration: number
}

/** How long before a song's end the next one starts loading (at once when the join must be exact). */
export const PRELOAD_SECONDS = 20
/** Previous restarts the song when it is further in than this; before it, the song before. */
export const RESTART_AFTER_SECONDS = 3
/** Songs in a row that would not play before the player stops trying the next. */
const MAX_FAILURES = 3
/** The most songs remembered across a reload. */
const SAVED_MAX = 500
/** While a song plays, its place is saved this often (seconds of the song). */
const SAVE_EVERY_SECONDS = 5
/** How long the note about a song that would not play stays up. */
export const ERROR_SHOWN_MS = 6000

const fresh = (): PlayerState => ({ queue: emptyQueue, playing: false, waiting: false, shuffle: false, repeat: 'off', volume: 1, muted: false, error: null, stream: null })
let state: PlayerState = fresh()
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

// ---- the engine ----

let engine = makeEngine()
/** Whether this play of the song was counted, and how many songs in a row would not play. */
let counted = false
let failures = 0
/** The queue place and song made ready to follow the one playing (null: nothing is). */
let prepared: { index: number; id: string; url: string } | null = null
let savedAtPosition = 0
let errorTimer: ReturnType<typeof setTimeout> | undefined

function makeEngine(): Engine {
  const e = new Engine()
  e.on(onEngine)
  return e
}

/** A song as the engine is told to play it: its stream, its level, and how it must join. */
function engineTrack(index: number): { track: EngineTrack; stream: Stream } | null {
  const t = state.queue.tracks[index]
  if (!t) return null
  const prefs = musicPrefs.get()
  const stream = chooseStream(t, prefs.quality)
  return {
    stream,
    track: {
      id: t.id,
      url: stream.url,
      duration: t.durationMs / 1000,
      gainDb: levelGain(t, prefs.levelling, inAlbumRun(state.queue, index)),
      exact: prefs.gapless && runsOn(state.queue, index),
    },
  }
}

function onEngine(e: EngineEvent) {
  switch (e.type) {
    case 'time':
      onTime()
      break
    case 'playing':
      failures = 0
      if (state.waiting) set({ waiting: false })
      mediaState()
      break
    case 'waiting':
      if (state.playing) set({ waiting: true })
      break
    case 'paused':
    case 'blocked':
      set({ playing: false, waiting: false })
      mediaState()
      saveNow()
      break
    case 'resumed':
      set({ playing: true })
      mediaState()
      break
    case 'advanced':
      onAdvanced()
      break
    case 'ended': {
      // Nothing had been made ready to follow (a song shorter than the time it takes, or
      // the queue changed at the last moment): what follows is played now.
      const at = nextIndex(state.queue, state.repeat, true)
      if (at != null) {
        load(at, true)
        break
      }
      // The end of the queue: stop on the last song, ready to play it again.
      set({ playing: false, waiting: false })
      engine.seek(0)
      setProgress({ position: 0, duration: progress.duration })
      mediaState()
      saveNow()
      break
    }
    case 'error':
      onError()
      break
  }
}

function onTime() {
  const position = engine.position()
  const duration = engine.duration()
  setProgress({ position, duration })
  const track = currentTrack()
  if (track && !counted && engine.heard() >= heardEnough(duration)) {
    counted = true
    reportPlay(track.id)
  }
  prepareNext()
  if (Math.abs(position - savedAtPosition) >= SAVE_EVERY_SECONDS) saveNow()
}

/**
 * Tells the engine what follows the song playing, once it is time to: at once when the
 * join has to be exact (the song is decoded for it, which takes a moment), else a little
 * before the end. Asked again after anything that can change what follows.
 */
function prepareNext() {
  const at = nextIndex(state.queue, state.repeat, true)
  const next = at == null ? null : engineTrack(at)
  if (at == null || !next) {
    if (prepared) {
      prepared = null
      engine.setNext(null)
    }
    return
  }
  const left = progress.duration - progress.position
  const due = next.track.exact || (progress.duration > 0 && left <= PRELOAD_SECONDS)
  if (!due) {
    // What was made ready is no longer what follows (the queue changed).
    if (prepared && (prepared.index !== at || prepared.url !== next.track.url)) {
      prepared = null
      engine.setNext(null)
    }
    return
  }
  prepared = { index: at, id: next.track.id, url: next.track.url }
  engine.setNext(next.track)
}

/** The engine went on to the song that was made ready. */
function onAdvanced() {
  const to = prepared
  prepared = null
  const index = to && state.queue.tracks[to.index]?.id === to.id ? to.index : nextIndex(state.queue, state.repeat, true)
  if (index == null) return
  counted = false
  const made = engineTrack(index)
  set({ queue: { ...state.queue, index }, playing: true, waiting: false, stream: made?.stream ?? null })
  setProgress({ position: engine.position(), duration: engine.duration() })
  const track = currentTrack()
  if (track) mediaTrack(track)
  saveNow()
  prepareNext()
}

// The note stays while the next song starts (it says why the song changed) and goes by
// itself after a moment.
function showError(message: string) {
  set({ error: message })
  clearTimeout(errorTimer)
  errorTimer = setTimeout(() => {
    if (state.error === message) set({ error: null })
  }, ERROR_SHOWN_MS)
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

/** Makes the song at `index` the current one, playing it when `play` is set. */
function load(index: number, play: boolean, startAt = 0) {
  const made = engineTrack(index)
  const track = state.queue.tracks[index]
  if (!made || !track) return
  counted = false
  prepared = null
  set({ queue: { ...state.queue, index }, playing: play, waiting: play, error: failures > 0 ? state.error : null, stream: made.stream })
  setProgress({ position: startAt, duration: track.durationMs / 1000 })
  engine.setVolume(state.volume, state.muted)
  engine.load(made.track, play, startAt)
  mediaTrack(track)
  saveNow()
  prepareNext()
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
  /** What the visualizer reads (null until something has played, or without Web Audio). */
  analyser: () => engine.analyser(),
  /** The rate sound leaves the browser at, once known. */
  outputRate: () => engine.outputRate(),
  /** The decoded songs held in memory now, and what they take. */
  memory: () => engine.memory(),
  /** How songs have followed each other since the page opened. */
  joins: () => ({ ...engine.stats, voice: engine.voice() }),

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
    else queueChanged()
  },
  addToQueue(tracks: MusicTrack[]) {
    const had = currentTrack()
    set({ queue: append(state.queue, tracks) })
    if (!had) load(0, true)
    else queueChanged()
  },
  toggle() {
    const track = currentTrack()
    if (!track) return
    if (state.playing) {
      player.pause()
      return
    }
    // A queue brought back after a reload has nothing loaded yet.
    if (engine.voice() == null) {
      load(state.queue.index, true, progress.position)
      return
    }
    set({ playing: true, waiting: true })
    engine.play()
    mediaState()
  },
  pause() {
    if (!state.playing) return
    set({ playing: false, waiting: false })
    engine.pause()
    mediaState()
    saveNow()
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
    if (!currentTrack()) return
    const to = Math.max(0, Math.min(seconds, progress.duration || seconds))
    engine.seek(to)
    setProgress({ position: to, duration: progress.duration })
    mediaPosition()
    prepareNext()
  },
  /** Jumps to a song of the queue. */
  jump(index: number) {
    if (index >= 0 && index < state.queue.tracks.length) load(index, true)
  },
  /** Moves a song of the queue from one place to another. */
  move(from: number, to: number) {
    const queue = moveItem(state.queue, from, to)
    if (queue === state.queue) return
    set({ queue })
    queueChanged()
  },
  remove(index: number) {
    const { queue, changed } = removeAt(state.queue, index)
    if (!changed) {
      set({ queue })
      queueChanged()
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
    engine.clear()
    prepared = null
    set({ queue: emptyQueue, playing: false, waiting: false, error: null, stream: null })
    setProgress({ position: 0, duration: 0 })
    mediaTrack(null)
    saveNow()
  },
  setShuffle(on: boolean) {
    set({ shuffle: on, queue: setShuffle(state.queue, on) })
    queueChanged()
  },
  cycleRepeat() {
    const order: Repeat[] = ['off', 'all', 'one']
    set({ repeat: order[(order.indexOf(state.repeat) + 1) % order.length] })
    queueChanged()
  },
  setVolume(v: number) {
    const volume = Math.max(0, Math.min(1, v))
    set({ volume, muted: volume === 0 ? state.muted : false })
    engine.setVolume(state.volume, state.muted)
    save()
  },
  toggleMute() {
    set({ muted: !state.muted })
    engine.setVolume(state.volume, state.muted)
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

/** The queue, shuffle or repeat changed: what follows the song playing may have. */
function queueChanged() {
  // Its place among its album's songs decides how a song is levelled and joined.
  const made = engineTrack(state.queue.index)
  if (made && engine.voice() != null) engine.setGain(made.track.gainDb)
  prepareNext()
  save()
}

// A setting changed: levelling applies at once; another quality reloads the song where
// it is (it is another stream).
musicPrefs.subscribe(() => {
  const made = engineTrack(state.queue.index)
  if (!made || engine.voice() == null) return
  if (made.stream.url !== state.stream?.url) {
    load(state.queue.index, state.playing, progress.position)
    return
  }
  engine.setGain(made.track.gainDb)
  prepared = null
  engine.setNext(null)
  prepareNext()
})

// ---- listens ----

interface PendingPlay {
  id: string
  at: number
}
const playsKey = (profile: string) => 'roxy.music.plays.' + profile

function pendingPlays(profile: string): PendingPlay[] {
  try {
    const v = JSON.parse(localStorage.getItem(playsKey(profile)) ?? '[]') as unknown
    return Array.isArray(v) ? v.filter((p): p is PendingPlay => !!p && typeof p.id === 'string' && typeof p.at === 'number').slice(-200) : []
  } catch {
    return []
  }
}

function storePlays(profile: string, list: PendingPlay[]) {
  try {
    if (list.length) localStorage.setItem(playsKey(profile), JSON.stringify(list))
    else localStorage.removeItem(playsKey(profile))
  } catch {
    /* storage blocked: a listen whose report fails is then lost */
  }
}

/**
 * A listen was counted. It is written down before it is sent, and kept until the server
 * has answered for it: a report that got no answer goes again later, and since the time
 * names the listen, going twice counts once.
 */
function reportPlay(id: string) {
  const profile = session.get()?.profileId
  if (!profile) return
  storePlays(profile, [...pendingPlays(profile), { id, at: Date.now() }])
  void sendPlays()
}

let sending = false
/** Sends the listens not yet answered for. */
export async function sendPlays(): Promise<void> {
  const profile = session.get()?.profileId
  if (!profile || sending) return
  sending = true
  try {
    // Until none are left: a listen counted while these were being sent is sent too.
    for (let list = pendingPlays(profile); list.length; list = pendingPlays(profile)) {
      const play = list[0]
      let answered = true
      try {
        await music.played(play.id, play.at)
      } catch (e) {
        // The server said no (the song is gone, the report is malformed): not sent again.
        // No answer, or the server in trouble: kept, and tried again later.
        const status = (e as { status?: number } | null)?.status ?? 0
        answered = status >= 400 && status < 500
      }
      if (session.get()?.profileId !== profile) return
      if (!answered) return
      storePlays(profile, pendingPlays(profile).filter((p) => !(p.id === play.id && p.at === play.at)))
    }
  } finally {
    sending = false
  }
}
connection.onBack(() => void sendPlays())

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
  /** The order from before shuffle was turned on (absent while it is off). */
  unshuffled?: string[]
}

const keyFor = (profile: string | null | undefined) => (profile ? 'roxy.music.' + profile : null)
const storageKey = () => keyFor(session.get()?.profileId)

let saveTimer: ReturnType<typeof setTimeout> | undefined
function save() {
  clearTimeout(saveTimer)
  saveTimer = setTimeout(() => saveNow(), 400)
}

/** Writes the queue and the place in it down, for `key` (the profile it belongs to). */
function saveNow(key: string | null = storageKey()) {
  clearTimeout(saveTimer)
  if (!key) return
  // Around the song playing when the queue is longer than what is kept.
  const { tracks, index, unshuffled } = state.queue
  const from = Math.max(0, Math.min(index - 50, tracks.length - SAVED_MAX))
  const kept = tracks.slice(from, from + SAVED_MAX)
  // Where the song is now, not where it was when the time was last shown.
  const position = engine.voice() != null ? engine.position() : progress.position
  savedAtPosition = position
  const saved: Saved = {
    ids: kept.map((t) => t.id),
    index: index < 0 ? -1 : index - from,
    position: Math.floor(position),
    shuffle: state.shuffle,
    repeat: state.repeat,
    volume: state.volume,
    muted: state.muted,
  }
  // The order to go back to when shuffle is turned off: kept whole or not at all (a part
  // of it would put the songs back in an order that never was).
  if (unshuffled && unshuffled.length === tracks.length && tracks.length <= SAVED_MAX) saved.unshuffled = unshuffled.map((t) => t.id)
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
    const strings = (list: unknown) => (Array.isArray(list) ? list.filter((x): x is string => typeof x === 'string').slice(0, SAVED_MAX) : [])
    const repeat: Repeat = v.repeat === 'all' || v.repeat === 'one' ? v.repeat : 'off'
    const unshuffled = strings(v.unshuffled)
    return {
      ids: strings(v.ids),
      index: typeof v.index === 'number' ? v.index : -1,
      position: typeof v.position === 'number' && v.position > 0 ? v.position : 0,
      shuffle: v.shuffle === true,
      repeat,
      volume: typeof v.volume === 'number' && v.volume >= 0 && v.volume <= 1 ? v.volume : 1,
      muted: v.muted === true,
      unshuffled: unshuffled.length ? unshuffled : undefined,
    }
  } catch {
    return null
  }
}

let restoredFor: string | null = null
/**
 * Brings back the queue this profile had when the page was last open: paused, at the
 * place it was, with the order shuffle will go back to. Songs that are gone from the
 * library are left out. Once per profile.
 */
export async function restorePlayer(): Promise<void> {
  const key = storageKey()
  if (!key || restoredFor === key) return
  restoredFor = key
  void sendPlays()
  const saved = readSaved()
  if (!saved) return
  set({ shuffle: saved.shuffle, repeat: saved.repeat, volume: saved.volume, muted: saved.muted })
  engine.setVolume(saved.volume, saved.muted)
  if (!saved.ids.length || currentTrack()) return
  let found: MusicTrack[]
  try {
    found = (await music.tracks({ ids: [...new Set(saved.ids)].join(','), limit: SAVED_MAX })).tracks
  } catch {
    restoredFor = null // asked again at the next chance
    return
  }
  // The queue as it was saved: in its order (whatever order the answer came in), a song
  // that was in it twice in it twice, a song that is gone left out.
  const byId = new Map(found.map((t) => [t.id, t]))
  const inOrder = (ids: string[]) => ids.flatMap((id) => (byId.has(id) ? [byId.get(id)!] : []))
  const tracks = inOrder(saved.ids)
  // Something was started while the saved queue was being read: that wins.
  if (currentTrack() || storageKey() !== key || !tracks.length) return
  const wanted = saved.ids[saved.index]
  // The place it was at: the same entry when its song is still there, else the start.
  const before = saved.ids.slice(0, Math.max(0, saved.index)).filter((id) => byId.has(id)).length
  const same = wanted != null && byId.has(wanted)
  const at = same ? before : 0
  // The order from before shuffle: the same songs (the ones that are still there).
  let unshuffled: MusicTrack[] | null = null
  if (saved.shuffle && saved.unshuffled) {
    const order = inOrder(saved.unshuffled)
    if (order.length === tracks.length) unshuffled = order
  }
  set({ queue: { tracks, index: at, unshuffled }, stream: null })
  setProgress({ position: same ? saved.position : 0, duration: tracks[at].durationMs / 1000 })
  savedAtPosition = progress.position
  mediaTrack(tracks[at])
}

// Another profile, or signed out: the music stops, and the queue is written down for the
// profile it belongs to (with the place in the song) before it is taken away.
let owner = session.get()?.profileId ?? null
session.subscribe(() => {
  const now = session.get()?.profileId ?? null
  if (now === owner) return
  if (currentTrack()) saveNow(keyFor(owner))
  owner = now
  clearTimeout(saveTimer)
  engine.clear()
  prepared = null
  restoredFor = null
  state = { ...state, queue: emptyQueue, playing: false, waiting: false, error: null, stream: null }
  stateListeners.forEach((l) => l())
  setProgress({ position: 0, duration: 0 })
  mediaTrack(null)
})

if (typeof window !== 'undefined') window.addEventListener('pagehide', () => saveNow())

/** For tests: the player as a page that was just opened has it. */
export function resetPlayerForTests() {
  clearTimeout(saveTimer)
  clearTimeout(errorTimer)
  engine = makeEngine()
  counted = false
  failures = 0
  prepared = null
  savedAtPosition = 0
  restoredFor = null
  sending = false
  owner = session.get()?.profileId ?? null
  state = fresh()
  progress = { position: 0, duration: 0 }
}
