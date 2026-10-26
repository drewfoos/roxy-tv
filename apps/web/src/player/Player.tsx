import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from 'react'
import { api } from '../api/client'
import { ageLabel, languageName, pickSubtitle, primarySource } from '../api/items'
import type { Item, MediaSegment, MediaStream, TrickplayManifest } from '../api/types'
import { IconCheckCircle, IconFullscreen, IconPause, IconPlay, IconSettings, IconSkipNext, IconSubtitles, IconVolume, IconVolumeOff, IconForward10, IconReplay10 } from '../components/Icons'
import { createSubtitleRenderer, type SubtitleRenderer } from './subtitles'

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

/** Quality choices (Crunchyroll's gear): the file itself, or a server H.264 encode at a height. */
type Quality = 'original' | 1080 | 720 | 480
const QUALITY_KBPS: Record<Exclude<Quality, 'original'>, number> = { 1080: 8000, 720: 4000, 480: 1500 }

function fmt(sec: number): string {
  if (!isFinite(sec) || sec < 0) sec = 0
  const s = Math.floor(sec % 60)
  const m = Math.floor((sec / 60) % 60)
  const h = Math.floor(sec / 3600)
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}` : `${m}:${String(s).padStart(2, '0')}`
}

function trackLabel(s: MediaStream): string {
  const lang = s.detectedLanguage || s.language
  const parts = [lang ? languageName(lang) : null, s.title && !/^(und|unknown)$/i.test(s.title) ? s.title : null]
  const label = parts.filter(Boolean).join(' · ') || `Track ${s.index}`
  return s.type === 'audio' && s.channels ? `${label} (${s.channels > 2 ? `${s.channels - 1}.1` : s.channels === 2 ? 'stereo' : 'mono'})` : label
}

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
  const hideTimer = useRef<number | null>(null)
  const seekBarRef = useRef<HTMLDivElement>(null)

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
  const [quality, setQuality] = useState<Quality>('original')
  const qualityRef = useRef<Quality>('original')
  const [skip, setSkip] = useState<MediaSegment | null>(null)
  const [advisory, setAdvisory] = useState(true)
  const [hover, setHover] = useState<{ x: number; t: number } | null>(null)
  const [trick, setTrick] = useState<TrickplayManifest | null>(null)
  const [audioIndex, setAudioIndex] = useState<number | null>(() => {
    const want = item.userData?.audioIndex
    return want != null && audioTracks.some((t) => t.index === want) ? want : (audioTracks[0]?.index ?? null)
  })
  const [subIndex, setSubIndex] = useState<number | null>(() => {
    const want = item.userData?.subtitleIndex
    if (want != null && subTracks.some((t) => t.index === want)) return want
    return pickSubtitle(source)?.index ?? null
  })
  const [subsOn, setSubsOn] = useState(true)

  const absTime = useCallback(() => {
    const v = videoRef.current
    if (!v) return 0
    const m = modeRef.current
    return (m.kind === 'server' ? m.offset : 0) + v.currentTime
  }, [])

  const report = useCallback(
    (force = false) => {
      const now = Date.now()
      if (!force && now - lastReport.current < REPORT_EVERY_MS) return
      lastReport.current = now
      api.progress(item.id, Math.round(absTime() * 1000), audioIndex ?? undefined, subsOn ? (subIndex ?? undefined) : undefined).catch(() => undefined)
    },
    [item.id, absTime, audioIndex, subIndex, subsOn],
  )

  /** Decide direct play or remux for the chosen audio track, and load the stream at a position. */
  const load = useCallback(
    async (startSec: number, audio: number | null) => {
      const v = videoRef.current
      if (!v || !source) return
      if (!tokenRef.current) tokenRef.current = (await api.mediaToken(source.id)).token
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
              resolve()
            }
            v.addEventListener('loadedmetadata', once)
            v.addEventListener('error', once)
          })
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
    tokenRef.current = ''
    const subs = createSubtitleRenderer(v)
    subsRef.current = subs
    const start = item.userData?.played ? 0 : (item.userData?.positionMs ?? 0) / 1000
    load(start > 5 ? start : 0, audioIndex).catch((e: unknown) => live && setError(e instanceof Error ? e.message : String(e)))
    const sub = subTracks.find((s) => s.index === subIndex) ?? null
    if (sub) subs.setTrack(source.id, sub).catch(() => undefined)
    api.trickplay(source.id).then((m) => live && setTrick(m)).catch(() => undefined)
    const t = setTimeout(() => live && setAdvisory(false), 7000)
    return () => {
      live = false
      clearTimeout(t)
      report(true)
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
      setSkip((source.segments ?? []).find((s) => (s.type === 'intro' || s.type === 'outro') && ms >= s.startMs && ms < s.endMs - 1000) ?? null)
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
      onEnded?.()
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
  }, [source, absTime, report, onEnded])

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
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement | null
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT')) return
      switch (e.key) {
        case ' ':
        case 'k':
          e.preventDefault()
          togglePlay()
          break
        case 'ArrowLeft':
        case 'j':
          e.preventDefault()
          seekTo(absTime() - 10)
          break
        case 'ArrowRight':
        case 'l':
          e.preventDefault()
          seekTo(absTime() + 10)
          break
        case 'ArrowUp':
          e.preventDefault()
          setVolume((x) => Math.min(1, x + 0.1))
          break
        case 'ArrowDown':
          e.preventDefault()
          setVolume((x) => Math.max(0, x - 0.1))
          break
        case 'f':
          toggleFullscreen()
          break
        case 'm':
          setMuted((m) => !m)
          break
        case 'c':
          setSubsOn((s) => !s)
          break
        case 'n':
          if (next) onNext?.()
          break
        case 'Escape':
          setMenu('none')
          break
        default:
          return
      }
      poke()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [togglePlay, seekTo, absTime, toggleFullscreen, next, onNext, poke])

  // ---- seek bar ----
  const barTime = (e: ReactMouseEvent) => {
    const bar = seekBarRef.current
    if (!bar || !duration) return null
    const r = bar.getBoundingClientRect()
    const f = Math.max(0, Math.min(1, (e.clientX - r.left) / r.width))
    return { x: e.clientX - r.left, t: f * duration }
  }
  const onBarMove = (e: ReactMouseEvent) => setHover(barTime(e))
  const onBarClick = (e: ReactMouseEvent) => {
    const h = barTime(e)
    if (h) seekTo(h.t)
  }
  const preview = useMemo(() => {
    if (!hover || !trick || !source || trick.count === 0) return null
    const tile = Math.min(trick.count - 1, Math.max(0, Math.floor((hover.t * 1000) / trick.intervalMs)))
    const per = trick.columns * trick.rows
    const sheet = Math.floor(tile / per)
    const col = (tile % per) % trick.columns
    const row = Math.floor((tile % per) / trick.columns)
    return { sheet, x: col * trick.width, y: row * trick.height }
  }, [hover, trick, source])

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
  const played = duration ? Math.min(1, current / duration) : 0
  const buff = duration ? Math.min(1, buffered / duration) : 0
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

      <div className="player__controls" onClick={(e) => e.stopPropagation()}>
        <div ref={seekBarRef} className="seek" onMouseMove={onBarMove} onMouseLeave={() => setHover(null)} onClick={onBarClick}>
          <div className="seek__track">
            <div className="seek__buffered" style={{ width: `${buff * 100}%` }} />
            <div className="seek__played" style={{ width: `${played * 100}%` }} />
          </div>
          <div className="seek__thumb" style={{ left: `${played * 100}%` }} />
          {(source.segments ?? []).map((s, i) =>
            duration ? <div key={i} className="seek__segment" style={{ left: `${(s.startMs / 1000 / duration) * 100}%`, width: `${((s.endMs - s.startMs) / 1000 / duration) * 100}%` }} /> : null,
          )}
          {hover && (
            <div className="seek__hover" style={{ left: hover.x }}>
              {preview && trick && (
                <div className="seek__thumbnail" style={{ width: trick.width, height: trick.height, backgroundImage: `url(${api.trickplaySheetUrl(source.id, preview.sheet)})`, backgroundPosition: `-${preview.x}px -${preview.y}px` }} />
              )}
              <div className="seek__time">{fmt(hover.t)}</div>
            </div>
          )}
        </div>
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
              <button className={'controls__btn' + (subsOn && subTracks.length > 0 ? ' controls__btn--on' : '')} onClick={() => setMenu((m) => (m === 'tracks' ? 'none' : 'tracks'))} aria-label="Audio and subtitles">
                <IconSubtitles size={26} />
              </button>
              {menu === 'tracks' && (
                <div className="pmenu pmenu--wide">
                  {audioTracks.length > 0 && (
                    <>
                      <div className="pmenu__title">Audio</div>
                      {audioTracks.map((t) => (
                        <button key={t.index} className={'pmenu__item' + (t.index === audioIndex ? ' pmenu__item--on' : '')} onClick={() => changeAudio(t.index)}>
                          {trackLabel(t)}
                          {t.index === audioIndex && <IconCheckCircle size={18} />}
                        </button>
                      ))}
                    </>
                  )}
                  <div className="pmenu__title">Subtitles</div>
                  <button className={'pmenu__item' + (!subsOn ? ' pmenu__item--on' : '')} onClick={() => changeSub(null)}>
                    Off
                    {!subsOn && <IconCheckCircle size={18} />}
                  </button>
                  {subTracks.map((t) => (
                    <button key={t.index} className={'pmenu__item' + (subsOn && t.index === subIndex ? ' pmenu__item--on' : '')} onClick={() => changeSub(t.index)}>
                      {trackLabel(t)}
                      {subsOn && t.index === subIndex && <IconCheckCircle size={18} />}
                    </button>
                  ))}
                </div>
              )}
            </div>
          )}
          <div className="controls__menu-anchor">
            <button className="controls__btn controls__rate" onClick={() => setMenu((m) => (m === 'speed' ? 'none' : 'speed'))} aria-label="Speed">
              {rate}x
            </button>
            {menu === 'speed' && (
              <div className="pmenu">
                <div className="pmenu__title">Speed</div>
                {SPEEDS.map((s) => (
                  <button key={s} className={'pmenu__item' + (s === rate ? ' pmenu__item--on' : '')} onClick={() => { setRate(s); setMenu('none') }}>
                    {s === 1 ? 'Normal' : `${s}x`}
                    {s === rate && <IconCheckCircle size={18} />}
                  </button>
                ))}
              </div>
            )}
          </div>
          <div className="controls__menu-anchor">
            <button className="controls__btn" onClick={() => setMenu((m) => (m === 'quality' ? 'none' : 'quality'))} aria-label="Quality">
              <IconSettings size={26} />
            </button>
            {menu === 'quality' && (
              <div className="pmenu">
                <div className="pmenu__title">Quality</div>
                {qualities.map((q) => (
                  <button key={q} className={'pmenu__item' + (q === quality ? ' pmenu__item--on' : '')} onClick={() => changeQuality(q)}>
                    {q === 'original' ? (srcHeight ? `${srcHeight}p (Original)` : 'Original') : `${q}p`}
                    {q === quality && <IconCheckCircle size={18} />}
                  </button>
                ))}
              </div>
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
