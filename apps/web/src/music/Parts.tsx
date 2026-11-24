// What the music pages and the player bar are built from: covers, the song list, the
// tiles for albums and artists, and the favourite mark.
import { Fragment, useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from 'react'
import { Link, useNavigate } from 'react-router'
import { px } from '../api/client'
import { clock, music, qualityLabel, qualityMark, type FavoriteKind, type MusicAlbum, type MusicArtist, type MusicTrack } from '../api/music'
import { session } from '../api/session'
import { IconAlbum, IconHeart, IconHeartOutline, IconMore, IconMusicNote, IconPause, IconPerson, IconPlay } from '../components/Icons'
import { player } from './player'
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
export function Cover({ art, size, round = false, kind = 'track', className = '' }: { art?: string; size: number; round?: boolean; kind?: 'track' | 'album' | 'artist'; className?: string }) {
  const url = music.artUrl(art, px(size))
  const [broken, setBroken] = useState(false)
  useEffect(() => setBroken(false), [url])
  const Icon = kind === 'artist' ? IconPerson : kind === 'album' ? IconAlbum : IconMusicNote
  return (
    <span className={'mcover' + (round ? ' mcover--round' : '') + (className ? ' ' + className : '')}>
      <Icon size={Math.max(16, Math.round(size * 0.4))} />
      {url && !broken && <img src={url} alt="" loading="lazy" decoding="async" onError={() => setBroken(true)} />}
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
  /** Show how many times each song was played. */
  plays?: boolean
}

/**
 * Songs as rows. A press on a row plays the list from that song; the names of its
 * artists and its album are links; the heart and the menu act on that one song.
 */
export function TrackList({ tracks, numbers = 'index', hideAlbum = false, hideCover = false, plays = false }: TrackListProps) {
  const currentId = useCurrentTrackId()
  const playing = usePlayer().playing
  const discs = new Set(tracks.map((t) => t.disc ?? 0)).size
  return (
    <div className={'tlist' + (hideAlbum ? ' tlist--noalbum' : '') + (hideCover ? ' tlist--nocover' : '')} role="list">
      {tracks.map((t, i) => {
        const current = t.id === currentId
        const newDisc = numbers === 'track' && discs > 1 && (i === 0 || (tracks[i - 1].disc ?? 0) !== (t.disc ?? 0))
        return (
          <Fragment key={t.id}>
            {newDisc && <div className="tlist__disc">Disc {t.disc || 1}</div>}
            <div
              role="listitem"
              className={'trow' + (current ? ' trow--current' : '')}
              onClick={() => (current ? player.toggle() : player.playTracks(tracks, i, { shuffle: false }))}
            >
              <span className="trow__n">
                {current ? (
                  <Equalizer moving={playing} />
                ) : (
                  <span className="trow__num">{numbers === 'track' ? t.track || i + 1 : i + 1}</span>
                )}
                <span className="trow__play">{current && playing ? <IconPause size={20} /> : <IconPlay size={20} />}</span>
              </span>
              {!hideCover && <Cover art={t.art} size={40} />}
              <span className="trow__main">
                <span className="trow__title">{t.title}</span>
                <span className="trow__artist">
                  <ArtistLinks track={t} />
                </span>
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
              <TrackMenu track={t} />
            </div>
          </Fragment>
        )
      })}
    </div>
  )
}

/** The ⋮ on a song: put it in the queue, or go to where it comes from. */
export function TrackMenu({ track, up = false }: { track: MusicTrack; up?: boolean }) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLSpanElement>(null)
  const nav = useNavigate()
  useEffect(() => {
    if (!open) return
    const away = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false)
    }
    const key = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', away)
    document.addEventListener('keydown', key)
    return () => {
      document.removeEventListener('mousedown', away)
      document.removeEventListener('keydown', key)
    }
  }, [open])
  const act = (fn: () => void) => () => {
    setOpen(false)
    fn()
  }
  return (
    <span className="trow__menu" ref={ref} onClick={(e) => e.stopPropagation()}>
      <button type="button" className="micon" onClick={() => setOpen((o) => !o)} aria-label={'More for ' + track.title} aria-expanded={open}>
        <IconMore size={20} />
      </button>
      {open && (
        <div className={'menu' + (up ? ' menu--up' : '')} role="menu">
          <button role="menuitem" onClick={act(() => player.playNext([track]))}>
            Play next
          </button>
          <button role="menuitem" onClick={act(() => player.addToQueue([track]))}>
            Add to queue
          </button>
          <button role="menuitem" onClick={act(() => nav('/music/artist/' + track.artistId))}>
            Go to artist
          </button>
          {track.albumId && (
            <button role="menuitem" onClick={act(() => nav('/music/album/' + track.albumId))}>
              Go to album
            </button>
          )}
        </div>
      )}
    </span>
  )
}

// ---- tiles ----

/** A song as a square tile: a press plays `list` from it. */
export function TrackTile({ track, list, index }: { track: MusicTrack; list: MusicTrack[]; index: number }) {
  const currentId = useCurrentTrackId()
  const playing = usePlayer().playing
  const current = currentId === track.id
  return (
    <div className={'mtile' + (current ? ' mtile--current' : '')}>
      <button
        type="button"
        className="mtile__art"
        onClick={() => (current ? player.toggle() : player.playTracks(list, index, { shuffle: false }))}
        aria-label={(current && playing ? 'Pause ' : 'Play ') + track.title}
      >
        <Cover art={track.art} size={220} />
        <span className="mtile__play">{current && playing ? <IconPause size={26} /> : <IconPlay size={26} />}</span>
      </button>
      <div className="mtile__name">{track.title}</div>
      <div className="mtile__sub">
        <ArtistLinks track={track} />
      </div>
    </div>
  )
}

export function AlbumTile({ album, sub }: { album: MusicAlbum; sub?: ReactNode }) {
  return (
    <div className="mtile">
      <Link to={'/music/album/' + album.id} className="mtile__art" aria-label={album.title}>
        <Cover art={album.art} size={220} kind="album" />
      </Link>
      <Link to={'/music/album/' + album.id} className="mtile__name">
        {album.title}
      </Link>
      <div className="mtile__sub">{sub ?? <Link to={'/music/artist/' + album.artistId}>{album.artist}</Link>}</div>
    </div>
  )
}

export function ArtistTile({ artist }: { artist: MusicArtist }) {
  return (
    <Link to={'/music/artist/' + artist.id} className="mtile mtile--artist">
      <span className="mtile__art">
        <Cover art={artist.art} size={220} round kind="artist" />
      </span>
      <span className="mtile__name">{artist.name}</span>
      <span className="mtile__sub">
        {artist.trackCount} {artist.trackCount === 1 ? 'song' : 'songs'}
      </span>
    </Link>
  )
}

/** Tiles the shape of the ones to come, while a page loads. */
export function TileSkeleton({ count = 12, round = false }: { count?: number; round?: boolean }) {
  return (
    <div className="mgrid" aria-hidden="true">
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className="mtile">
          <div className="bone" style={{ aspectRatio: '1 / 1', borderRadius: round ? '50%' : 4 }} />
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
