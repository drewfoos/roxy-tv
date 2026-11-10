import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api/client'
import { ageLabel, pickSubtitle, primarySource } from '../api/items'
import { lang3, prefs } from '../api/prefs'
import { SUBTITLES_OFF, type Item, type MediaSegment, type MediaStream, type TrickplayManifest } from '../api/types'
import { IconFullscreen, IconPause, IconPlay, IconSettings, IconSkipNext, IconSubtitles, IconVolume, IconVolumeOff, IconForward10, IconReplay10 } from '../components/Icons'
import { fmt } from './format'
import { chooseStream, MOVED_SEC, ReopenBudget, StreamClock, streamEnded, type Mode } from './stream'
import { SettingsPanel, SpeedPanel, TracksPanel, type Quality } from './PlayerPanels'
import { SeekBar } from './SeekBar'
import { createSubtitleRenderer, type SubtitleRenderer } from './subtitles'
import { usePlayerKeys } from './usePlayerKeys'

export interface PlayerProps {
  item: Item
  /** The series, for the age rating and advisories shown at the start. */
  series?: Item | null
  next?: Item | null
  onNext?: () => void
  onEnded?: () => void
}

const REPORT_EVERY_MS = 10_000
const HIDE_AFTER_MS = 3000
const SPEEDS = [0.5, 0.75, 1, 1.25, 1.5, 2]


/**
 * The audio and subtitle tracks an item starts with: the choice saved on this title, else
 * the profile's preferences (audio language; subtitles off, only with foreign audio, or
 * always). A saved SUBTITLES_OFF means the viewer turned them off on this title: they stay
 * off, and the track C would turn on is the preferred one. Worked out again for every
 * item: the Watch page keeps one Player across episodes (so fullscreen survives Autoplay
 * Next), and the next episode must not inherit the last one's tracks.
 */
function initialTracks(item: Item, source: ReturnType<typeof primarySource>, audioTracks: MediaStream[], subTracks: MediaStream[]) {
  const p = prefs.get()
  const saved = item.userData?.audioIndex
  // A saved track this player cannot use (a picture-based PGS subtitle: only text tracks
  // are listed here) counts as no choice: the profile decides, and nothing is saved back
  // (see ownTracks), so the other device keeps it.
  const savedAudio = saved != null && audioTracks.some((t) => t.index === saved) ? saved : null
  const audio = savedAudio ?? audioTracks.find((t) => lang3(t.language) === p.audioLang)?.index ?? audioTracks[0]?.index ?? null
  const rawSub = item.userData?.subtitleIndex
  const savedSub = rawSub === SUBTITLES_OFF || (rawSub != null && subTracks.some((t) => t.index === rawSub)) ? rawSub : null
  const sub = savedSub != null && savedSub !== SUBTITLES_OFF ? savedSub : (pickSubtitle(source, p.subLang)?.index ?? null)
  let subsOn = true
  if (savedSub === SUBTITLES_OFF) subsOn = false
  else if (savedSub == null) {
    if (p.subMode === 'none') subsOn = false
    else if (p.subMode === 'foreign') {
      const a = audioTracks.find((t) => t.index === (savedAudio ?? -1)) ?? audioTracks.find((t) => lang3(t.language) === p.audioLang) ?? audioTracks[0]
      subsOn = !!a && lang3(a.language) !== p.audioLang
    }
  }
  return { audio, sub, subsOn, ownAudio: savedAudio != null, ownSub: savedSub != null }
}

const SUBS_FAILED = "Subtitles couldn't be loaded."

