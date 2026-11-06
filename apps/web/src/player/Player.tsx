import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api/client'
import { ageLabel, pickSubtitle, primarySource } from '../api/items'
import { lang3, prefs } from '../api/prefs'
import { SUBTITLES_OFF, type Item, type MediaSegment, type MediaStream, type TrickplayManifest } from '../api/types'
import { IconFullscreen, IconPause, IconPlay, IconSettings, IconSkipNext, IconSubtitles, IconVolume, IconVolumeOff, IconForward10, IconReplay10 } from '../components/Icons'
import { fmt } from './format'
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

/** Codecs every browser decodes; a file whose chosen track is anything else is remuxed. */
const BROWSER_AUDIO = new Set(['aac', 'opus', 'flac', 'vorbis', 'mp3'])
const REPORT_EVERY_MS = 10_000
const HIDE_AFTER_MS = 3000
const SPEEDS = [0.5, 0.75, 1, 1.25, 1.5, 2]

/** Direct play, or a server stream (remux or transcode) whose clock starts at `offset`. */
type Mode = { kind: 'direct' } | { kind: 'server'; audio: number | null; offset: number }

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
  const savedAudio = item.userData?.audioIndex
  const audio = savedAudio != null && audioTracks.some((t) => t.index === savedAudio)
    ? savedAudio
    : (audioTracks.find((t) => lang3(t.language) === p.audioLang)?.index ?? audioTracks[0]?.index ?? null)
  const savedSub = item.userData?.subtitleIndex
  const sub = savedSub != null && subTracks.some((t) => t.index === savedSub) ? savedSub : (pickSubtitle(source, p.subLang)?.index ?? null)
  let subsOn = true
  if (savedSub === SUBTITLES_OFF) subsOn = false
  else if (savedSub == null) {
    if (p.subMode === 'none') subsOn = false
    else if (p.subMode === 'foreign') {
      const a = audioTracks.find((t) => t.index === (savedAudio ?? -1)) ?? audioTracks.find((t) => lang3(t.language) === p.audioLang) ?? audioTracks[0]
      subsOn = !!a && lang3(a.language) !== p.audioLang
    }
  }
  return { audio, sub, subsOn }
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
 * (video copied, that one track) and the clock restarts at the start point, which the
 * player adds back. Subtitles are jassub over either stream.
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
  const lastReport = useRef(0)
  // The last position read from a live <video>. On exit React clears videoRef before the
  // effects' cleanups run, so the final report (and the pause the removal fires) would
  // read 0 and wipe the saved position; they use this instead.
  const lastAbs = useRef(0)
  const hideTimer = useRef<number | null>(null)
  // Each load() takes a number; one that wakes from an await to find a newer number stops
  // there. A seek, a track or quality change, or the next episode can start a load while an
  // older one is still waiting (on the token, the metadata, play()), and the older one used
  // to go on and seek or flag the newer stream.
  const loadGen = useRef(0)

  const [ready, setReady] = useState(false)
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
  // "2 subtitle fonts didn't load": shown for a few seconds when a font download fails.
  const [fontWarn, setFontWarn] = useState<string | null>(null)
  const fontWarnTimer = useRef<number | null>(null)
  const [trick, setTrick] = useState<TrickplayManifest | null>(null)
  // Set again for every new item in the start effect below (see initialTracks).
  const [audioIndex, setAudioIndex] = useState<number | null>(() => initialTracks(item, source, audioTracks, subTracks).audio)
  const [subIndex, setSubIndex] = useState<number | null>(() => initialTracks(item, source, audioTracks, subTracks).sub)
  const [subsOn, setSubsOn] = useState(() => initialTracks(item, source, audioTracks, subTracks).subsOn)

  const absTime = useCallback(() => {
    const v = videoRef.current
    if (!v) return lastAbs.current
    const m = modeRef.current
    const t = (m.kind === 'server' ? m.offset : 0) + v.currentTime
    lastAbs.current = t
    return t
  }, [])

  const report = useCallback(
    (force = false) => {
      const now = Date.now()
      if (!force && now - lastReport.current < REPORT_EVERY_MS) return
      // Never report 0 (the apps don't either): a position of 0 is "nothing watched", and
      // the server would store it over the real one.
      const positionMs = Math.round(absTime() * 1000)
      if (positionMs <= 0) return
      lastReport.current = now
      // Subtitles off is saved as SUBTITLES_OFF; leaving it out (as this used to) kept the
      // last track saved, so they came back on next time.
      api.progress(item.id, positionMs, audioIndex ?? undefined, subsOn ? (subIndex ?? undefined) : SUBTITLES_OFF).catch(() => undefined)
    },
    [item.id, absTime, audioIndex, subIndex, subsOn],
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

  /** Decide direct play or remux for the chosen audio track, and load the stream at a position. */
  const load = useCallback(
    async (startSec: number, audio: number | null) => {
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
      const direct = q === 'original' && (!chosen || (chosen.index === first?.index && BROWSER_AUDIO.has(chosen.codec.toLowerCase())))
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
        modeRef.current = { kind: 'server', audio: chosen?.index ?? null, offset: startSec }
        v.src = api.remuxUrl(source.id, chosen?.index ?? null, startSec, tokenRef.current)
        subsRef.current?.setOffset(startSec)
      }
      setCurrent(startSec)
      setBuffered(startSec)
      v.playbackRate = rate
      if (wasPlaying || !ready) await v.play().catch(() => undefined)
      if (stale()) return
      setReady(true)
    },
    [source, audioTracks, rate, ready],
  )

  const seekTo = useCallback(
    (t: number) => {
      const v = videoRef.current
      if (!v) return
      t = Math.max(0, Math.min(duration || Infinity, t))
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
        void load(t, m.audio)
      }
    },
    [duration, load],
  )

  // ---- start: token, stream, subtitles, trickplay ----
  useEffect(() => {
    const v = videoRef.current
    if (!v || !source) return
    let live = true
    setError(null)
    setReady(false)
    setAdvisory(true)
    // The previous episode's seek previews: its tile size and sheet numbers must not be used
    // with this source's images while the new manifest loads (or at all, if it has none).
    setTrick(null)
    tokenRef.current = ''
    lastAbs.current = 0 // a new item: never carry the last one's position over
    setFontWarn(null)
    const subs = createSubtitleRenderer(v, (names) => {
      if (!live) return
      setFontWarn(`${names.length === 1 ? `The subtitle font ${names[0]} didn't` : `${names.length} subtitle fonts didn't`} load, so some signs may look wrong.`)
      if (fontWarnTimer.current) clearTimeout(fontWarnTimer.current)
      fontWarnTimer.current = window.setTimeout(() => setFontWarn(null), 8000)
    })
    subsRef.current = subs
    // This item's own tracks, not the previous episode's (the Player is reused).
    const tracks = initialTracks(item, source, audioTracks, subTracks)
    setAudioIndex(tracks.audio)
    setSubIndex(tracks.sub)
    setSubsOn(tracks.subsOn)
    setSkip(null)
    // A watched episode with a position is a rewatch under way: resume it, as the apps do.
    const start = (item.userData?.positionMs ?? 0) / 1000
    load(start > 5 ? start : 0, tracks.audio).catch((e: unknown) => live && setError(e instanceof Error ? e.message : String(e)))
    const sub = tracks.subsOn ? (subTracks.find((s) => s.index === tracks.sub) ?? null) : null
    if (sub) subs.setTrack(source.id, sub).catch(() => undefined)
    api.trickplay(source.id).then((m) => live && setTrick(m)).catch(() => undefined)
    const t = setTimeout(() => live && setAdvisory(false), 7000)
    return () => {
      live = false
      loadGen.current++ // a load still in flight for this item stops at its next await
      clearTimeout(t)
      if (fontWarnTimer.current) clearTimeout(fontWarnTimer.current)
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
      const t = absTime()
      setCurrent(t)
      const m = modeRef.current
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
      report(true)
    }
    const onWaiting = () => setWaiting(true)
    const onPlaying = () => setWaiting(false)
    const onError = () => {
      const code = v.error?.code
      setError(code === 4 ? 'This file cannot be played in this browser (unsupported video or audio codec).' : `Playback error ${code ?? ''}`.trim())
    }
    const onEnd = () => {
      report(true)
      if (autoplayRef.current) onEnded?.()
    }
    v.addEventListener('timeupdate', onTime)
    v.addEventListener('play', onPlay)
    v.addEventListener('pause', onPause)
    v.addEventListener('waiting', onWaiting)
    v.addEventListener('playing', onPlaying)
    v.addEventListener('error', onError)
    v.addEventListener('ended', onEnd)
    return () => {
      v.removeEventListener('timeupdate', onTime)
      v.removeEventListener('play', onPlay)
      v.removeEventListener('pause', onPause)
      v.removeEventListener('waiting', onWaiting)
      v.removeEventListener('playing', onPlaying)
      v.removeEventListener('error', onError)
      v.removeEventListener('ended', onEnd)
    }
  }, [source, absTime, report, onEnded, seekTo])

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
    if (!subs || !source) return
    const track = subsOn ? (subTracks.find((s) => s.index === subIndex) ?? null) : null
    subs.setTrack(source.id, track).catch(() => undefined)
  }, [subIndex, subsOn, source, subTracks])

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
    toggleSubs: () => setSubsOn((x) => !x),
    next: next && onNext ? onNext : null,
    closeMenu: () => setMenu('none'),
    poke,
  })


  const changeAudio = (idx: number) => {
    setAudioIndex(idx)
    setMenu('none')
    void load(absTime(), idx)
    lastReport.current = 0
  }
  const changeQuality = (q: Quality) => {
    qualityRef.current = q
    setQuality(q)
    setMenu('none')
    void load(absTime(), audioIndex)
  }
  const srcHeight = source?.streams?.find((s) => s.type === 'video')?.height ?? 0
  const qualities: Quality[] = ['original', ...([1080, 720, 480] as const).filter((h) => srcHeight === 0 || h < srcHeight)]
  const changeSub = (idx: number | null) => {
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
      {fontWarn && !error && (
        <div className="player__notice" role="status">
          {fontWarn}
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
