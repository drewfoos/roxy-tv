// The menus of the music pages: what can be done with a song, an album, an artist or a
// playlist (play it next, put it at the end of the queue, add it to a playlist, go to
// where it comes from). Opened from a ⋮ button or with the right mouse button.
import { useEffect, useLayoutEffect, useRef, useState, type FormEvent, type MouseEvent as ReactMouseEvent, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate } from 'react-router'
import { music, type MusicPlaylist, type MusicTrack } from '../api/music'
import { IconAdd, IconBack, IconMore } from '../components/Icons'
import { playlistsChanged } from './changes'
import { player } from './player'
import { toast } from './toast'

export type MenuItem = { label: string; onSelect: () => void; danger?: boolean } | 'divider'

/** Where a menu opens: under a button, or at the pointer. */
export interface MenuAt {
  x: number
  y: number
  /** The button it belongs to: the menu opens under it (or over it when there is no room). */
  box?: DOMRect
}

/**
 * A menu floating over the page, kept inside the window and clear of the player bar,
 * closed by a press elsewhere or Escape. It is placed again whenever its size changes
 * (the playlists arriving, a line added to the settings).
 */
export function FloatingMenu({ at, onClose, wide = false, children }: { at: MenuAt; onClose: () => void; wide?: boolean; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState<{ left: number; top: number } | null>(null)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const place = () => {
      // Its laid-out size: while it opens it is drawn a little smaller than it is.
      const width = el.offsetWidth
      const height = el.offsetHeight
      const pad = 8
      // Over the page it stays above the player bar; opened from the bar it goes up from it.
      const bar = document.querySelector('.mbar')?.getBoundingClientRect()
      const floor = bar && !(at.box && at.box.top >= bar.top) && (at.box?.top ?? at.y) < bar.top ? bar.top : window.innerHeight
      let left = at.box ? at.box.right - width : at.x
      let top = at.box ? at.box.bottom + 4 : at.y
      if (top + height > floor - pad) top = at.box ? at.box.top - height - 4 : at.y - height
      left = Math.max(pad, Math.min(left, window.innerWidth - width - pad))
      top = Math.max(pad, Math.min(top, floor - height - pad))
      setPos((was) => (was && was.left === left && was.top === top ? was : { left, top }))
    }
    place()
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(place)
    ro.observe(el)
    return () => ro.disconnect()
  }, [at])
  useEffect(() => {
    const away = (e: Event) => {
      const t = e.target as Element | null
      // The button that opened it closes it by its own press (it would open it again here).
      if (ref.current?.contains(t) || t?.closest?.('[aria-expanded="true"]')) return
      onClose()
    }
    const key = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      e.stopPropagation()
      onClose()
    }
    document.addEventListener('pointerdown', away, true)
    document.addEventListener('keydown', key, true)
    window.addEventListener('resize', onClose)
    window.addEventListener('blur', onClose)
    return () => {
      document.removeEventListener('pointerdown', away, true)
      document.removeEventListener('keydown', key, true)
      window.removeEventListener('resize', onClose)
      window.removeEventListener('blur', onClose)
    }
  }, [onClose])
  return createPortal(
    <div ref={ref} className={'menu menu--float mmenu' + (wide ? ' mmenu--wide' : '')} role="menu" style={pos ? { left: pos.left, top: pos.top } : { left: 0, top: 0, visibility: 'hidden' }} onClick={(e) => e.stopPropagation()} onContextMenu={(e) => e.preventDefault()}>
      {children}
    </div>,
    document.body,
  )
}

function Items({ items, onClose }: { items: MenuItem[]; onClose: () => void }) {
  return (
    <>
      {items.map((it, i) =>
        it === 'divider' ? (
          <div key={i} className="mmenu__line" role="separator" />
        ) : (
          <button
            key={i}
            role="menuitem"
            className={it.danger ? 'mmenu__danger' : ''}
            onClick={() => {
              onClose()
              it.onSelect()
            }}
          >
            {it.label}
          </button>
        ),
      )}
    </>
  )
}

