// The Music pages: the library (home, songs, artists, albums, liked), an artist, an album.
// Songs come first: most libraries are single songs, so a song is listed by itself and
// an album appears once two of its songs are there.
import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link, useParams, useSearchParams } from 'react-router'
import { clock, longDuration, music, type MusicAlbum, type MusicHome, type MusicTrack, type TrackSort } from '../api/music'
import { useAsync, type AsyncState } from '../hooks/useAsync'
import { useSession } from '../app/useSession'
import { TopBar } from '../components/TopBar'
import { Footer } from '../components/Footer'
import { LoadError } from '../components/Connection'
import { IconClose, IconPlay, IconSearch, IconShuffle } from '../components/Icons'
import { AlbumTile, ArtistTile, Cover, HeartButton, ListSkeleton, TileSkeleton, TrackList, TrackTile } from '../music/Parts'
import { player } from '../music/player'
import '../styles/music.css'

type Tab = 'home' | 'songs' | 'artists' | 'albums' | 'liked'
const TABS: { id: Tab; label: string }[] = [
  { id: 'home', label: 'Home' },
  { id: 'songs', label: 'Songs' },
  { id: 'artists', label: 'Artists' },
  { id: 'albums', label: 'Albums' },
  { id: 'liked', label: 'Liked' },
]

const plural = (n: number, one: string, many = one + 's') => `${n.toLocaleString()} ${n === 1 ? one : many}`

function Page({ children }: { children: ReactNode }) {
  return (
    <div className="page page--tab page--phone">
      <TopBar phone="tab" />
      <div className="music">{children}</div>
      <Footer />
    </div>
  )
}

/** /music: the library, by tab; a search across all of it replaces the tabs while there is one. */
export function Music() {
  const [params, setParams] = useSearchParams()
  const tab = (TABS.find((t) => t.id === params.get('tab'))?.id ?? 'home') as Tab
  const query = params.get('q') ?? ''
  const [typed, setTyped] = useState(query)
  useEffect(() => setTyped(query), [query])
  const change = (set: (p: URLSearchParams) => void) => {
    const next = new URLSearchParams(params)
    set(next)
    setParams(next, { replace: true })
  }
  // The search follows the typing, a moment behind it.
  useEffect(() => {
    if (typed.trim() === query) return
    const t = setTimeout(() => change((p) => (typed.trim() ? p.set('q', typed.trim()) : p.delete('q'))), 250)
    return () => clearTimeout(t)
  }, [typed]) // eslint-disable-line react-hooks/exhaustive-deps
  const home = useAsync('music-home', () => music.home())
  const empty = home.data != null && home.data.counts.tracks === 0
  return (
    <Page>
      <div className="music__head">
        <h1>Music</h1>
        <span className="music__spacer" />
        {!empty && (
          <label className="music__search">
            <IconSearch size={20} />
            <input value={typed} onChange={(e) => setTyped(e.target.value)} placeholder="Search songs, artists, albums" aria-label="Search music" />
            {typed && (
              <button type="button" onClick={() => setTyped('')} aria-label="Clear the search">
                <IconClose size={18} />
              </button>
            )}
          </label>
        )}
      </div>
      {empty ? (
        <NoMusic scanning={!!home.data?.scanning} />
      ) : query ? (
        <SearchResults query={query} />
      ) : (
        <>
          <div className="mtabs" role="tablist">
            {TABS.map((t) => (
              <button key={t.id} role="tab" aria-selected={tab === t.id} className={tab === t.id ? 'on' : ''} onClick={() => change((p) => (t.id === 'home' ? p.delete('tab') : p.set('tab', t.id)))}>
                {t.label}
              </button>
            ))}
          </div>
          {tab === 'home' && <HomeTab home={home} />}
          {tab === 'songs' && <SongsTab />}
          {tab === 'artists' && <ArtistsTab />}
          {tab === 'albums' && <AlbumsTab />}
          {tab === 'liked' && <LikedTab />}
        </>
      )}
    </Page>
  )
}

function NoMusic({ scanning }: { scanning: boolean }) {
  const s = useSession()
  return (
    <div className="music__empty">
      <b>{scanning ? 'Reading the music folder…' : 'No music yet'}</b>
      {scanning ? (
        'Songs appear here as they are read.'
      ) : s?.isAdmin ? (
        <>
          Add a music folder in <Link to="/account/library" className="link-brand">Library settings</Link>: its songs are read from their tags.
        </>
      ) : (
        'This server has no music folder yet.'
      )}
    </div>
  )
}

