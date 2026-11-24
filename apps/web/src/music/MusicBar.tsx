// The player bar along the bottom of every page while there is a queue, with what opens
// from it: the queue (a drawer at the right) and "now playing" (the cover large, with
// the lyrics or the queue beside it). Three parts, as music players lay them out: the
// song at the left, the controls over the seek line in the middle, the rest at the right.
import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent } from 'react'
import { Link, useLocation } from 'react-router'
import { clock, isHiRes, longDuration, music, qualityLabel, type MusicTrack } from '../api/music'
import { useAsync } from '../hooks/useAsync'
import { IconClose, IconExpand, IconMic, IconPause, IconPlay, IconQueue, IconRepeat, IconRepeatOne, IconShuffle, IconSkipNext, IconSkipPrevious, IconVolume, IconVolumeOff } from '../components/Icons'
import { ArtistLinks, Cover, Equalizer, HeartButton, TrackMenu } from './Parts'
import { claimMediaKeys, currentTrack, player } from './player'
import { lyricIndexAt } from './queue'
import { usePlayer, useProgress } from './usePlayer'
import '../styles/music.css'

type Open = 'none' | 'queue' | 'now'
type NowTab = 'lyrics' | 'queue'

export function MusicBar() {
  const state = usePlayer()
  const track = currentTrack(state)
  const [open, setOpen] = useState<Open>('none')
  const [tab, setTab] = useState<NowTab>('lyrics')
  const loc = useLocation()
  useEffect(() => claimMediaKeys(), [])
  // Going to another page closes "now playing" (a link in it was followed); the queue
  // drawer stays, like the bar.
  useEffect(() => setOpen((o) => (o === 'now' ? 'none' : o)), [loc.pathname])
  useEffect(() => {
    if (open === 'none') return
    const key = (e: KeyboardEvent) => e.key === 'Escape' && setOpen('none')
    window.addEventListener('keydown', key)
    return () => window.removeEventListener('keydown', key)
  }, [open])
  if (!track) return null
  const showNow = (t: NowTab) => {
    setTab(t)
    setOpen((o) => (o === 'now' && tab === t ? 'none' : 'now'))
  }
  const repeatLabel = state.repeat === 'off' ? 'Repeat' : state.repeat === 'all' ? 'Repeat one' : 'Repeat off'
  return (
    <>
      {open === 'now' && <NowPlaying track={track} tab={tab} onTab={setTab} onClose={() => setOpen('none')} />}
      {open === 'queue' && <QueueDrawer onClose={() => setOpen('none')} />}
      <div className="mbar" role="region" aria-label="Music player">
        <div className="mbar__song">
          <button type="button" className="mbar__cover" onClick={() => showNow(track.lyrics ? 'lyrics' : 'queue')} aria-label="Now playing" title="Now playing">
            <Cover art={track.art} size={60} />
            <span className="mbar__grow">
              <IconExpand size={22} />
            </span>
          </button>
          <div className="mbar__names">
            <div className="mbar__title">{track.title}</div>
            <div className="mbar__artist">
              <ArtistLinks track={track} />
            </div>
            {track.albumId && track.album && (
              <div className="mbar__album">
                <Link to={'/music/album/' + track.albumId}>{track.album}</Link>
              </div>
            )}
          </div>
        </div>

        <div className="mbar__middle">
          <div className="mbar__buttons">
            <button type="button" className={'micon' + (state.shuffle ? ' micon--on' : '')} onClick={() => player.setShuffle(!state.shuffle)} aria-pressed={state.shuffle} aria-label="Shuffle" title="Shuffle">
              <IconShuffle size={20} />
            </button>
            <button type="button" className="micon" onClick={() => player.previous()} aria-label="Previous" title="Previous">
              <IconSkipPrevious size={26} />
            </button>
            <button type="button" className="mbar__play" onClick={() => player.toggle()} aria-label={state.playing ? 'Pause' : 'Play'} title={state.playing ? 'Pause' : 'Play'} aria-busy={state.waiting}>
              {state.playing ? <IconPause size={26} /> : <IconPlay size={26} />}
            </button>
            <button type="button" className="micon" onClick={() => player.next()} aria-label="Next" title="Next">
              <IconSkipNext size={26} />
            </button>
            <button type="button" className={'micon' + (state.repeat !== 'off' ? ' micon--on' : '')} onClick={() => player.cycleRepeat()} aria-label={repeatLabel} title={repeatLabel}>
              {state.repeat === 'one' ? <IconRepeatOne size={20} /> : <IconRepeat size={20} />}
            </button>
          </div>
          <SeekLine />
        </div>

        <div className="mbar__rest">
          <span className={'mbar__quality' + (isHiRes(track) ? ' mbar__quality--hires' : '')} title="How this file is encoded">
            {qualityLabel(track)}
          </span>
          <HeartButton kind="track" id={track.id} loaded={track.favorite} />
          {track.lyrics && (
            <button type="button" className={'micon' + (open === 'now' && tab === 'lyrics' ? ' micon--on' : '')} onClick={() => showNow('lyrics')} aria-label="Lyrics" title="Lyrics">
              <IconMic size={20} />
            </button>
          )}
          <button type="button" className={'micon' + (open === 'queue' ? ' micon--on' : '')} onClick={() => setOpen((o) => (o === 'queue' ? 'none' : 'queue'))} aria-label="Queue" title="Queue" aria-expanded={open === 'queue'}>
            <IconQueue size={22} />
          </button>
          <Volume />
        </div>
        {state.error && (
          <div className="mbar__error" role="alert">
            {state.error}
            <button type="button" onClick={() => player.dismissError()} aria-label="Dismiss">
              <IconClose size={16} />
            </button>
          </div>
        )}
      </div>
    </>
  )
}