const plural = (n: number) => `${n} ${n === 1 ? 'song' : 'songs'}`

/** The list of the profile's playlists to add songs to, with "New playlist" at the top. */
function PlaylistPicker({ tracks, onBack, onNew, onClose }: { tracks: () => Promise<MusicTrack[]>; onBack: () => void; onNew: () => void; onClose: () => void }) {
  const [lists, setLists] = useState<MusicPlaylist[] | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    let live = true
    music
      .playlists()
      .then((r) => live && setLists(r.playlists))
      .catch(() => live && setFailed(true))
    return () => {
      live = false
    }
  }, [])
  const add = async (list: MusicPlaylist) => {
    onClose()
    try {
      const songs = await tracks()
      if (!songs.length) return
      await music.addToPlaylist(list.id, songs.map((t) => t.id))
      playlistsChanged()
      toast.show(`Added ${plural(songs.length)} to ${list.name}`)
    } catch {
      toast.show('Could not add to ' + list.name)
    }
  }
  return (
    <>
      <button role="menuitem" className="mmenu__back" onClick={onBack}>
        <IconBack size={18} /> Add to playlist
      </button>
      <div className="mmenu__line" role="separator" />
      <button role="menuitem" onClick={onNew}>
        <IconAdd size={18} /> New playlist
      </button>
      <div className="mmenu__scroll">
        {lists?.map((l) => (
          <button key={l.id} role="menuitem" onClick={() => void add(l)}>
            <span className="mmenu__name">{l.name}</span>
            <span className="mmenu__count">{l.trackCount}</span>
          </button>
        ))}
        {failed && <div className="mmenu__note">The playlists could not be read.</div>}
        {lists?.length === 0 && <div className="mmenu__note">No playlists yet.</div>}
      </div>
    </>
  )
}

/**
 * A menu for songs: one song (a row) or many (an album, an artist, a playlist: `tracks`
 * then fetches them when an entry is chosen). `extra` are entries of the place it is
 * opened in ("Remove from queue").
 */
export function SongsMenu({
  at,
  onClose,
  tracks,
  single,
  extra = [],
  play = false,
}: {
  at: MenuAt
  onClose: () => void
  tracks: () => Promise<MusicTrack[]>
  /** The one song the menu is for (gives "Go to artist" and "Go to album"). */
  single?: MusicTrack
  extra?: MenuItem[]
  /** Offer Play and Shuffle too (a tile has no row to press). */
  play?: boolean
}) {
  const nav = useNavigate()
  const [picking, setPicking] = useState(false)
  const [naming, setNaming] = useState(false)
  const withTracks = (fn: (t: MusicTrack[]) => void, said?: (n: number) => string) => async () => {
    try {
      const t = await tracks()
      if (!t.length) return
      fn(t)
      if (said) toast.show(said(t.length))
    } catch {
      toast.show('That could not be read from the server')
    }
  }
  const items: MenuItem[] = []
  if (play) {
    items.push({ label: 'Play', onSelect: withTracks((t) => player.playTracks(t, 0, { shuffle: false })) })
    items.push({ label: 'Shuffle', onSelect: withTracks((t) => player.playTracks(t, Math.floor(Math.random() * t.length), { shuffle: true })) })
  }
  items.push({ label: 'Play next', onSelect: withTracks((t) => player.playNext(t), (n) => (n === 1 ? 'Playing next' : `${plural(n)} playing next`)) })
  items.push({ label: 'Add to queue', onSelect: withTracks((t) => player.addToQueue(t), (n) => (n === 1 ? 'Added to the queue' : `${plural(n)} added to the queue`)) })
  if (single) {
    items.push('divider')
    items.push({ label: 'Go to artist', onSelect: () => nav('/music/artist/' + single.artistId) })
    if (single.albumId) items.push({ label: 'Go to album', onSelect: () => nav('/music/album/' + single.albumId) })
  }
  if (extra.length) items.push('divider', ...extra)
  // The window that asks for a new playlist's name stands in for the menu (inside it, a
  // press in the window would count as a press outside the menu and close both).
  if (naming)
    return (
      <NameDialog
        title="New playlist"
        action="Create"
        onClose={onClose}
        onSubmit={async (name) => {
          const songs = await tracks()
          const made = await music.createPlaylist(name, songs.map((t) => t.id))
          playlistsChanged()
          toast.show(songs.length ? `Added ${plural(songs.length)} to ${made.name}` : `Made ${made.name}`)
        }}
      />
    )
  return (
    <FloatingMenu at={at} onClose={onClose}>
      {picking ? (
        <PlaylistPicker tracks={tracks} onBack={() => setPicking(false)} onNew={() => setNaming(true)} onClose={onClose} />
      ) : (
        <>
          <Items items={items.slice(0, play ? 4 : 2)} onClose={onClose} />
          <button role="menuitem" onClick={() => setPicking(true)}>
            Add to playlist…
          </button>
          <Items items={items.slice(play ? 4 : 2)} onClose={onClose} />
        </>
      )}
    </FloatingMenu>
  )
}