/** Every song, to play all of it at once. The library's size is known; lists are capped at 1000 a request. */
async function allTracks(sort: TrackSort = 'title'): Promise<MusicTrack[]> {
  const out: MusicTrack[] = []
  for (;;) {
    const r = await music.tracks({ sort, limit: 1000, offset: out.length })
    out.push(...r.tracks)
    if (!r.tracks.length || out.length >= r.total) return out
  }
}

function ShuffleAll({ label = 'Shuffle all' }: { label?: string }) {
  const [busy, setBusy] = useState(false)
  const go = async () => {
    setBusy(true)
    try {
      const tracks = await allTracks()
      if (tracks.length) player.playTracks(tracks, Math.floor(Math.random() * tracks.length), { shuffle: true })
    } finally {
      setBusy(false)
    }
  }
  return (
    <button className="btn btn--brand" onClick={go} disabled={busy} aria-busy={busy}>
      <IconShuffle size={20} /> {label}
    </button>
  )
}

function HomeTab({ home }: { home: AsyncState<MusicHome> }) {
  const [, setParams] = useSearchParams()
  if (home.loading && !home.data) return <TileSkeleton count={12} />
  if (home.error && !home.data) return <LoadError error={home.error} onRetry={() => home.reload()} />
  const d = home.data
  if (!d) return null
  const added = d.recentlyAdded ?? []
  const recent = d.recentlyPlayed ?? []
  const most = d.mostPlayed ?? []
  // A press on a lone song among "recently added" plays the lone songs from there on.
  const addedSongs = added.flatMap((e) => (e.track ? [e.track] : []))
  return (
    <>
      <div className="music__actions">
        <ShuffleAll />
        <button className="btn btn--grey" onClick={() => setParams({ tab: 'liked' }, { replace: true })}>
          Liked songs
        </button>
        <span className="music__count" style={{ alignSelf: 'center' }}>
          {plural(d.counts.tracks, 'song')} · {plural(d.counts.artists, 'artist')} · {plural(d.counts.albums, 'album')}
        </span>
      </div>
      {recent.length > 0 && (
        <section className="msection">
          <div className="msection__head">
            <h2>Jump back in</h2>
          </div>
          <div className="mgrid mgrid--row">
            {recent.map((t, i) => (
              <TrackTile key={t.id} track={t} list={recent} index={i} />
            ))}
          </div>
        </section>
      )}
      {added.length > 0 && (
        <section className="msection">
          <div className="msection__head">
            <h2>Recently added</h2>
            <button className="link-brand" onClick={() => setParams({ tab: 'songs', sort: 'added' }, { replace: true })}>
              See all
            </button>
          </div>
          <div className="mgrid mgrid--rows2">
            {added.map((e) =>
              e.album ? (
                <AlbumTile key={'a' + e.album.id} album={e.album} />
              ) : e.track ? (
                <TrackTile key={e.track.id} track={e.track} list={addedSongs} index={addedSongs.indexOf(e.track)} />
              ) : null,
            )}
          </div>
        </section>
      )}
      {most.length > 0 && (
        <section className="msection">
          <div className="msection__head">
            <h2>Most played</h2>
          </div>
          <TrackList tracks={most.slice(0, 10)} plays />
        </section>
      )}
    </>
  )
}

const SORTS: { id: TrackSort; label: string }[] = [
  { id: 'title', label: 'Title' },
  { id: 'artist', label: 'Artist' },
  { id: 'added', label: 'Recently added' },
  { id: 'plays', label: 'Most played' },
]

