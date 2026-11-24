// The player bar along the bottom of every page while there is a queue, with what opens
// from it: the queue (a drawer at the right), "now playing" (the cover large, with the
// lyrics, the visualizer or the queue beside it) and how the sound is played. Three
// parts, as music players lay them out: the song at the left, the controls over the seek
// line in the middle, the rest at the right.
import { useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent } from 'react'
import { Link, useLocation } from 'react-router'
import { clock, codecName, isHiRes, longDuration, music, qualityLabel, type MusicTrack } from '../api/music'
import { useAsync } from '../hooks/useAsync'
import { IconClose, IconDrag, IconExpand, IconMic, IconPause, IconPlay, IconPlaylistAdd, IconQueue, IconRepeat, IconRepeatOne, IconShuffle, IconSkipNext, IconSkipPrevious, IconSpectrum, IconTune, IconVolume, IconVolumeOff } from '../components/Icons'
import { FloatingMenu, MoreButton, NameDialog, SongsMenu, type MenuAt } from './Menus'
import { ArtistLinks, Cover, Empty, Equalizer, HeartButton } from './Parts'
import { playlistsChanged } from './changes'
import { claimMediaKeys, currentTrack, player, type PlayerState } from './player'
import { musicPrefs, QUALITIES } from './prefs'
import { lyricIndexAt } from './queue'
import { useReorder } from './Reorder'
import { inAlbumRun, levelGain } from './stream'
import { toast } from './toast'
import { usePlayer, useProgress } from './usePlayer'
import { usePrefs } from './usePrefs'
import { VisualizerPanel } from './Visualizer'
import '../styles/music.css'

type Open = 'none' | 'queue' | 'now'
type NowTab = 'lyrics' | 'visualizer' | 'queue'

/** How long "now playing" and the queue take to leave (the length of their closing animation). */
export const LEAVE_MS = 240