/** A slider drawn as a line: dragged or pressed with the pointer, moved with the arrow keys. */
function Slider({ value, max, onChange, onCommit, label, text, step, className = '' }: { value: number; max: number; onChange?: (v: number) => void; onCommit: (v: number) => void; label: string; text: string; step: number; className?: string }) {
  const ref = useRef<HTMLDivElement>(null)
  const [drag, setDrag] = useState<number | null>(null)
  const shown = drag ?? value
  const at = (e: ReactPointerEvent) => {
    const box = ref.current!.getBoundingClientRect()
    return Math.max(0, Math.min(1, (e.clientX - box.left) / Math.max(1, box.width))) * max
  }
  const down = (e: ReactPointerEvent) => {
    if (!(max > 0)) return
    ref.current!.setPointerCapture(e.pointerId)
    const v = at(e)
    setDrag(v)
    onChange?.(v)
  }
  const move = (e: ReactPointerEvent) => {
    if (drag == null) return
    const v = at(e)
    setDrag(v)
    onChange?.(v)
  }
  const up = (e: ReactPointerEvent) => {
    if (drag == null) return
    const v = at(e)
    setDrag(null)
    onCommit(v)
  }
  const key = (e: ReactKeyboardEvent) => {
    const by = e.key === 'ArrowRight' || e.key === 'ArrowUp' ? step : e.key === 'ArrowLeft' || e.key === 'ArrowDown' ? -step : 0
    if (!by) return
    e.preventDefault()
    onCommit(Math.max(0, Math.min(max, value + by)))
  }
  const pct = max > 0 ? Math.max(0, Math.min(100, (shown / max) * 100)) : 0
  return (
    <div
      ref={ref}
      className={'mslider' + (drag != null ? ' mslider--drag' : '') + (className ? ' ' + className : '')}
      role="slider"
      tabIndex={0}
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={Math.round(max)}
      aria-valuenow={Math.round(shown)}
      aria-valuetext={text}
      onPointerDown={down}
      onPointerMove={move}
      onPointerUp={up}
      onPointerCancel={() => setDrag(null)}
      onKeyDown={key}
    >
      <div className="mslider__line">
        <div className="mslider__fill" style={{ width: pct + '%' }} />
      </div>
      <div className="mslider__thumb" style={{ left: pct + '%' }} />
    </div>
  )
}