function SongsTab() {
  const [params, setParams] = useSearchParams()
  const sort = (SORTS.find((s) => s.id === params.get('sort'))?.id ?? 'title') as TrackSort
  const songs = useAsync('music-songs:' + sort, () => allTracks(sort), [sort])
  const tracks = songs.data ?? []
  const setSort = (s: TrackSort) => {
    const next = new URLSearchParams(params)
    if (s === 'title') next.delete('sort')
    else next.set('sort', s)
    setParams(next, { replace: true })
  }
  if (songs.loading && !songs.data) return <ListSkeleton />
  if (songs.error && !songs.data) return <LoadError error={songs.error} onRetry={() => songs.reload()} />
  return (
    <>
      <div className="music__bar">
        <button className="btn btn--brand" onClick={() => player.playTracks(tracks, 0, { shuffle: false })} disabled={!tracks.length}>
          <IconPlay size={20} /> Play
        </button>
        <button className="btn btn--grey" onClick={() => tracks.length && player.playTracks(tracks, Math.floor(Math.random() * tracks.length), { shuffle: true })} disabled={!tracks.length}>
          <IconShuffle size={20} /> Shuffle
        </button>
        <span className="music__count">{plural(tracks.length, 'song')}</span>
        <span className="music__spacer" />
        <select className="music__sort" value={sort} onChange={(e) => setSort(e.target.value as TrackSort)} aria-label="Sort the songs by">
          {SORTS.map((s) => (
            <option key={s.id} value={s.id}>
              {s.label}
            </option>
          ))}
        </select>
      </div>
      <TrackList tracks={tracks} plays={sort === 'plays'} />
    </>
  )
}

function ArtistsTab() {
  const artists = useAsync('music-artists', () => music.artists())
  if (artists.loading && !artists.data) return <TileSkeleton round />
  if (artists.error && !artists.data) return <LoadError error={artists.error} onRetry={() => artists.reload()} />
  const list = artists.data?.artists ?? []
  return (
    <>
      <div className="music__bar">
        <span className="music__count">{plural(list.length, 'artist')}</span>
      </div>
      <div className="mgrid">
        {list.map((a) => (
          <ArtistTile key={a.id} artist={a} />
        ))}
      </div>
    </>
  )
}

function AlbumsTab() {
  const albums = useAsync('music-albums', () => music.albums({ sort: 'artist' }))
  if (albums.loading && !albums.data) return <TileSkeleton />
  if (albums.error && !albums.data) return <LoadError error={albums.error} onRetry={() => albums.reload()} />
  const list = albums.data?.albums ?? []
  if (!list.length)
    return (
      <div className="music__empty">
        <b>No albums yet</b>
        An album is listed once two of its songs are in the library. Single songs are under Songs.
      </div>
    )
  return (
    <>
      <div className="music__bar">
        <span className="music__count">{plural(list.length, 'album')}</span>
      </div>
      <div className="mgrid">
        {list.map((a) => (
          <AlbumTile key={a.id} album={a} />
        ))}
      </div>
    </>
  )
}

function LikedTab() {
  const liked = useAsync('music-liked', () => music.tracks({ favorite: true, sort: 'favorited', limit: 1000 }))
  if (liked.loading && !liked.data) return <ListSkeleton />
  if (liked.error && !liked.data) return <LoadError error={liked.error} onRetry={() => liked.reload()} />
  const tracks = liked.data?.tracks ?? []
  if (!tracks.length)
    return (
      <div className="music__empty">
        <b>No liked songs yet</b>
        The heart on a song puts it here.
      </div>
    )
  return (
    <>
      <PlayButtons tracks={tracks} count={plural(tracks.length, 'song')} />
      <TrackList tracks={tracks} />
    </>
  )
}

function PlayButtons({ tracks, count, children }: { tracks: MusicTrack[]; count?: string; children?: ReactNode }) {
  return (
    <div className="music__bar">
      <button className="btn btn--brand" onClick={() => player.playTracks(tracks, 0, { shuffle: false })} disabled={!tracks.length}>
        <IconPlay size={20} /> Play
      </button>
      <button className="btn btn--grey" onClick={() => tracks.length && player.playTracks(tracks, Math.floor(Math.random() * tracks.length), { shuffle: true })} disabled={!tracks.length}>
        <IconShuffle size={20} /> Shuffle
      </button>
      {children}
      {count && <span className="music__count">{count}</span>}
    </div>
  )
}

function SearchResults({ query }: { query: string }) {
  const found = useAsync('music-search:' + query, () => music.search(query), [query])
  if (found.loading && !found.data) return <ListSkeleton count={6} />
  if (found.error && !found.data) return <LoadError error={found.error} onRetry={() => found.reload()} />
  const d = found.data
  if (!d || d.tracks.length + d.albums.length + d.artists.length === 0)
    return (
      <div className="music__empty">
        <b>Nothing found for “{query}”</b>
        Every word must be part of a title, an artist or an album.
      </div>
    )
  return (
    <>
      {d.artists.length > 0 && (
        <section className="msection">
          <div className="msection__head">
            <h2>Artists</h2>
          </div>
          <div className="mgrid mgrid--row">
            {d.artists.map((a) => (
              <ArtistTile key={a.id} artist={a} />
            ))}
          </div>
        </section>
      )}
      {d.albums.length > 0 && (
        <section className="msection">
          <div className="msection__head">
            <h2>Albums</h2>
          </div>
          <div className="mgrid mgrid--row">
            {d.albums.map((a) => (
              <AlbumTile key={a.id} album={a} />
            ))}
          </div>
        </section>
      )}
      {d.tracks.length > 0 && (
        <section className="msection">
          <div className="msection__head">
            <h2>Songs</h2>
          </div>
          <TrackList tracks={d.tracks} />
        </section>
      )}
    </>
  )
}

