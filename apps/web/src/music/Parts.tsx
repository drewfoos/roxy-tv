// What the music pages and the player bar are built from: covers, the song list, the
// tiles for songs, albums, artists and playlists, and the favourite mark.
import { Fragment, useEffect, useState, useSyncExternalStore, type ReactNode } from 'react'
import { Link } from 'react-router'
import { px } from '../api/client'
import { clock, music, qualityLabel, qualityMark, type FavoriteKind, type MusicAlbum, type MusicArtist, type MusicPlaylist, type MusicTrack } from '../api/music'
import { session } from '../api/session'
import { IconAlbum, IconDrag, IconHeart, IconHeartOutline, IconMusicNote, IconPause, IconPerson, IconPlay, IconPlaylist } from '../components/Icons'
import { MoreButton, SongsMenu, useMenu, type MenuItem } from './Menus'
import { player } from './player'
import { useReorder } from './Reorder'
import { toast } from './toast'
import { useCurrentTrackId, usePlayer } from './usePlayer'

// ---- favourites ----

// A favourite mark is shown in many places at once (a row, the bar, the queue). What the
// person just pressed is kept here and wins over what a list was loaded with, so every
// one of them changes together and at once.
const marks = new Map<string, boolean>()
const markListeners = new Set<() => void>()
let markVersion = 0
session.subscribe(() => {
  if (!marks.size) return
  marks.clear()
  markVersion++
  markListeners.forEach((l) => l())
})

function subscribeMarks(l: () => void) {
  markListeners.add(l)
  return () => {
    markListeners.delete(l)
  }
}

/** Whether something is a favourite: what was pressed here since, else what the server said. */
export function useFavorite(kind: FavoriteKind, id: string, loaded: boolean): [boolean, () => void] {
  useSyncExternalStore(subscribeMarks, () => markVersion)
  const key = kind + ':' + id
  const on = marks.get(key) ?? loaded
  const toggle = () => {
    const next = !on
    marks.set(key, next)
    markVersion++
    markListeners.forEach((l) => l())
    music.favorite(kind, id, next).catch(() => {
      // Not saved: back to what it was.
      marks.set(key, on)
      markVersion++
      markListeners.forEach((l) => l())
    })
  }
  return [on, toggle]
}

export function HeartButton({ kind, id, loaded, size = 20, className = '' }: { kind: FavoriteKind; id: string; loaded: boolean; size?: number; className?: string }) {
  const [on, toggle] = useFavorite(kind, id, loaded)
  return (
    <button
      type="button"
      className={'mheart' + (on ? ' mheart--on' : '') + (className ? ' ' + className : '')}
      onClick={(e) => {
        e.stopPropagation()
        toggle()
      }}
      aria-pressed={on}
      aria-label={on ? 'Remove from liked' : 'Add to liked'}
      title={on ? 'Remove from liked' : 'Add to liked'}
    >
      {on ? <IconHeart size={size} /> : <IconHeartOutline size={size} />}
    </button>
  )
}

// ---- pictures ----

/**
 * A cover: square, the picture cropped to fill it (a video still used as a cover is
 * wider than it is tall). Without a picture, or while it loads, a note (or a person, a
 * disc) on the placeholder colour.
 */
export function Cover({ art, size, round = false, kind = 'track', className = '' }: { art?: string; size: number; round?: boolean; kind?: 'track' | 'album' | 'artist' | 'playlist'; className?: string }) {
  const url = music.artUrl(art, px(size))
  const [broken, setBroken] = useState(false)
  useEffect(() => setBroken(false), [url])
  const Icon = kind === 'artist' ? IconPerson : kind === 'album' ? IconAlbum : kind === 'playlist' ? IconPlaylist : IconMusicNote
  return (
    <span className={'mcover' + (round ? ' mcover--round' : '') + (className ? ' ' + className : '')}>
      <Icon size={Math.max(16, Math.round(size * 0.4))} />
      {url && !broken && <img src={url} alt="" loading="lazy" decoding="async" draggable={false} onError={() => setBroken(true)} />}
    </span>
  )
}

/** A playlist's cover: the pictures of its first songs, four in a square when it has four. */
export function PlaylistCover({ art, size, className = '' }: { art: string[]; size: number; className?: string }) {
  if (art.length < 4) return <Cover art={art[0]} size={size} kind="playlist" className={className} />
  return (
    <span className={'mcover mcover--four' + (className ? ' ' + className : '')}>
      {art.slice(0, 4).map((hash, i) => (
        <img key={i} src={music.artUrl(hash, px(size / 2)) ?? undefined} alt="" loading="lazy" decoding="async" draggable={false} />
      ))}
    </span>
  )
}