/** Whether things may move here: not where less motion was asked for, nor where that cannot be asked. */
function animates(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function' && !window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/**
 * What is open, and what was open a moment ago and is still on its way out: a panel that
 * is closed stays drawn, leaving, until its closing animation is done.
 */
function usePanels(): { open: Open; leaving: Open; setOpen: (next: Open | ((o: Open) => Open)) => void } {
  const [panel, setPanel] = useState<{ open: Open; leaving: Open }>({ open: 'none', leaving: 'none' })
  const setOpen = (next: Open | ((o: Open) => Open)) =>
    setPanel((p) => {
      const to = typeof next === 'function' ? next(p.open) : next
      if (to === p.open) return p
      return { open: to, leaving: p.open !== 'none' && animates() ? p.open : 'none' }
    })
  useEffect(() => {
    if (panel.leaving === 'none') return
    const t = setTimeout(() => setPanel((p) => (p.leaving === 'none' ? p : { ...p, leaving: 'none' })), LEAVE_MS)
    return () => clearTimeout(t)
  }, [panel])
  return { open: panel.open, leaving: panel.leaving, setOpen }
}

/** How the song playing is sent, in the words on the bar ("Opus · 128 kbps" for a conversion). */
function streamLabel(track: MusicTrack, state: PlayerState): string {
  const c = state.stream?.converted
  if (!c) return qualityLabel(track)
  if (c.codec === 'flac') return 'FLAC · 16-bit'
  return `${codecName({ codec: c.codec, container: c.codec })} · ${c.kbps} kbps`
}

export function MusicBar() {
  const state = usePlayer()
  const track = currentTrack(state)
  const { open, leaving, setOpen } = usePanels()
  const [tab, setTab] = useState<NowTab>('lyrics')
  const [settings, setSettings] = useState<MenuAt | null>(null)
  const loc = useLocation()
  useEffect(() => claimMediaKeys(), [])
  // Going to another page closes "now playing" (a link in it was followed); the queue
  // drawer stays, like the bar.
  useEffect(() => setOpen((o) => (o === 'now' ? 'none' : o)), [loc.pathname]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && open !== 'none') setOpen('none')
      // Space plays and pauses on the music pages (and with the player open), unless
      // something that takes typing or pressing has the keyboard.
      if (e.key !== ' ' || e.repeat || !(open !== 'none' || loc.pathname.startsWith('/music'))) return
      const t = e.target as HTMLElement | null
      if (t?.closest('input, textarea, select, button, a, [contenteditable], [role="slider"], [role="tab"], [role="menu"], [role="dialog"] form')) return
      e.preventDefault()
      player.toggle()
    }
    window.addEventListener('keydown', key)
    return () => window.removeEventListener('keydown', key)
  }, [open, loc.pathname])
  if (!track) return null
  const showNow = (t: NowTab) => {
    setTab(t)
    setOpen((o) => (o === 'now' && tab === t ? 'none' : 'now'))
  }
  const first: NowTab = track.lyrics ? 'lyrics' : 'visualizer'
  const repeatLabel = state.repeat === 'off' ? 'Repeat' : state.repeat === 'all' ? 'Repeat one' : 'Repeat off'
  return (
    <>
      {(open === 'now' || leaving === 'now') && <NowPlaying track={track} state={state} tab={tab} onTab={setTab} onClose={() => setOpen('none')} closing={open !== 'now'} />}
      {(open === 'queue' || leaving === 'queue') && <QueueDrawer onClose={() => setOpen('none')} closing={open !== 'queue'} />}
      <div className={'mbar' + (open !== 'none' ? ' mbar--open' : '') + (open === 'now' ? ' mbar--now' : '')} role="region" aria-label="Music player">
        <div className="mbar__song">
          {/* The cover opens "now playing" and, whatever tab that is on, closes it: its arrow points the way it will go. */}
          <button type="button" className="mbar__cover" onClick={() => (open === 'now' ? setOpen('none') : showNow(first))} aria-label={open === 'now' ? 'Close now playing' : 'Now playing'} title={open === 'now' ? 'Close' : 'Now playing'} aria-expanded={open === 'now'}>
            <Cover art={track.art} size={60} />
            <span className="mbar__grow">
              <IconExpand size={22} />
            </span>
          </button>
          {/* On a narrow screen the names are one thing to press (their links are off there): "now playing". */}
          <div className="mbar__names" onClick={(e) => (e.target as HTMLElement).closest('a') == null && window.matchMedia?.('(max-width: 860px)').matches && showNow(first)}>
            <div className="mbar__title" title={track.title}>
              {track.title}
            </div>
            <div className="mbar__artist">
              <ArtistLinks track={track} />
            </div>
            {track.albumId && track.album && (
              <div className="mbar__album">
                <Link to={'/music/album/' + track.albumId}>{track.album}</Link>
              </div>
            )}
          </div>
          <HeartButton kind="track" id={track.id} loaded={track.favorite} className="mbar__like" />
        </div>

        <div className="mbar__middle">
          <Transport state={state} repeatLabel={repeatLabel} />
          <SeekLine />
        </div>

        <div className="mbar__rest">
          <button
            type="button"
            className={'mbar__quality' + (isHiRes(track) && !state.stream?.converted ? ' mbar__quality--hires' : '')}
            onClick={(e) => {
              const box = e.currentTarget.getBoundingClientRect()
              setSettings((s) => (s ? null : { x: box.right, y: box.top, box }))
            }}
            title="How this song is played"
            aria-haspopup="dialog"
            aria-expanded={!!settings}
          >
            {streamLabel(track, state)}
          </button>
          <button type="button" className="micon mbar__settings" onClick={(e) => setSettings((s) => (s ? null : { x: 0, y: 0, box: e.currentTarget.getBoundingClientRect() }))} aria-label="Playback settings" title="Playback settings" aria-expanded={!!settings}>
            <IconTune size={20} />
          </button>
          {track.lyrics && (
            <button type="button" className={'micon' + (open === 'now' && tab === 'lyrics' ? ' micon--on' : '')} onClick={() => showNow('lyrics')} aria-label="Lyrics" title="Lyrics">
              <IconMic size={20} />
            </button>
          )}
          <button type="button" className={'micon mbar__viz' + (open === 'now' && tab === 'visualizer' ? ' micon--on' : '')} onClick={() => showNow('visualizer')} aria-label="Visualizer" title="Visualizer">
            <IconSpectrum size={20} />
          </button>
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
      {settings && <PlaybackSettings at={settings} track={track} state={state} onClose={() => setSettings(null)} />}
    </>
  )
}