/** The time gone, the line to seek on, the song's length. */
function SeekLine() {
  const { position, duration } = useProgress()
  // While the line is dragged the time at its left follows the pointer; the song is
  // moved once, when the pointer is let go.
  const [drag, setDrag] = useState<number | null>(null)
  const shown = drag ?? position
  return (
    <div className="mbar__seek">
      <span className="mbar__time">{clock(shown * 1000)}</span>
      <Slider
        value={position}
        max={duration}
        step={5}
        label="Seek"
        text={`${clock(shown * 1000)} of ${clock(duration * 1000)}`}
        onChange={setDrag}
        onCommit={(v) => {
          setDrag(null)
          player.seek(v)
        }}
      />
      <span className="mbar__time">{clock(duration * 1000)}</span>
    </div>
  )
}

function Volume() {
  const { volume, muted } = usePlayer()
  const shown = muted ? 0 : volume
  return (
    <div className="mbar__volume">
      <button type="button" className="micon" onClick={() => player.toggleMute()} aria-label={muted ? 'Unmute' : 'Mute'} title={muted ? 'Unmute' : 'Mute'}>
        {shown === 0 ? <IconVolumeOff size={20} /> : <IconVolume size={20} />}
      </button>
      <Slider value={shown * 100} max={100} step={5} label="Volume" text={Math.round(shown * 100) + '%'} onChange={(v) => player.setVolume(v / 100)} onCommit={(v) => player.setVolume(v / 100)} className="mslider--volume" />
    </div>
  )
}

// ---- the queue ----

function QueueList() {
  const state = usePlayer()
  const { tracks, index } = state.queue
  const currentRef = useRef<HTMLDivElement>(null)
  // Opened on the song that is playing, not at the top of a long queue.
  useLayoutEffect(() => {
    currentRef.current?.scrollIntoView({ block: 'center' })
  }, [])
  const left = tracks.slice(index + 1).reduce((ms, t) => ms + t.durationMs, 0)
  return (
    <>
      <div className="mqueue__head">
        <b>Queue</b>
        <span>
          {tracks.length} {tracks.length === 1 ? 'song' : 'songs'}
          {left > 0 && ` · ${longDuration(left)} left`}
        </span>
        <button type="button" className="link-brand" onClick={() => player.clear()}>
          Clear
        </button>
      </div>
      <div className="mqueue__list">
        {tracks.map((t, i) => (
          <div
            key={t.id + ':' + i}
            ref={i === index ? currentRef : undefined}
            className={'qrow' + (i === index ? ' qrow--current' : '') + (i < index ? ' qrow--played' : '')}
            onClick={() => (i === index ? player.toggle() : player.jump(i))}
          >
            <span className="qrow__art">
              <Cover art={t.art} size={40} />
              {i === index && (
                <span className="qrow__eq">
                  <Equalizer moving={state.playing} />
                </span>
              )}
            </span>
            <span className="qrow__main">
              <span className="qrow__title">{t.title}</span>
              <span className="qrow__artist">{t.artist}</span>
            </span>
            <span className="qrow__time">{clock(t.durationMs)}</span>
            <button
              type="button"
              className="micon qrow__remove"
              onClick={(e) => {
                e.stopPropagation()
                player.remove(i)
              }}
              aria-label={'Remove ' + t.title + ' from the queue'}
              title="Remove"
            >
              <IconClose size={18} />
            </button>
          </div>
        ))}
      </div>
    </>
  )
}

function QueueDrawer({ onClose }: { onClose: () => void }) {
  return (
    <aside className="mqueue" aria-label="Queue">
      <button type="button" className="micon mqueue__close" onClick={onClose} aria-label="Close the queue">
        <IconClose size={20} />
      </button>
      <QueueList />
    </aside>
  )
}

// ---- now playing ----