/** The artists a song credits, each a link to that artist's page. */
export function ArtistLinks({ track }: { track: MusicTrack }) {
  const artists = track.artists?.length ? track.artists : [{ id: track.artistId, name: track.artist }]
  return (
    <>
      {artists.map((a, i) => (
        <Fragment key={a.id}>
          {i > 0 && ', '}
          <Link to={'/music/artist/' + a.id} onClick={(e) => e.stopPropagation()}>
            {a.name}
          </Link>
        </Fragment>
      ))}
    </>
  )
}

// ---- the song list ----

/** The bars that move beside the song that is playing. */
export function Equalizer({ moving }: { moving: boolean }) {
  return (
    <span className={'meq' + (moving ? ' meq--on' : '')} aria-hidden="true">
      <i />
      <i />
      <i />
    </span>
  )
}

export interface TrackListProps {
  tracks: MusicTrack[]
  /** "index": 1, 2, 3 down the list; "track": the song's number on its album. */
  numbers?: 'index' | 'track'
  /** Leave the album column out (an album's own page). */
  hideAlbum?: boolean
  /** Leave the picture out (an album's own page: every row would show the same one). */
  hideCover?: boolean
  /** Leave out the line of artists under a title when it only repeats this name (an album's own artist). */
  hideArtist?: string
  /** Show how many times each song was played. */
  plays?: boolean
  /** The names of the columns over the list. */
  header?: boolean
  /** Rows can be dragged to another place: told where one was dropped. */
  onMove?: (from: number, to: number) => void
  /** More entries for a row's menu ("Remove from playlist"). */
  rowMenu?: (track: MusicTrack, index: number) => MenuItem[]
  /** A row's key when a song can be in the list twice (a playlist's entry id). */
  rowKey?: (track: MusicTrack, index: number) => string | number
}

/**
 * Songs as rows. A press on a row plays the list from that song; the names of its
 * artists and its album are links; the heart and the menu (the ⋮, or the right mouse
 * button anywhere on the row) act on that one song.
 */