/** Shuffle, previous, play or pause, next, repeat. */
function Transport({ state, repeatLabel, large = false }: { state: PlayerState; repeatLabel: string; large?: boolean }) {
  const big = large ? 34 : 26
  return (
    <div className={'mbar__buttons' + (large ? ' mbar__buttons--large' : '')}>
      <button type="button" className={'micon' + (state.shuffle ? ' micon--on' : '')} onClick={() => player.setShuffle(!state.shuffle)} aria-pressed={state.shuffle} aria-label="Shuffle" title="Shuffle">
        <IconShuffle size={large ? 24 : 20} />
      </button>
      <button type="button" className="micon" onClick={() => player.previous()} aria-label="Previous" title="Previous">
        <IconSkipPrevious size={big} />
      </button>
      <button type="button" className="mbar__play" onClick={() => player.toggle()} aria-label={state.playing ? 'Pause' : 'Play'} title={state.playing ? 'Pause' : 'Play'} aria-busy={state.waiting}>
        {state.playing ? <IconPause size={big} /> : <IconPlay size={big} />}
      </button>
      <button type="button" className="micon" onClick={() => player.next()} aria-label="Next" title="Next">
        <IconSkipNext size={big} />
      </button>
      <button type="button" className={'micon' + (state.repeat !== 'off' ? ' micon--on' : '')} onClick={() => player.cycleRepeat()} aria-label={repeatLabel} title={repeatLabel}>
        {state.repeat === 'one' ? <IconRepeatOne size={large ? 24 : 20} /> : <IconRepeat size={large ? 24 : 20} />}
      </button>
    </div>
  )
}

/** A slider drawn as a line: dragged or pressed with the pointer, moved with the arrow keys. */
function Slider({ value, max, onChange, onCommit, label, text, step, className = '' }: { value: number; max: number; onChange?: (v: number | null) => void; onCommit: (v: number) => void; label: string; text: string; step: number; className?: string }) {
  const ref = useRef<HTMLDivElement>(null)
  const [drag, setDrag] = useState<number | null>(null)
  const shown = drag ?? value
  const at = (e: ReactPointerEvent) => {
    const box = ref.current!.getBoundingClientRect()
    return Math.max(0, Math.min(1, (e.clientX - box.left) / Math.max(1, box.width))) * max
  }
  const down = (e: ReactPointerEvent) => {
    if (!(max > 0)) return
    ref.current!.setPointerCapture?.(e.pointerId)
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
      onPointerCancel={() => {
        setDrag(null)
        onChange?.(null)
      }}
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
      <Slider value={shown * 100} max={100} step={5} label="Volume" text={Math.round(shown * 100) + '%'} onChange={(v) => v != null && player.setVolume(v / 100)} onCommit={(v) => player.setVolume(v / 100)} className="mslider--volume" />
    </div>
  )
}

// ---- how the sound is played ----

const kHz = (rate: number) => (Number.isInteger(rate / 1000) ? String(rate / 1000) : (rate / 1000).toFixed(1)) + ' kHz'