/** /music/artist/:id: every song the artist is on, then their albums. */
export function MusicArtistPage() {
  const { id = '' } = useParams()
  const page = useAsync('music-artist:' + id, () => music.artist(id), [id])
  const d = page.data
  // Songs the artist leads first, then the ones they are a guest on; within each, the
  // most played, then by title (the server's order).
  const tracks = useMemo(() => {
    if (!d) return []
    return [...d.tracks.filter((t) => t.artistId === d.artist.id), ...d.tracks.filter((t) => t.artistId !== d.artist.id)]
  }, [d])
  if (page.loading && !d)
    return (
      <Page>
        <ListSkeleton />
      </Page>
    )
  if (!d)
    return (
      <Page>
        <LoadError error={page.error} onRetry={() => page.reload()} title="This artist could not be opened" />
      </Page>
    )
  const a = d.artist
  return (
    <Page>
      <div className="mhero">
        <Cover art={a.art} size={232} round kind="artist" className="mhero__art mhero__art--round" />
        <div className="mhero__text">
          <div className="mhero__kind">Artist</div>
          <h1 className="mhero__title">{a.name}</h1>
          <div className="mhero__facts">
            {plural(a.trackCount, 'song')}
            {a.albumCount > 0 && ` · ${plural(a.albumCount, 'album')}`}
          </div>
          <div className="mhero__actions">
            <PlayButtons tracks={tracks}>
              <HeartButton kind="artist" id={a.id} loaded={a.favorite} size={24} />
            </PlayButtons>
          </div>
        </div>
      </div>
      {d.albums.length > 0 && (
        <section className="msection">
          <div className="msection__head">
            <h2>Albums</h2>
          </div>
          <div className="mgrid">
            {d.albums.map((al) => (
              <AlbumTile key={al.id} album={al} sub={albumLine(al)} />
            ))}
          </div>
        </section>
      )}
      <section className="msection">
        <div className="msection__head">
          <h2>Songs</h2>
        </div>
        <TrackList tracks={tracks} />
      </section>
    </Page>
  )
}

const albumLine = (al: MusicAlbum) => [al.year || null, plural(al.trackCount, 'song')].filter(Boolean).join(' · ')

/** /music/album/:id: the album's songs in their order. */
export function MusicAlbumPage() {
  const { id = '' } = useParams()
  const page = useAsync('music-album:' + id, () => music.album(id), [id])
  const d = page.data
  if (page.loading && !d)
    return (
      <Page>
        <ListSkeleton />
      </Page>
    )
  if (!d)
    return (
      <Page>
        <LoadError error={page.error} onRetry={() => page.reload()} title="This album could not be opened" />
      </Page>
    )
  const al = d.album
  const total = d.tracks.reduce((ms, t) => ms + t.durationMs, 0)
  return (
    <Page>
      <div className="mhero">
        <Cover art={al.art} size={232} kind="album" className="mhero__art" />
        <div className="mhero__text">
          <div className="mhero__kind">{d.tracks.length === 1 ? 'Single' : 'Album'}</div>
          <h1 className="mhero__title">{al.title}</h1>
          <div className="mhero__facts">
            <Link to={'/music/artist/' + al.artistId}>{al.artist}</Link>
            {al.year ? ` · ${al.year}` : ''} · {plural(d.tracks.length, 'song')}, {total >= 3_600_000 ? longDuration(total) : clock(total)}
          </div>
          <div className="mhero__actions">
            <PlayButtons tracks={d.tracks}>
              <HeartButton kind="album" id={al.id} loaded={al.favorite} size={24} />
            </PlayButtons>
          </div>
        </div>
      </div>
      <TrackList tracks={d.tracks} numbers="track" hideAlbum hideCover />
    </Page>
  )
}