export function TrackList({ tracks, numbers = 'index', hideAlbum = false, hideCover = false, hideArtist, plays = false, header = false, onMove, rowMenu, rowKey }: TrackListProps) {
  const currentId = useCurrentTrackId()
  const playing = usePlayer().playing
  const discs = new Set(tracks.map((t) => t.disc ?? 0)).size
  // The line of column names is a row to look at, not one of the rows that move.
  const reorder = useReorder(tracks.length, onMove ?? (() => undefined), '.trow:not(.trow--head)')
  const [menu, setMenu] = useState<{ index: number; at: { x: number; y: number; box?: DOMRect } } | null>(null)
  const cls = 'tlist' + (hideAlbum ? ' tlist--noalbum' : '') + (hideCover ? ' tlist--nocover' : '') + (onMove ? ' tlist--move' : '') + (reorder.dragging != null ? ' tlist--dragging' : '')
  const menuTrack = menu ? tracks[menu.index] : null
  return (
    <div className={cls} role="list" ref={reorder.listRef}>
      {header && (
        <div className="trow trow--head" aria-hidden="true">
          {onMove && <span />}
          <span className="trow__n">#</span>
          {!hideCover && <span />}
          <span>Title</span>
          {!hideAlbum && <span className="trow__album">Album</span>}
          <span className="trow__mark" />
          <span />
          <span className="trow__time">Time</span>
          <span />
        </div>
      )}
      {tracks.map((t, i) => {
        const current = t.id === currentId
        const newDisc = numbers === 'track' && discs > 1 && (i === 0 || (tracks[i - 1].disc ?? 0) !== (t.disc ?? 0))
        const open = menu?.index === i
        return (
          <Fragment key={rowKey ? rowKey(t, i) : t.id}>
            {newDisc && <div className="tlist__disc">Disc {t.disc || 1}</div>}
            <div
              role="listitem"
              className={'trow' + (current ? ' trow--current' : '') + (open ? ' trow--open' : '') + reorder.rowClass(i)}
              onClick={() => (current ? player.toggle() : player.playTracks(tracks, i, { shuffle: false }))}
              onContextMenu={(e) => {
                e.preventDefault()
                setMenu({ index: i, at: { x: e.clientX, y: e.clientY } })
              }}
            >
              {onMove && (
                <button type="button" className="trow__grip" data-reorder-handle aria-label={'Move ' + t.title} title="Drag to move" {...reorder.handle(i)}>
                  <IconDrag size={18} />
                </button>
              )}
              <span className="trow__n">
                {current ? <Equalizer moving={playing} /> : <span className="trow__num">{numbers === 'track' ? t.track || i + 1 : i + 1}</span>}
                <span className="trow__play">{current && playing ? <IconPause size={20} /> : <IconPlay size={20} />}</span>
              </span>
              {!hideCover && <Cover art={t.art} size={40} />}
              <span className="trow__main">
                <span className="trow__title">{t.title}</span>
                {!(hideArtist && t.artist === hideArtist) && (
                  <span className="trow__artist">
                    <ArtistLinks track={t} />
                  </span>
                )}
              </span>
              {!hideAlbum && (
                <span className="trow__album">
                  {t.albumId && t.album ? (
                    <Link to={'/music/album/' + t.albumId} onClick={(e) => e.stopPropagation()}>
                      {t.album}
                    </Link>
                  ) : null}
                </span>
              )}
              <span className="trow__mark" title={qualityLabel(t)}>
                {plays && t.playCount > 0 ? `${t.playCount} ${t.playCount === 1 ? 'play' : 'plays'}` : qualityMark(t)}
              </span>
              <HeartButton kind="track" id={t.id} loaded={t.favorite} size={18} className="trow__heart" />
              <span className="trow__time">{clock(t.durationMs)}</span>
              <span className="trow__menu" onClick={(e) => e.stopPropagation()}>
                <MoreButton
                  label={'More for ' + t.title}
                  open={open}
                  onClick={(e) => {
                    const box = e.currentTarget.getBoundingClientRect()
                    setMenu(open ? null : { index: i, at: { x: box.right, y: box.bottom, box } })
                  }}
                />
              </span>
            </div>
          </Fragment>
        )
      })}
      {menu && menuTrack && <SongsMenu at={menu.at} onClose={() => setMenu(null)} tracks={async () => [menuTrack]} single={menuTrack} extra={rowMenu?.(menuTrack, menu.index)} />}
    </div>
  )
}

// ---- tiles ----

/** A song as a square tile: a press plays `list` from it. */
export function TrackTile({ track, list, index }: { track: MusicTrack; list: MusicTrack[]; index: number }) {
  const currentId = useCurrentTrackId()
  const playing = usePlayer().playing
  const current = currentId === track.id
  const menu = useMenu()
  return (
    <div className={'mtile' + (current ? ' mtile--current' : '') + (menu.at ? ' mtile--open' : '')} onContextMenu={menu.context}>
      <div className="mtile__art">
        <button type="button" className="mtile__hit" onClick={() => (current ? player.toggle() : player.playTracks(list, index, { shuffle: false }))} aria-label={(current && playing ? 'Pause ' : 'Play ') + track.title}>
          <Cover art={track.art} size={240} />
        </button>
        <span className="mtile__play" aria-hidden="true">
          {current && playing ? <IconPause size={26} /> : <IconPlay size={26} />}
        </span>
        <MoreButton label={'More for ' + track.title} open={!!menu.at} onClick={menu.toggle} className="mtile__more" />
      </div>
      <div className="mtile__name" title={track.title}>
        {track.title}
      </div>
      <div className="mtile__sub">
        <ArtistLinks track={track} />
      </div>
      {menu.at && <SongsMenu at={menu.at} onClose={menu.close} tracks={async () => [track]} single={track} />}
    </div>
  )
}

/** Plays what a tile stands for, fetched when its button is pressed. */
function useTilePlay(load: () => Promise<MusicTrack[]>) {
  const [busy, setBusy] = useState(false)
  const play = async () => {
    if (busy) return
    setBusy(true)
    try {
      const tracks = await load()
      if (tracks.length) player.playTracks(tracks, 0, { shuffle: false })
    } catch {
      toast.show('That could not be read from the server')
    } finally {
      setBusy(false)
    }
  }
  return { busy, play }
}