/** The settings of the sound, and what is happening to the song that plays. */
function PlaybackSettings({ at, track, state, onClose }: { at: MenuAt; track: MusicTrack; state: PlayerState; onClose: () => void }) {
  const prefs = usePrefs()
  const out = player.outputRate()
  const gain = levelGain(track, prefs.levelling, inAlbumRun(state.queue, state.queue.index))
  const joins = player.joins()
  const converted = state.stream?.converted
  return (
    <FloatingMenu at={at} onClose={onClose} wide>
      <div className="mset" role="dialog" aria-label="Playback settings">
        <h3>Streaming quality</h3>
        <div className="mset__options" role="radiogroup" aria-label="Streaming quality">
          {QUALITIES.map((q) => (
            <button key={q.id} type="button" role="radio" aria-checked={prefs.quality === q.id} className={prefs.quality === q.id ? 'on' : ''} onClick={() => musicPrefs.set({ quality: q.id })}>
              <b>{q.label}</b>
              <span>{q.hint}</span>
            </button>
          ))}
        </div>
        <label className="mset__switch">
          <span>
            <b>Volume levelling</b>
            <small>Songs play as loud as each other; an album keeps its own balance.</small>
          </span>
          <input type="checkbox" checked={prefs.levelling === 'on'} onChange={(e) => musicPrefs.set({ levelling: e.target.checked ? 'on' : 'off' })} />
        </label>
        <label className="mset__switch">
          <span>
            <b>Gapless albums</b>
            <small>Songs that run into each other on an album are joined without a gap.</small>
          </span>
          <input type="checkbox" checked={prefs.gapless} onChange={(e) => musicPrefs.set({ gapless: e.target.checked })} />
        </label>
        <h3>This song</h3>
        <dl className="mset__facts">
          <dt>File</dt>
          <dd>{qualityLabel(track)}</dd>
          <dt>Sent as</dt>
          <dd>{converted ? (converted.codec === 'flac' ? 'FLAC, 16-bit (converted)' : `${codecName({ codec: converted.codec, container: converted.codec })}, ${converted.kbps} kbit/s (converted)`) : 'The file itself'}</dd>
          {out != null && (
            <>
              <dt>Output</dt>
              <dd>
                {kHz(out)}
                {(track.sampleRate ?? 0) > out && !converted && <small>This song is {kHz(track.sampleRate!)}. Your system's sound output is set lower, so it is converted on the way out.</small>}
              </dd>
            </>
          )}
          <dt>Levelling</dt>
          <dd>{prefs.levelling === 'off' ? 'Off' : track.loudness == null && track.gainTrack == null ? 'Not measured yet' : `${gain > 0 ? '+' : ''}${gain.toFixed(1)} dB`}</dd>
          {joins.exactJoins + joins.clockJoins + joins.plainJoins > 0 && (
            <>
              <dt>Joins</dt>
              <dd>
                {joins.exactJoins} exact, {joins.clockJoins} timed, {joins.plainJoins} plain
              </dd>
            </>
          )}
        </dl>
      </div>
    </FloatingMenu>
  )
}

// ---- the queue ----

function QueueList() {
  const state = usePlayer()
  const { tracks, index } = state.queue
  const box = useRef<HTMLDivElement | null>(null)
  const reorder = useReorder(tracks.length, (from, to) => player.move(from, to), '.qrow')
  const [menu, setMenu] = useState<{ index: number; at: MenuAt } | null>(null)
  const [saving, setSaving] = useState(false)
  // Opened on the song that is playing, not at the top of a long queue.
  useLayoutEffect(() => {
    box.current?.querySelector('.qrow--current')?.scrollIntoView({ block: 'center' })
  }, [])
  const left = tracks.slice(index + 1).reduce((ms, t) => ms + t.durationMs, 0)
  const menuTrack = menu ? tracks[menu.index] : null
  return (
    <>
      <div className="mqueue__head">
        <b>Queue</b>
        <span>
          {tracks.length} {tracks.length === 1 ? 'song' : 'songs'}
          {left > 0 && ` · ${longDuration(left)} left`}
        </span>
        <button type="button" className="micon" onClick={() => setSaving(true)} aria-label="Save the queue as a playlist" title="Save as playlist">
          <IconPlaylistAdd size={20} />
        </button>
        <button type="button" className="link-brand" onClick={() => player.clear()}>
          Clear
        </button>
      </div>
      <div
        className={'mqueue__list' + (reorder.dragging != null ? ' mqueue__list--dragging' : '')}
        ref={(el) => {
          box.current = el
          reorder.listRef(el)
        }}
      >
        {tracks.map((t, i) => (
          <div
            key={t.id + ':' + i}
            className={'qrow' + (i === index ? ' qrow--current' : '') + (i < index ? ' qrow--played' : '') + (menu?.index === i ? ' qrow--open' : '') + reorder.rowClass(i)}
            onClick={() => (i === index ? player.toggle() : player.jump(i))}
            onContextMenu={(e) => {
              e.preventDefault()
              setMenu({ index: i, at: { x: e.clientX, y: e.clientY } })
            }}
          >
            <button type="button" className="qrow__grip" data-reorder-handle aria-label={'Move ' + t.title} title="Drag to move" {...reorder.handle(i)}>
              <IconDrag size={18} />
            </button>
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
            <span className="qrow__menu" onClick={(e) => e.stopPropagation()}>
              <MoreButton
                label={'More for ' + t.title}
                open={menu?.index === i}
                onClick={(e) => {
                  const b = e.currentTarget.getBoundingClientRect()
                  setMenu(menu?.index === i ? null : { index: i, at: { x: b.right, y: b.bottom, box: b } })
                }}
              />
            </span>
          </div>
        ))}
      </div>
      {menu && menuTrack && (
        <SongsMenu
          at={menu.at}
          onClose={() => setMenu(null)}
          tracks={async () => [menuTrack]}
          single={menuTrack}
          extra={[
            ...(menu.index > index + 1 ? [{ label: 'Move to next', onSelect: () => player.move(menu.index, index + 1) }] : []),
            ...(menu.index < tracks.length - 1 ? [{ label: 'Move to the end', onSelect: () => player.move(menu.index, tracks.length - 1) }] : []),
            { label: 'Remove from the queue', onSelect: () => player.remove(menu.index), danger: true },
          ]}
        />
      )}
      {saving && (
        <NameDialog
          title="Save the queue as a playlist"
          action="Save"
          onClose={() => setSaving(false)}
          onSubmit={async (name) => {
            const made = await music.createPlaylist(name, tracks.map((t) => t.id))
            playlistsChanged()
            toast.show(`Saved as ${made.name}`)
          }}
        />
      )}
    </>
  )
}

function QueueDrawer({ onClose, closing = false }: { onClose: () => void; closing?: boolean }) {
  return (
    <aside className={'mqueue' + (closing ? ' mqueue--closing' : '')} aria-label="Queue" aria-hidden={closing || undefined}>
      <button type="button" className="micon mqueue__close" onClick={onClose} aria-label="Close the queue">
        <IconClose size={20} />
      </button>
      <QueueList />
    </aside>
  )
}

// ---- now playing ----

function NowPlaying({ track, state, tab, onTab, onClose, closing = false }: { track: MusicTrack; state: PlayerState; tab: NowTab; onTab: (t: NowTab) => void; onClose: () => void; closing?: boolean }) {
  const art = music.artUrl(track.art, 640)
  const [menu, setMenu] = useState<MenuAt | null>(null)
  const repeatLabel = state.repeat === 'off' ? 'Repeat' : state.repeat === 'all' ? 'Repeat one' : 'Repeat off'
  const tabs: [NowTab, string][] = [
    ['lyrics', 'Lyrics'],
    ['visualizer', 'Visualizer'],
    ['queue', 'Queue'],
  ]
  return (
    <div className={'mnow' + (closing ? ' mnow--closing' : '')} role="dialog" aria-label="Now playing" aria-hidden={closing || undefined}>
      {art && <div className="mnow__back" key={art} style={{ backgroundImage: `url(${art})` }} />}
      <div className="mnow__shade" />
      <button type="button" className="micon mnow__close" onClick={onClose} aria-label="Close">
        <IconClose size={26} />
      </button>
      <div className="mnow__body">
        <div className="mnow__song">
          <Cover art={track.art} size={520} className="mnow__cover" key={track.art ?? track.id} />
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
            <span className={'mbar__quality' + (isHiRes(track) && !state.stream?.converted ? ' mbar__quality--hires' : '')}>{streamLabel(track, state)}</span>
            <HeartButton kind="track" id={track.id} loaded={track.favorite} />
            <MoreButton label={'More for ' + track.title} open={!!menu} onClick={(e) => setMenu(menu ? null : { x: 0, y: 0, box: e.currentTarget.getBoundingClientRect() })} />
          </div>
          {/* A narrow screen's bar has only play and next: the rest is here. */}
          <div className="mnow__controls">
            <SeekLine />
            <Transport state={state} repeatLabel={repeatLabel} large />
          </div>
        </div>
        <div className="mnow__side">
          <div className="mnow__tabs" role="tablist">
            {tabs.map(([id, label]) => (
              <button key={id} type="button" role="tab" aria-selected={tab === id} className={tab === id ? 'on' : ''} onClick={() => onTab(id)}>
                {label}
              </button>
            ))}
          </div>
          {tab === 'lyrics' && <LyricsView track={track} />}
          {tab === 'visualizer' && <VisualizerPanel />}
          {tab === 'queue' && (
            <div className="mnow__queue">
              <QueueList />
            </div>
          )}
        </div>
      </div>
      {menu && <SongsMenu at={menu} onClose={() => setMenu(null)} tracks={async () => [track]} single={track} />}
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
  if (!track.lyrics)
    return (
      <div className="mlyrics mlyrics--none">
        <Empty small icon={<IconMic size={30} />} title="No lyrics for this song">
          A file named like the song, ending in .lrc or .txt, beside it in the music folder gives it some.
        </Empty>
      </div>
    )
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