function NowPlaying({ track, tab, onTab, onClose }: { track: MusicTrack; tab: NowTab; onTab: (t: NowTab) => void; onClose: () => void }) {
  const art = music.artUrl(track.art, 640)
  return (
    <div className="mnow" role="dialog" aria-label="Now playing">
      {art && <div className="mnow__back" style={{ backgroundImage: `url(${art})` }} />}
      <div className="mnow__shade" />
      <button type="button" className="micon mnow__close" onClick={onClose} aria-label="Close">
        <IconClose size={26} />
      </button>
      <div className="mnow__body">
        <div className="mnow__song">
          <Cover art={track.art} size={520} className="mnow__cover" />
          <div className="mnow__title">{track.title}</div>
          <div className="mnow__artist">
            <ArtistLinks track={track} />
          </div>
          {track.albumId && track.album && (
            <div className="mnow__album">
              <Link to={'/music/album/' + track.albumId}>{track.album}</Link>
              {track.year ? ` · ${track.year}` : ''}
            </div>
          )}
          <div className="mnow__facts">
            <span className={'mbar__quality' + (isHiRes(track) ? ' mbar__quality--hires' : '')}>{qualityLabel(track)}</span>
            <HeartButton kind="track" id={track.id} loaded={track.favorite} />
            <TrackMenu track={track} up />
          </div>
        </div>
        <div className="mnow__side">
          <div className="mnow__tabs" role="tablist">
            <button type="button" role="tab" aria-selected={tab === 'lyrics'} className={tab === 'lyrics' ? 'on' : ''} onClick={() => onTab('lyrics')}>
              Lyrics
            </button>
            <button type="button" role="tab" aria-selected={tab === 'queue'} className={tab === 'queue' ? 'on' : ''} onClick={() => onTab('queue')}>
              Queue
            </button>
          </div>
          {tab === 'lyrics' ? (
            <LyricsView track={track} />
          ) : (
            <div className="mnow__queue">
              <QueueList />
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

/**
 * A song's words. Synced lyrics follow the song: the line being sung is bright and kept
 * in the middle, and a press on a line goes to it. Scrolling by hand is left alone for a
 * few seconds before the view follows the song again.
 */
function LyricsView({ track }: { track: MusicTrack }) {
  const lyrics = useAsync(track.lyrics ? 'music-lyrics:' + track.id : null, () => music.lyrics(track.id), [track.id], 300_000)
  const { position } = useProgress()
  const box = useRef<HTMLDivElement>(null)
  const handsOff = useRef(0)
  const lines = lyrics.data?.lines ?? []
  const synced = !!lyrics.data?.synced
  // A line lights up a moment before it is sung, as the eye reads ahead of the ear.
  const active = synced ? lyricIndexAt(lines, position * 1000 + 250) : -1
  useEffect(() => {
    const el = box.current
    if (!el || active < 0 || Date.now() < handsOff.current) return
    const line = el.querySelector<HTMLElement>(`[data-line="${active}"]`)
    if (!line) return
    el.scrollTo({ top: line.offsetTop - el.clientHeight / 2 + line.clientHeight / 2, behavior: 'smooth' })
  }, [active])
  useEffect(() => {
    box.current?.scrollTo({ top: 0 })
    handsOff.current = 0
  }, [track.id])
  const touched = () => {
    handsOff.current = Date.now() + 4000
  }
  if (!track.lyrics) return <div className="mlyrics mlyrics--none">No lyrics for this song.</div>
  if (lyrics.loading && !lyrics.data) return <div className="mlyrics mlyrics--none" aria-busy="true" />
  if (!lines.length) return <div className="mlyrics mlyrics--none">{lyrics.error ? 'The lyrics could not be read.' : 'No lyrics for this song.'}</div>
  return (
    <div className={'mlyrics' + (synced ? ' mlyrics--synced' : '')} ref={box} onWheel={touched} onTouchMove={touched}>
      {lines.map((l, i) =>
        synced ? (
          <button type="button" key={i} data-line={i} className={'mlyrics__line' + (i === active ? ' on' : '') + (i < active ? ' sung' : '')} onClick={() => player.seek(l.ms / 1000)}>
            {l.text || '♪'}
          </button>
        ) : (
          <p key={i} className="mlyrics__line">
            {l.text || ' '}
          </p>
        ),
      )}
    </div>
  )
}