function errorText(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

/**
 * Asks the server for a file's stream headers only (HEAD): 'ok' when it would serve it,
 * else what is wrong, in words. Sent without the app's 401 handling on purpose: an
 * expired media token is not a signed-out session.
 */
async function probeStream(url: string): Promise<'ok' | string> {
  try {
    const res = await fetch(url, { method: 'HEAD' })
    if (res.ok) return 'ok'
    if (res.status === 404) return 'This file is no longer on the server. It may have been deleted or replaced.'
    if (res.status === 401 || res.status === 403) return 'The playback link expired. Reload the page to keep watching.'
    return `The server could not stream this file (HTTP ${res.status}).`
  } catch {
    return "Couldn't reach the server. Check the connection and try again."
  }
}

const QUALITY_KBPS: Record<Exclude<Quality, 'original'>, number> = { 1080: 8000, 720: 4000, 480: 1500 }

/**
 * Roxy's player, laid out like Crunchyroll's: a seek bar across the top of the controls
 * with a round thumb and trickplay previews on hover; left: back 10 s, play / pause,
 * forward 10 s, volume, time; right: next episode, subtitles on / off, speed, settings
 * (audio and subtitle tracks), fullscreen. The controls hide while playing until the
 * mouse moves. Keyboard: space, J / L (10 s), arrows, F, M, C, N.
 *
 * Playback is the browser's own decoder from /v1/stream when the chosen audio track is
 * one it can decode; otherwise, or when another track is picked, the server remuxes
 * (video copied, that one track) and the clock restarts at zero: a transcode's zero is the
 * start point, a remux's the keyframe before it (the server says which), and the player
 * adds it back. Subtitles are jassub over either stream.
 */
export function Player({ item, series, next, onNext, onEnded }: PlayerProps) {
  const source = primarySource(item)
  const sourceId = source?.id ?? ''
  const duration = (source?.durationMs ?? item.runtimeMs ?? 0) / 1000
  const audioTracks = useMemo(() => (source?.streams ?? []).filter((s) => s.type === 'audio' && !s.isExternal), [source])
  const subTracks = useMemo(() => (source?.streams ?? []).filter((s) => s.type === 'subtitle' && s.isTextFormat), [source])

  const videoRef = useRef<HTMLVideoElement>(null)
  const boxRef = useRef<HTMLDivElement>(null)
  const subsRef = useRef<SubtitleRenderer | null>(null)
  const tokenRef = useRef<string>('')
  const modeRef = useRef<Mode>({ kind: 'direct' })
  const reopens = useRef(new ReopenBudget()) // stream reopenings in a row (see ReopenBudget)
  // The player stays mounted from one episode to the next: each title starts afresh.
  useEffect(() => reopens.current.reset(), [source?.id])
  // How far the element really played (its own clock), and its last reading: a server
  // stream that runs out makes Chrome move the playhead to the header's duration (with
  // buffered stretched to match), so only the readings before that jump say where the
  // stream got to.
  const clock = useRef(new StreamClock())
  const lastReport = useRef(0)
  // The last position read from a live <video>. On exit React clears videoRef before the
  // effects' cleanups run, so the final report (and the pause the removal fires) would
  // read 0 and wipe the saved position; they use this instead.
  const lastAbs = useRef(0)
  const seekReport = useRef<number | null>(null)
  useEffect(() => () => { if (seekReport.current) window.clearTimeout(seekReport.current) }, [])
  // When the position last moved, or the tracks were changed (performance.now): a report's
  // age. A tab left paused that reports on leaving is then older than a finish elsewhere.
  const lastMoved = useRef({ pos: -1, at: 0, tracks: '' })
  const hideTimer = useRef<number | null>(null)
  // Each load() takes a number; one that wakes from an await to find a newer number stops
  // there. A seek, a track or quality change, or the next episode can start a load while an
  // older one is still waiting (on the token, the metadata, play()), and the older one used
  // to go on and seek or flag the newer stream.
  const loadGen = useRef(0)
  // True once this item's first load has put the stream at its start position. Until then
  // nothing is reported: a timeupdate or pause before the start seek would save a position
  // the viewer never reached over the one they left.
  const started = useRef(false)
  // True from the start of an item until its first load has started playing: that load
  // plays even though nothing is playing yet. It read the `ready` state of the render it
  // was made in, which on the next episode (the Player stays mounted) was still the last
  // episode's true, so Next and Autoplay left the new one paused.
  const firstLoad = useRef(true)
  // Whether the audio / subtitle choice is this player's to save: the saved one was usable
  // here, or the viewer picked one here. Otherwise the reports leave the saved choice alone
  // (a PGS track picked on the TV is not listed here, and the web's own pick used to
  // overwrite it at the first report).
  const ownTracks = useRef({ audio: false, sub: false })
  // The last report sent (item, position, tracks): the same report again is not sent (the
  // end of an episode was reported by 'ended' and again by the change to the next item).
  const lastSent = useRef('')

  const [error, setError] = useState<string | null>(null)
  const [playing, setPlaying] = useState(false)
  const [waiting, setWaiting] = useState(false)
  const [current, setCurrent] = useState(0)
  const [buffered, setBuffered] = useState(0)
  const [volume, setVolume] = useState(() => Number(localStorage.getItem('roxy.volume') ?? 1))
  const [muted, setMuted] = useState(false)
  const [rate, setRate] = useState(1)
  const [visible, setVisible] = useState(true)
  const [menu, setMenu] = useState<'none' | 'tracks' | 'quality' | 'speed'>('none')
  // Account > Preferences sets the defaults; the gear menu overrides them for this episode.
  const [quality, setQuality] = useState<Quality>(() => {
    const q = prefs.get().quality
    return q === 1080 || q === 720 || q === 480 ? q : 'original'
  })
  const [autoplay, setAutoplay] = useState(() => prefs.get().autoplay)
  useEffect(() => {
    autoplayRef.current = autoplay
    if (prefs.get().autoplay !== autoplay) prefs.set({ autoplay })
  }, [autoplay])
  const qualityRef = useRef<Quality>(quality)
  const autoplayRef = useRef(true)
  const [skip, setSkip] = useState<MediaSegment | null>(null)
  const [advisory, setAdvisory] = useState(true)
  // A passing problem that leaves playback going, shown for a few seconds: "2 subtitle
  // fonts didn't load", or subtitles that failed to load at all.
  const [notice, setNotice] = useState<string | null>(null)
  const noticeTimer = useRef<number | null>(null)
  const showNotice = useCallback((text: string) => {
    setNotice(text)
    if (noticeTimer.current) clearTimeout(noticeTimer.current)
    noticeTimer.current = window.setTimeout(() => setNotice(null), 8000)
  }, [])
  const [trick, setTrick] = useState<TrickplayManifest | null>(null)
  // Set again for every new item in the start effect below (see initialTracks).
  const [audioIndex, setAudioIndex] = useState<number | null>(() => initialTracks(item, source, audioTracks, subTracks).audio)
  const [subIndex, setSubIndex] = useState<number | null>(() => initialTracks(item, source, audioTracks, subTracks).sub)
  const [subsOn, setSubsOn] = useState(() => initialTracks(item, source, audioTracks, subTracks).subsOn)
  // The item whose tracks the audio / subtitle state holds. The Player is reused: in the
  // commit that switches item the state is still the last item's, and the subtitle effect
  // waits for this item's. It is the only place subtitles are fetched: the start effect
  // fetched them too, in the same commit, megabytes of text and fonts beside the video.
  const [tracksOf, setTracksOf] = useState(item.id)

  const absTime = useCallback(() => {
    const v = videoRef.current
    if (!v) return lastAbs.current
    const m = modeRef.current
    // A server stream that ran out (or was given up as lost): Chrome has moved the
    // playhead to the header's duration, so the element's time is not where the viewer
    // is; the last normal reading is (StreamClock). Reported, the jumped time marked the
    // episode watched (on leaving the page, or on the change of item).
    const elem = m.kind === 'server' && !clock.current.pauseIsReal(v, true) && !v.seeking ? clock.current.playedTo : v.currentTime
    const t = (m.kind === 'server' ? m.offset : 0) + elem
    lastAbs.current = t
    return t
  }, [])

  const report = useCallback(
    (force = false, atSec?: number) => {
      const now = Date.now()
      if (!started.current) return
      if (!force && now - lastReport.current < REPORT_EVERY_MS) return
      // Never report 0 (the apps don't either): a position of 0 is "nothing watched", and
      // the server would store it over the real one.
      const pos = atSec ?? absTime()
      const positionMs = Math.round(pos * 1000)
      if (positionMs <= 0) return
      lastReport.current = now
      // Subtitles off is saved as SUBTITLES_OFF; leaving it out (as this used to) kept the
      // last track saved, so they came back on next time. Left out on purpose while the
      // choice is not this player's (ownTracks).
      const own = ownTracks.current
      const sub = own.sub ? (subsOn ? (subIndex ?? undefined) : SUBTITLES_OFF) : undefined
      const audio = own.audio ? (audioIndex ?? undefined) : undefined
      const tracks = `${audio}/${sub}`
      const sent = `${item.id}/${positionMs}/${tracks}`
      if (sent === lastSent.current) return
      lastSent.current = sent
      const mono = performance.now()
      const lm = lastMoved.current
      if (Math.abs(pos - lm.pos) > MOVED_SEC || tracks !== lm.tracks) lastMoved.current = { pos, at: mono, tracks }
      const ageMs = Math.max(0, Math.round(mono - lastMoved.current.at))
      api.progress(item.id, positionMs, audio, sub, ageMs, sourceId || undefined, duration > 0 ? Math.round(duration * 1000) : undefined).catch(() => undefined)
    },
    [item.id, absTime, audioIndex, subIndex, subsOn, sourceId, duration],
  )
  // The start effect's cleanup sends the final report when the item changes or the player
  // closes. It runs from the closure made at mount, whose report still holds the tracks the
  // item started with; reading the latest report through this ref saves the ones picked since.
  // Updated in an effect, not during render: React runs every effect cleanup before any new
  // effect, so on a change of item the cleanup still finds the last item's report here (one
  // set during render would already post the old position under the new item's id).
  const reportRef = useRef(report)
  useEffect(() => {
    reportRef.current = report
  }, [report])
  // Closing the tab or going to another site unmounts nothing, so without this the last
  // up to 10 s of watching were lost. The report is sent with keepalive, so it outlives
  // the page.
  useEffect(() => {
    const onHide = () => reportRef.current(true)
    window.addEventListener('pagehide', onHide)
    return () => window.removeEventListener('pagehide', onHide)
  }, [])

  /** Decide direct play or remux for the chosen audio track, and load the stream at a position. */
  const load = useCallback(
    async (startSec: number, audio: number | null, resume = false) => {
      const my = ++loadGen.current
      const stale = () => my !== loadGen.current
      const v = videoRef.current
      if (!v || !source) return
      if (!tokenRef.current) {
        const token = (await api.mediaToken(source.id)).token
        if (stale()) return // the item may have changed: its token must not be kept
        tokenRef.current = token
      }
      const first = audioTracks[0]
      const chosen = audioTracks.find((t) => t.index === audio) ?? first
      const q = qualityRef.current
      const kind = chooseStream(q, audioTracks, audio)
      const direct = kind === 'direct'
      const wasPlaying = !v.paused && !v.ended
      if (q !== 'original') {
        modeRef.current = { kind: 'server', audio: chosen?.index ?? null, offset: startSec }
        v.src = api.transcodeUrl(source.id, q, QUALITY_KBPS[q], chosen?.index ?? null, startSec, tokenRef.current)
        subsRef.current?.setOffset(startSec)
      } else if (direct) {
        modeRef.current = { kind: 'direct' }
        v.src = api.streamUrl(source.id, tokenRef.current)
        subsRef.current?.setOffset(0)
        if (startSec > 0) {
          // A seek set before the metadata is in is dropped by the browser: wait for it.
          await new Promise<void>((resolve) => {
            if (v.readyState >= 1) return resolve()
            const once = () => {
              v.removeEventListener('loadedmetadata', once)
              v.removeEventListener('error', once)
              v.removeEventListener('emptied', replaced)
              resolve()
            }
            // A newer load replaced the src: stop waiting (the check below then stops this
            // one). Only then: setting this load's own src fires 'emptied' too.
            const replaced = () => { if (stale()) once() }
            v.addEventListener('loadedmetadata', once)
            v.addEventListener('error', once)
            v.addEventListener('emptied', replaced)
          })
          if (stale()) return
          v.currentTime = startSec
        }
      } else {
        // The video is copied, so the remux begins at the keyframe the seek lands on (up to
        // a GOP before startSec, often 10 s): that is the stream's zero, which the server
        // tells. Taken as startSec, subtitles ran that much early after every resume, seek
        // or reopening. Asked for while the stream opens.
        const zero = startSec > 0 ? api.remuxStart(source.id, startSec).catch(() => startSec) : Promise.resolve(0)
        modeRef.current = { kind: 'server', audio: chosen?.index ?? null, offset: startSec }
        v.src = api.remuxUrl(source.id, chosen?.index ?? null, startSec, tokenRef.current)
        subsRef.current?.setOffset(startSec)
        const at = await zero
        if (stale()) return
        modeRef.current = { kind: 'server', audio: chosen?.index ?? null, offset: at }
        subsRef.current?.setOffset(at)
      }
      setCurrent(startSec)
      setBuffered(startSec)
      v.playbackRate = rate
      if (wasPlaying || resume || firstLoad.current) await v.play().catch(() => undefined)
      if (stale()) return
      firstLoad.current = false
      started.current = true
    },
    [source, audioTracks, rate],
  )

  /**
   * A load started by the viewer (a seek past the buffer, a track or quality change). It
   * can fail like the first one (the media token request, for one) and used to fail
   * silently, leaving a stopped player with no word why.
   */
  const reload = useCallback(
    (startSec: number, audio: number | null, resume = false) => {
      const my = loadGen.current + 1 // the number this load takes
      setError(null) // a new try: an earlier failure's message no longer applies
      load(startSec, audio, resume).catch((e: unknown) => {
        if (my === loadGen.current) setError(errorText(e))
      })
    },
    [load],
  )

  const seekTo = useCallback(
    (t: number) => {
      const v = videoRef.current
      if (!v) return
      t = Math.max(0, Math.min(duration || Infinity, t))
      // Where the viewer is now, even before the stream gets there: leaving right after a
      // seek out of the buffer (the element still empty) saved the position before it.
      lastAbs.current = t
      // Saved once the seeking settles (one report after a run of skips), not at the next
      // 10 s tick: a crash or a killed tab right after a skip resumed from before it.
      if (seekReport.current) window.clearTimeout(seekReport.current)
      seekReport.current = window.setTimeout(() => reportRef.current(true), 1500)
      // The viewer moved: cuts before this no longer count as "in a row".
      reopens.current.reset()
      const m = modeRef.current
      if (m.kind === 'direct') {
        v.currentTime = t
        setCurrent(t)
        return
      }
      const rel = t - m.offset
      const end = v.buffered.length ? v.buffered.end(v.buffered.length - 1) : 0
      if (rel >= 0 && rel <= end) {
        v.currentTime = rel
        setCurrent(t)
      } else {
        reload(t, m.audio)
      }
    },
    [duration, reload],
  )

  // ---- start: token, stream, subtitles, trickplay ----
  useEffect(() => {
    const v = videoRef.current
    if (!v || !source) return
    let live = true
    setError(null)
    setAdvisory(true)
    // The previous episode's seek previews: its tile size and sheet numbers must not be used
    // with this source's images while the new manifest loads (or at all, if it has none).
    setTrick(null)
    tokenRef.current = ''
    lastAbs.current = 0 // a new item: never carry the last one's position over
    lastMoved.current = { pos: -1, at: 0, tracks: '' }
    if (seekReport.current) window.clearTimeout(seekReport.current) // a seek in the last item is not this one's
    started.current = false
    firstLoad.current = true
    lastSent.current = ''
    // The first report waits a full interval after the start, like every later one.
    lastReport.current = Date.now()
    setNotice(null)
    const subs = createSubtitleRenderer(v, (names) => {
      if (!live) return
      showNotice(`${names.length === 1 ? `The subtitle font ${names[0]} didn't` : `${names.length} subtitle fonts didn't`} load, so some signs may look wrong.`)
    })
    subsRef.current = subs
    // This item's own tracks, not the previous episode's (the Player is reused).
    const tracks = initialTracks(item, source, audioTracks, subTracks)
    setAudioIndex(tracks.audio)
    setSubIndex(tracks.sub)
    setSubsOn(tracks.subsOn)
    setTracksOf(item.id)
    ownTracks.current = { audio: tracks.ownAudio, sub: tracks.ownSub }
    setSkip(null)
    // A watched episode with a position is a rewatch under way: resume it, as the apps do.
    const start = (item.userData?.positionMs ?? 0) / 1000
    load(start > 5 ? start : 0, tracks.audio).catch((e: unknown) => live && setError(errorText(e)))
    api.trickplay(source.id).then((m) => live && setTrick(m)).catch(() => undefined)
    const t = setTimeout(() => live && setAdvisory(false), 7000)
    return () => {
      live = false
      loadGen.current++ // a load still in flight for this item stops at its next await
      clearTimeout(t)
      if (noticeTimer.current) clearTimeout(noticeTimer.current)
      reportRef.current(true)
      subs.destroy()
      subsRef.current = null
      v.removeAttribute('src')
      v.load()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [item.id, sourceId])

  // ---- video events ----
  useEffect(() => {
    const v = videoRef.current
    if (!v || !source) return
    const onTime = () => {
      const m = modeRef.current
      // A jump nobody asked for (no seek): a server stream that ran out. Neither shown nor
      // reported (it would mark the episode watched); 'ended' follows and reopens it.
      if (!clock.current.tick(v, m.kind === 'server')) return
      const t = absTime()
      reopens.current.progress(t) // real playback: far enough past a reopening, it recovered
      setCurrent(t)
      const off = m.kind === 'server' ? m.offset : 0
      if (v.buffered.length) setBuffered(off + v.buffered.end(v.buffered.length - 1))
      if (!v.paused) report()
      const ms = t * 1000
      const seg = (source.segments ?? []).find((s) => (s.type === 'intro' || s.type === 'outro') && ms >= s.startMs && ms < s.endMs - 1000) ?? null
      const p = prefs.get()
      if (seg && p.autoSkip && !v.paused && !v.seeking) {
        // Jump once per segment: the end is past the window, so this does not fire again.
        setSkip(null)
        seekTo(seg.endMs / 1000)
        return
      }
      // The button stays for the window, or the whole segment when the window is 0.
      setSkip(seg && (p.skipWindowSec === 0 || ms < seg.startMs + p.skipWindowSec * 1000) ? seg : null)
    }
    const onPlay = () => setPlaying(true)
    const onPause = () => {
      setPlaying(false)
      // A server stream that ran out pauses at the header's duration before 'ended': not
      // where the viewer is (reported, it marked the episode watched).
      if (!clock.current.pauseIsReal(v, modeRef.current.kind === 'server')) return
      report(true)
    }
    const onWaiting = () => setWaiting(true)
    const onPlaying = () => setWaiting(false)
    // A server stream (transcode or remux) that ended or dropped well before the end of
    // the file is a lost connection, not the end: the server ends a stream nobody read for
    // a long time (a long pause), and restarts end them too. Reopened where it was, a few
    // times in a row; it used to count as the end of the episode (marked watched, on to
    // the next one with autoplay) or stop with "Try again".
    // 'end' (the real end), 'reopened', or 'lost': cut too often in a row. Lost is said and
    // saved where the stream got to, never taken for the end (that marked the episode
    // watched at the header's duration and moved on to the next one).
    const reconnect = (): 'end' | 'reopened' | 'lost' => {
      const m = modeRef.current
      // Where the stream really got to (StreamClock): the playhead and buffered both say
      // "the end" once Chrome has jumped there.
      const e = streamEnded(m, clock.current.playedTo, duration, reopens.current.used)
      if (e.kind === 'end' || m.kind !== 'server') return 'end'
      if (e.kind === 'lost') {
        lastAbs.current = e.at
        report(true, e.at)
        setError('The connection to the server keeps dropping. Try again.')
        return 'lost'
      }
      reopens.current.reopened(e.at)
      reload(e.at, m.audio, true)
      return 'reopened'
    }
    const onError = () => {
      const code = v.error?.code
      if (code === 2 && reconnect() !== 'end') return
      if (code !== 4) {
        setError(code === 2 ? 'The connection to the server dropped while playing. Try again.' : code === 3 ? 'The video could not be decoded.' : `Playback error ${code ?? ''}`.trim())
        return
      }
      // Code 4 ("source not supported") is also what the browser reports when the URL
      // answers 404 (the file was deleted or replaced) or 401 / 403 (the media token
      // expired): ask the server about the same file before blaming the codec.
      const my = loadGen.current
      const direct = modeRef.current.kind === 'direct'
      probeStream(api.streamUrl(source.id, tokenRef.current)).then((status) => {
        if (my !== loadGen.current) return // a newer load replaced this stream meanwhile
        setError(status === 'ok' ? (direct ? 'This file cannot be played in this browser (unsupported video or audio codec).' : 'The server could not prepare a stream this browser can play.') : status)
      })
    }
    const onEnd = () => {
      if (reconnect() !== 'end') return
      report(true)
      if (autoplayRef.current) onEnded?.()
    }
    // A seek, or a new stream (its clock starts again at 0), is where playing goes on from.
    const onSeeked = () => clock.current.reset(v)
    v.addEventListener('seeked', onSeeked)
    v.addEventListener('loadstart', onSeeked)
    v.addEventListener('timeupdate', onTime)
    v.addEventListener('play', onPlay)
    v.addEventListener('pause', onPause)
    v.addEventListener('waiting', onWaiting)
    v.addEventListener('playing', onPlaying)
    v.addEventListener('error', onError)
    v.addEventListener('ended', onEnd)
    return () => {
      v.removeEventListener('seeked', onSeeked)
      v.removeEventListener('loadstart', onSeeked)
      v.removeEventListener('timeupdate', onTime)
      v.removeEventListener('play', onPlay)
      v.removeEventListener('pause', onPause)
      v.removeEventListener('waiting', onWaiting)
      v.removeEventListener('playing', onPlaying)
      v.removeEventListener('error', onError)
      v.removeEventListener('ended', onEnd)
    }
  }, [source, absTime, report, onEnded, seekTo, reload, duration])

  // ---- volume, rate, fullscreen ----
  useEffect(() => {
    const v = videoRef.current
    if (!v) return
    v.volume = volume
    v.muted = muted
    localStorage.setItem('roxy.volume', String(volume))
  }, [volume, muted])
  useEffect(() => {
    const v = videoRef.current
    if (v) v.playbackRate = rate
  }, [rate])
  // ---- subtitles follow the picked track and the on / off switch ----
  useEffect(() => {
    const subs = subsRef.current
    if (!subs || !source || tracksOf !== item.id) return
    const track = subsOn ? (subTracks.find((s) => s.index === subIndex) ?? null) : null
    subs.setTrack(source.id, track).catch(() => {
      if (subsRef.current === subs) showNotice(SUBS_FAILED)
    })
  }, [subIndex, subsOn, source, subTracks, showNotice, tracksOf, item.id])

  // ---- controls visibility ----
  const poke = useCallback(() => {
    setVisible(true)
    if (hideTimer.current) window.clearTimeout(hideTimer.current)
    hideTimer.current = window.setTimeout(() => {
      if (menu === 'none') setVisible(false)
    }, HIDE_AFTER_MS)
  }, [menu])
  useEffect(() => {
    if (!playing || menu !== 'none') setVisible(true)
    else poke()
  }, [playing, menu, poke])

  const togglePlay = useCallback(() => {
    const v = videoRef.current
    if (!v) return
    if (v.paused) void v.play().catch(() => undefined)
    else v.pause()
  }, [])
  const toggleFullscreen = useCallback(() => {
    const box = boxRef.current
    if (!box) return
    if (document.fullscreenElement) void document.exitFullscreen()
    else void box.requestFullscreen().catch(() => undefined)
  }, [])

  // ---- keyboard ----
  usePlayerKeys({
    togglePlay,
    seekBy: (sec) => seekTo(absTime() + sec),
    volumeBy: (d) => setVolume((x) => Math.min(1, Math.max(0, x + d))),
    toggleFullscreen,
    toggleMute: () => setMuted((m) => !m),
    toggleSubs: () => {
      ownTracks.current.sub = true
      setSubsOn((x) => !x)
    },
    next: next && onNext ? onNext : null,
    closeMenu: () => setMenu('none'),
    poke,
  })


  const changeAudio = (idx: number) => {
    ownTracks.current.audio = true
    reopens.current.reset()
    setAudioIndex(idx)
    setMenu('none')
    reload(absTime(), idx)
    lastReport.current = 0
  }
  const changeQuality = (q: Quality) => {
    qualityRef.current = q
    setQuality(q)
    setMenu('none')
    reopens.current.reset() // a new stream the viewer asked for: a fresh start
    reload(absTime(), audioIndex)
  }
  const srcHeight = source?.streams?.find((s) => s.type === 'video')?.height ?? 0
  const qualities: Quality[] = ['original', ...([1080, 720, 480] as const).filter((h) => srcHeight === 0 || h < srcHeight)]
  const changeSub = (idx: number | null) => {
    ownTracks.current.sub = true
    setSubIndex(idx)
    setSubsOn(idx != null)
    setMenu('none')
    lastReport.current = 0
  }

  const rating = item.officialRating ?? series?.officialRating
  const advisories = item.advisories?.length ? item.advisories : (series?.advisories ?? [])
  const hidden = !visible && playing && menu === 'none'

  if (!source) return <div className="player player--empty">No media for this title.</div>

  return (
    <div ref={boxRef} className={'player' + (hidden ? ' player--hidden' : '')} onMouseMove={poke} onMouseLeave={() => playing && menu === 'none' && setVisible(false)}>
      <video ref={videoRef} playsInline preload="metadata" onClick={togglePlay} onDoubleClick={toggleFullscreen} />
      {waiting && !error && (
        <div className="player__wait">
          <div className="spinner" />
        </div>
      )}
      {advisory && rating && (
        <div className="player__advisory">
          <span className="player__age">{ageLabel(rating)}</span>
          {advisories.length > 0 && <span>{advisories.join(', ')}</span>}
        </div>
      )}
      {skip && (
        <button className="btn btn--white player__skip" onClick={() => seekTo(skip.endMs / 1000)}>
          {skip.type === 'intro' ? 'Skip intro' : 'Skip outro'}
        </button>
      )}
      {error && <div className="player__error">{error}</div>}
      {notice && !error && (
        <div className="player__notice" role="status">
          {notice}
        </div>
      )}

      <div className="player__controls" onClick={(e) => e.stopPropagation()}>
        <SeekBar sourceId={source.id} duration={duration} current={current} buffered={buffered} segments={source.segments ?? []} trick={trick} onSeek={seekTo} />
        <div className="controls">
          <button className="controls__btn" onClick={() => seekTo(absTime() - 10)} aria-label="Back 10 seconds">
            <IconReplay10 size={26} />
          </button>
          <button className="controls__btn" onClick={togglePlay} aria-label={playing ? 'Pause' : 'Play'}>
            {playing ? <IconPause size={30} /> : <IconPlay size={30} />}
          </button>
          <button className="controls__btn" onClick={() => seekTo(absTime() + 10)} aria-label="Forward 10 seconds">
            <IconForward10 size={26} />
          </button>
          <div className="controls__volume">
            <button className="controls__btn" onClick={() => setMuted((m) => !m)} aria-label={muted ? 'Unmute' : 'Mute'}>
              {muted || volume === 0 ? <IconVolumeOff size={26} /> : <IconVolume size={26} />}
            </button>
            <input type="range" min={0} max={1} step={0.05} value={muted ? 0 : volume} onChange={(e) => { setVolume(Number(e.target.value)); setMuted(false) }} aria-label="Volume" />
          </div>
          <div className="controls__time">
            {fmt(current)} / {fmt(duration)}
          </div>
          <div className="controls__spacer" />
          {next && (
            <button className="controls__btn" onClick={onNext} aria-label="Next episode" title={next.title}>
              <IconSkipNext size={28} />
            </button>
          )}
          {(subTracks.length > 0 || audioTracks.length > 1) && (
            <div className="controls__menu-anchor">
              <button className={'controls__btn' + (menu === 'tracks' ? ' controls__btn--open' : '')} onClick={() => setMenu((m) => (m === 'tracks' ? 'none' : 'tracks'))} aria-label="Audio and subtitles">
                <IconSubtitles size={26} />
              </button>
              {menu === 'tracks' && (
                <TracksPanel audioTracks={audioTracks} audioIndex={audioIndex} onAudio={changeAudio} subTracks={subTracks} subIndex={subIndex} subsOn={subsOn} onSub={changeSub} />
              )}
            </div>
          )}
          <div className="controls__menu-anchor">
            <button className={'controls__btn controls__rate' + (menu === 'speed' ? ' controls__btn--open' : '')} onClick={() => setMenu((m) => (m === 'speed' ? 'none' : 'speed'))} aria-label="Playback speed">
              {rate}x
            </button>
            {menu === 'speed' && (
              <SpeedPanel speeds={SPEEDS} rate={rate} onRate={(r) => { setRate(r); setMenu('none') }} />
            )}
          </div>
          <div className="controls__menu-anchor">
            <button className={'controls__btn' + (menu === 'quality' ? ' controls__btn--open' : '')} onClick={() => setMenu((m) => (m === 'quality' ? 'none' : 'quality'))} aria-label="Settings">
              <IconSettings size={26} />
            </button>
            {menu === 'quality' && (
              <SettingsPanel autoplay={autoplay} onAutoplay={() => setAutoplay((a) => !a)} qualities={qualities} quality={quality} srcHeight={srcHeight} onQuality={changeQuality} />
            )}
          </div>
          <button className="controls__btn" onClick={toggleFullscreen} aria-label="Fullscreen">
            <IconFullscreen size={26} />
          </button>
        </div>
      </div>
    </div>
  )
}

export { fmt as formatTime }