/** A tile that opens a page, with a button on its picture that plays it and a menu. */
function PageTile({ to, name, sub, cover, load, round = false, label }: { to: string; name: string; sub: ReactNode; cover: ReactNode; load: () => Promise<MusicTrack[]>; round?: boolean; label: string }) {
  const { busy, play } = useTilePlay(load)
  const menu = useMenu()
  return (
    <div className={'mtile' + (round ? ' mtile--artist' : '') + (menu.at ? ' mtile--open' : '')} onContextMenu={menu.context}>
      <div className="mtile__art">
        <Link to={to} className="mtile__hit" aria-label={name}>
          {cover}
        </Link>
        <button type="button" className="mtile__play mtile__play--button" onClick={() => void play()} aria-label={'Play ' + label} aria-busy={busy}>
          <IconPlay size={26} />
        </button>
        <MoreButton label={'More for ' + label} open={!!menu.at} onClick={menu.toggle} className="mtile__more" />
      </div>
      <Link to={to} className="mtile__name" title={name}>
        {name}
      </Link>
      <div className="mtile__sub">{sub}</div>
      {menu.at && <SongsMenu at={menu.at} onClose={menu.close} tracks={load} play />}
    </div>
  )
}

export const albumTracks = (id: string) => () => music.album(id).then((r) => r.tracks)
export const artistTracks = (id: string) => () => music.artist(id).then((r) => r.tracks)
export const playlistTracks = (id: string) => () => music.playlist(id).then((r) => r.tracks)

export function AlbumTile({ album, sub }: { album: MusicAlbum; sub?: ReactNode }) {
  return (
    <PageTile
      to={'/music/album/' + album.id}
      name={album.title}
      label={album.title}
      cover={<Cover art={album.art} size={240} kind="album" />}
      load={albumTracks(album.id)}
      sub={sub ?? <Link to={'/music/artist/' + album.artistId}>{album.artist}</Link>}
    />
  )
}

export function ArtistTile({ artist }: { artist: MusicArtist }) {
  return (
    <PageTile
      round
      to={'/music/artist/' + artist.id}
      name={artist.name}
      label={artist.name}
      cover={<Cover art={artist.art} size={240} round kind="artist" />}
      load={artistTracks(artist.id)}
      sub={`${artist.trackCount} ${artist.trackCount === 1 ? 'song' : 'songs'}`}
    />
  )
}

export function PlaylistTile({ playlist }: { playlist: MusicPlaylist }) {
  return (
    <PageTile
      to={'/music/playlist/' + playlist.id}
      name={playlist.name}
      label={playlist.name}
      cover={<PlaylistCover art={playlist.art} size={240} />}
      load={playlistTracks(playlist.id)}
      sub={`${playlist.trackCount} ${playlist.trackCount === 1 ? 'song' : 'songs'}`}
    />
  )
}

/**
 * A page (or a panel) with nothing in it yet: what would be here, drawn large and quietly
 * alive, what to do about it in a line, and the button that does it where there is one.
 */
export function Empty({ icon, title, children, action, small = false }: { icon: ReactNode; title: string; children?: ReactNode; action?: ReactNode; small?: boolean }) {
  return (
    <div className={'mempty' + (small ? ' mempty--small' : '')}>
      <div className="mempty__art" aria-hidden="true">
        <span className="mempty__ring" />
        <span className="mempty__ring" />
        <span className="mempty__ring" />
        <span className="mempty__icon">{icon}</span>
      </div>
      <b className="mempty__title">{title}</b>
      {children && <p className="mempty__text">{children}</p>}
      {action && <div className="mempty__action">{action}</div>}
    </div>
  )
}

/** Tiles the shape of the ones to come, while a page loads. */
export function TileSkeleton({ count = 12, round = false }: { count?: number; round?: boolean }) {
  return (
    <div className="mgrid" aria-hidden="true">
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className="mtile">
          <div className="bone" style={{ aspectRatio: '1 / 1', borderRadius: round ? '50%' : 6 }} />
          <div className="bone bone--line" style={{ width: '70%', marginTop: 12 }} />
        </div>
      ))}
    </div>
  )
}

/** Rows the shape of songs, while a list loads. */
export function ListSkeleton({ count = 10 }: { count?: number }) {
  return (
    <div className="tlist" aria-hidden="true">
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className="trow trow--bone">
          <span className="trow__n" />
          <span className="bone" style={{ width: 40, height: 40 }} />
          <span className="trow__main">
            <span className="bone bone--line" style={{ width: `${40 + ((i * 17) % 35)}%` }} />
          </span>
        </div>
      ))}
    </div>
  )
}