/** Opens a menu from a ⋮ button, or at the pointer for the right mouse button. */
export function useMenu() {
  const [at, setAt] = useState<MenuAt | null>(null)
  return {
    at,
    close: () => setAt(null),
    /** For a button's onClick. */
    toggle: (e: ReactMouseEvent) => {
      e.stopPropagation()
      const box = (e.currentTarget as HTMLElement).getBoundingClientRect()
      setAt((was) => (was ? null : { x: box.right, y: box.bottom, box }))
    },
    /** For onContextMenu. */
    context: (e: ReactMouseEvent) => {
      e.preventDefault()
      e.stopPropagation()
      setAt({ x: e.clientX, y: e.clientY })
    },
  }
}

/** The ⋮ of a song, an album, an artist or a playlist. */
export function MoreButton({ label, onClick, open, className = '' }: { label: string; onClick: (e: ReactMouseEvent) => void; open: boolean; className?: string }) {
  return (
    <button type="button" className={'micon' + (className ? ' ' + className : '')} onClick={onClick} aria-label={label} aria-haspopup="menu" aria-expanded={open} title="More">
      <IconMore size={20} />
    </button>
  )
}

/** A small window asking for a name (a new playlist, a new name for one). */
export function NameDialog({ title, action, initial = '', onSubmit, onClose }: { title: string; action: string; initial?: string; onSubmit: (name: string) => Promise<unknown>; onClose: () => void }) {
  const [name, setName] = useState(initial)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const input = useRef<HTMLInputElement>(null)
  useEffect(() => {
    input.current?.focus()
    input.current?.select()
    const key = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      e.stopPropagation()
      onClose()
    }
    document.addEventListener('keydown', key, true)
    return () => document.removeEventListener('keydown', key, true)
  }, [onClose])
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const clean = name.trim()
    if (!clean || busy) return
    setBusy(true)
    setError(null)
    try {
      await onSubmit(clean)
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'That did not work.')
      setBusy(false)
    }
  }
  return createPortal(
    <div className="mdialog__scrim" onPointerDown={(e) => e.target === e.currentTarget && onClose()} onClick={(e) => e.stopPropagation()}>
      <form className="mdialog" onSubmit={submit} role="dialog" aria-modal="true" aria-label={title}>
        <h2>{title}</h2>
        <input ref={input} value={name} onChange={(e) => setName(e.target.value)} maxLength={120} placeholder="Name" aria-label="Name" />
        {error && <div className="mdialog__error">{error}</div>}
        <div className="mdialog__actions">
          <button type="button" className="btn btn--grey" onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className="btn btn--brand" disabled={!name.trim() || busy} aria-busy={busy}>
            {action}
          </button>
        </div>
      </form>
    </div>,
    document.body,
  )
}
