// The Music pages: the library (home, songs, artists, albums, playlists, liked), an
// artist, an album, a playlist. Songs come first: most libraries are single songs, so a
// song is listed by itself and an album appears once two of its songs are there.
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Link, useNavigate, useNavigationType, useParams, useSearchParams } from 'react-router'
import { clock, longDuration, music, type MusicAlbum, type MusicHome, type MusicTrack, type PlaylistEntry, type TrackSort } from '../api/music'
import { useAsync, type AsyncState } from '../hooks/useAsync'
import { useSession } from '../app/useSession'
import { TopBar } from '../components/TopBar'
import { Footer } from '../components/Footer'
import { Row } from '../components/Cards'
import { LoadError } from '../components/Connection'
import { IconAdd, IconAlbum, IconClose, IconHeart, IconMusicNote, IconPlay, IconPlaylist, IconSearch, IconShuffle } from '../components/Icons'
import { playlistsChanged, usePlaylistsVersion } from '../music/changes'
import { MoreButton, NameDialog, SongsMenu, useMenu } from '../music/Menus'
import { AlbumTile, ArtistTile, Cover, Empty, HeartButton, ListSkeleton, PlaylistCover, PlaylistTile, TileSkeleton, TrackList, TrackTile } from '../music/Parts'
import { openNowPlaying } from '../music/links'
import { player } from '../music/player'
import { toast } from '../music/toast'
import '../styles/music.css'

type Tab = 'home' | 'songs' | 'artists' | 'albums' | 'playlists' | 'liked'
const TABS: { id: Tab; label: string }[] = [
  { id: 'home', label: 'Home' },
  { id: 'songs', label: 'Songs' },
  { id: 'artists', label: 'Artists' },
  { id: 'albums', label: 'Albums' },
  { id: 'playlists', label: 'Playlists' },
  { id: 'liked', label: 'Liked' },
]

const plural = (n: number, one: string, many = one + 's') => `${n.toLocaleString()} ${n === 1 ? one : many}`

// "Explore" shows the same pick for as long as the page is open: a new one at every
// refresh of the list behind it would shuffle the tiles under the pointer.
const pickSeed = Math.random().toString(36).slice(2)
function mix(text: string): number {
  let h = 2166136261
  for (let i = 0; i < text.length; i++) h = Math.imul(h ^ text.charCodeAt(i), 16777619)
  return h >>> 0
}
/** Some of a list, in an order that is the same for the whole visit. */
export function visitPick<T extends { id: string }>(all: T[], count: number, seed = pickSeed): T[] {
  return [...all].sort((a, b) => mix(seed + a.id) - mix(seed + b.id) || a.id.localeCompare(b.id)).slice(0, count)
}

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
      <div className="music__head music__pad">
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
          <div className="mtabs music__pad" role="tablist">
            {TABS.map((t) => (
              <button
                key={t.id}
                role="tab"
                aria-selected={tab === t.id}
                className={tab === t.id ? 'on' : ''}
                onClick={() => change((p) => (t.id === 'home' ? p.delete('tab') : p.set('tab', t.id)))}
              >
                {t.label}
              </button>
            ))}
          </div>
          <div className="music__tab" key={tab}>
            {tab === 'home' && <HomeTab home={home} />}
            {tab === 'songs' && <SongsTab />}
            {tab === 'artists' && <ArtistsTab />}
            {tab === 'albums' && <AlbumsTab />}
            {tab === 'playlists' && <PlaylistsTab />}
            {tab === 'liked' && <LikedTab />}
          </div>
        </>
      )}
    </Page>
  )
}

function NoMusic({ scanning }: { scanning: boolean }) {
  const s = useSession()
  if (scanning)
    return (
      <Empty icon={<IconMusicNote size={44} />} title="Reading the music folder…">
        Songs appear here as they are read.
      </Empty>
    )
  return (
    <Empty
      icon={<IconMusicNote size={44} />}
      title="No music yet"
      action={
        s?.isAdmin ? (
          <Link to="/account/library" className="btn btn--brand">
            <IconAdd size={20} /> Add a music folder
          </Link>
        ) : undefined
      }
    >
      {s?.isAdmin ? 'Point Roxy at a folder of songs: they are read from their tags, wherever they sit in it.' : 'This server has no music folder yet.'}
    </Empty>
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

/** A button that goes to another tab of the library (from an empty one). */
function TabButton({ tab, children }: { tab: Tab; children: ReactNode }) {
  const [, setParams] = useSearchParams()
  return (
    <button className="btn btn--brand" onClick={() => setParams({ tab }, { replace: true })}>
      {children}
    </button>
  )
}

function ShuffleAll({ label = 'Shuffle all' }: { label?: string }) {
  const [busy, setBusy] = useState(false)
  const go = async () => {
    setBusy(true)
    try {
      const tracks = await allTracks()
      if (tracks.length)
        player.playTracks(tracks, Math.floor(Math.random() * tracks.length), {
          shuffle: true,
        })
    } catch {
      toast.show('The songs could not be read from the server')
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
  const changes = usePlaylistsVersion()
  const lists = useAsync('music-playlists', () => music.playlists(), [changes])
  const albums = useAsync('music-albums', () => music.albums({ sort: 'artist' }))
  const artists = useAsync('music-artists', () => music.artists())
  const explore = useMemo(() => visitPick(albums.data?.albums ?? [], 24), [albums.data])
  // The artists with the most songs here.
  const top = useMemo(() => [...(artists.data?.artists ?? [])].sort((a, b) => b.trackCount - a.trackCount).slice(0, 24), [artists.data])
  if (home.loading && !home.data)
    return (
      <div className="music__pad">
        <TileSkeleton count={12} />
      </div>
    )
  if (home.error && !home.data) return <LoadError error={home.error} onRetry={() => home.reload()} />
  const d = home.data
  if (!d) return null
  const added = d.recentlyAdded ?? []
  const recent = d.recentlyPlayed ?? []
  const most = d.mostPlayed ?? []
  const playlists = lists.data?.playlists ?? []
  // A press on a lone song among "recently added" plays the lone songs from there on.
  const addedSongs = added.flatMap((e) => (e.track ? [e.track] : []))
  const more = (tab: Tab, sort?: string) => (
    <button className="link link-brand" onClick={() => setParams(sort ? { tab, sort } : { tab }, { replace: true })}>
      See all
    </button>
  )
  return (
    <>
      <div className="music__actions music__pad">
        <ShuffleAll />
        <button className="btn btn--grey" onClick={() => setParams({ tab: 'liked' }, { replace: true })}>
          Liked songs
        </button>
        <span className="music__count">
          {plural(d.counts.tracks, 'song')} · {plural(d.counts.artists, 'artist')} · {plural(d.counts.albums, 'album')}
        </span>
      </div>
      {recent.length > 0 && (
        <Row title="Jump back in" squares>
          {recent.map((t, i) => (
            <TrackTile key={t.id} track={t} list={recent} index={i} />
          ))}
        </Row>
      )}
      {added.length > 0 && (
        <Row title="Recently added" squares more={more('songs', 'added')}>
          {added.map((e) =>
            e.album ? (
              <AlbumTile key={'a' + e.album.id} album={e.album} />
            ) : e.track ? (
              <TrackTile key={e.track.id} track={e.track} list={addedSongs} index={addedSongs.indexOf(e.track)} />
            ) : null,
          )}
        </Row>
      )}
      {playlists.length > 0 && (
        <Row title="Your playlists" squares more={more('playlists')}>
          {playlists.map((p) => (
            <PlaylistTile key={p.id} playlist={p} />
          ))}
        </Row>
      )}
      {explore.length > 0 && (
        <Row title="Explore your albums" squares more={more('albums')}>
          {explore.map((a) => (
            <AlbumTile key={a.id} album={a} />
          ))}
        </Row>
      )}
      {top.length > 0 && (
        <Row title="Top artists" squares more={more('artists')}>
          {top.map((a) => (
            <ArtistTile key={a.id} artist={a} />
          ))}
        </Row>
      )}
      {most.length > 0 && (
        <section className="msection music__pad">
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

function PlayButtons({ tracks, count, children }: { tracks: MusicTrack[]; count?: string; children?: ReactNode }) {
  return (
    <div className="music__bar">
      <button className="btn btn--brand" onClick={() => player.playTracks(tracks, 0, { shuffle: false })} disabled={!tracks.length}>
        <IconPlay size={20} /> Play
      </button>
      <button
        className="btn btn--grey"
        onClick={() =>
          tracks.length &&
          player.playTracks(tracks, Math.floor(Math.random() * tracks.length), {
            shuffle: true,
          })
        }
        disabled={!tracks.length}
      >
        <IconShuffle size={20} /> Shuffle
      </button>
      {children}
      {count && <span className="music__count">{count}</span>}
    </div>
  )
}

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
  if (songs.loading && !songs.data)
    return (
      <div className="music__pad">
        <ListSkeleton />
      </div>
    )
  if (songs.error && !songs.data) return <LoadError error={songs.error} onRetry={() => songs.reload()} />
  return (
    <div className="music__pad">
      <PlayButtons tracks={tracks} count={plural(tracks.length, 'song')}>
        <span className="music__spacer" />
        <select className="music__sort" value={sort} onChange={(e) => setSort(e.target.value as TrackSort)} aria-label="Sort the songs by">
          {SORTS.map((s) => (
            <option key={s.id} value={s.id}>
              {s.label}
            </option>
          ))}
        </select>
      </PlayButtons>
      <TrackList tracks={tracks} plays={sort === 'plays'} header />
    </div>
  )
}

function ArtistsTab() {
  const artists = useAsync('music-artists', () => music.artists())
  if (artists.loading && !artists.data)
    return (
      <div className="music__pad">
        <TileSkeleton round />
      </div>
    )
  if (artists.error && !artists.data) return <LoadError error={artists.error} onRetry={() => artists.reload()} />
  const list = artists.data?.artists ?? []
  return (
    <div className="music__pad">
      <div className="music__bar">
        <span className="music__count">{plural(list.length, 'artist')}</span>
      </div>
      <div className="mgrid">
        {list.map((a) => (
          <ArtistTile key={a.id} artist={a} />
        ))}
      </div>
    </div>
  )
}

function AlbumsTab() {
  const albums = useAsync('music-albums', () => music.albums({ sort: 'artist' }))
  if (albums.loading && !albums.data)
    return (
      <div className="music__pad">
        <TileSkeleton />
      </div>
    )
  if (albums.error && !albums.data) return <LoadError error={albums.error} onRetry={() => albums.reload()} />
  const list = albums.data?.albums ?? []
  if (!list.length)
    return (
      <Empty icon={<IconAlbum size={44} />} title="No albums yet" action={<TabButton tab="songs">See the songs</TabButton>}>
        An album is listed once two of its songs are in the library. Single songs are under Songs.
      </Empty>
    )
  return (
    <div className="music__pad">
      <div className="music__bar">
        <span className="music__count">{plural(list.length, 'album')}</span>
      </div>
      <div className="mgrid">
        {list.map((a) => (
          <AlbumTile key={a.id} album={a} />
        ))}
      </div>
    </div>
  )
}

function PlaylistsTab() {
  const changes = usePlaylistsVersion()
  const lists = useAsync('music-playlists', () => music.playlists(), [changes])
  const [naming, setNaming] = useState(false)
  const nav = useNavigate()
  if (lists.loading && !lists.data)
    return (
      <div className="music__pad">
        <TileSkeleton count={6} />
      </div>
    )
  if (lists.error && !lists.data) return <LoadError error={lists.error} onRetry={() => lists.reload()} />
  const list = lists.data?.playlists ?? []
  const make = (
    <button className="btn btn--brand" onClick={() => setNaming(true)}>
      <IconAdd size={20} /> New playlist
    </button>
  )
  return (
    <div className="music__pad">
      {list.length ? (
        <>
          <div className="music__bar">
            {make}
            <span className="music__count">{plural(list.length, 'playlist')}</span>
          </div>
          <div className="mgrid">
            {list.map((p) => (
              <PlaylistTile key={p.id} playlist={p} />
            ))}
          </div>
        </>
      ) : (
        <Empty icon={<IconPlaylist size={44} />} title="No playlists yet" action={make}>
          Make one here, or from the three dots on any song, album or the queue: “Add to playlist”.
        </Empty>
      )}
      {naming && (
        <NameDialog
          title="New playlist"
          action="Create"
          onClose={() => setNaming(false)}
          onSubmit={async (name) => {
            const made = await music.createPlaylist(name)
            playlistsChanged()
            nav('/music/playlist/' + made.id)
          }}
        />
      )}
    </div>
  )
}

function LikedTab() {
  const liked = useAsync('music-liked', () => music.tracks({ favorite: true, sort: 'favorited', limit: 1000 }))
  if (liked.loading && !liked.data)
    return (
      <div className="music__pad">
        <ListSkeleton />
      </div>
    )
  if (liked.error && !liked.data) return <LoadError error={liked.error} onRetry={() => liked.reload()} />
  const tracks = liked.data?.tracks ?? []
  if (!tracks.length)
    return (
      <Empty icon={<IconHeart size={44} />} title="No liked songs yet" action={<TabButton tab="songs">Find some to like</TabButton>}>
        The heart on a song keeps it here, the newest first.
      </Empty>
    )
  return (
    <div className="music__pad">
      <PlayButtons tracks={tracks} count={plural(tracks.length, 'song')} />
      <TrackList tracks={tracks} header />
    </div>
  )
}

function SearchResults({ query }: { query: string }) {
  const found = useAsync('music-search:' + query, () => music.search(query), [query])
  if (found.loading && !found.data)
    return (
      <div className="music__pad">
        <ListSkeleton count={6} />
      </div>
    )
  if (found.error && !found.data) return <LoadError error={found.error} onRetry={() => found.reload()} />
  const d = found.data
  if (!d || d.tracks.length + d.albums.length + d.artists.length === 0)
    return (
      <Empty icon={<IconSearch size={44} />} title={`Nothing found for “${query}”`}>
        Every word must be part of a title, an artist or an album. Case and accents do not matter.
      </Empty>
    )
  return (
    <>
      {d.artists.length > 0 && (
        <Row title="Artists" squares>
          {d.artists.map((a) => (
            <ArtistTile key={a.id} artist={a} />
          ))}
        </Row>
      )}
      {d.albums.length > 0 && (
        <Row title="Albums" squares>
          {d.albums.map((a) => (
            <AlbumTile key={a.id} album={a} />
          ))}
        </Row>
      )}
      {d.tracks.length > 0 && (
        <section className="msection music__pad">
          <div className="msection__head">
            <h2>Songs</h2>
          </div>
          <TrackList tracks={d.tracks} />
        </section>
      )}
    </>
  )
}

/** The ⋮ beside a page's Play and Shuffle: the whole of it next, at the end of the queue, into a playlist. */
function PageMenu({ tracks, label }: { tracks: MusicTrack[]; label: string }) {
  const menu = useMenu()
  return (
    <>
      <MoreButton label={'More for ' + label} open={!!menu.at} onClick={menu.toggle} />
      {menu.at && <SongsMenu at={menu.at} onClose={menu.close} tracks={async () => tracks} />}
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
        <div className="music__pad">
          <ListSkeleton />
        </div>
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
      <div className="mhero music__pad">
        <Cover art={a.art} size={232} round kind="artist" className="mhero__art mhero__art--round" />
        <div className="mhero__text">
          <div className="mhero__kind">Artist</div>
          <h1 className="mhero__title">{a.name}</h1>
          <div className="mhero__facts">
            {plural(a.trackCount, 'song')}
            {a.albumCount > 0 && ` · ${plural(a.albumCount, 'album')}`}
          </div>
          <PlayButtons tracks={tracks}>
            <HeartButton kind="artist" id={a.id} loaded={a.favorite} size={24} />
            <PageMenu tracks={tracks} label={a.name} />
          </PlayButtons>
        </div>
      </div>
      {d.albums.length > 0 && (
        <Row title="Albums" squares>
          {d.albums.map((al) => (
            <AlbumTile key={al.id} album={al} sub={albumLine(al)} />
          ))}
        </Row>
      )}
      <section className="msection music__pad">
        <div className="msection__head">
          <h2>Songs</h2>
        </div>
        <TrackList tracks={tracks} header />
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
        <div className="music__pad">
          <ListSkeleton />
        </div>
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
      <div className="mpage">
        <div className="mhero music__pad">
          <Cover art={al.art} size={232} kind="album" className="mhero__art" />
          <div className="mhero__text">
            <div className="mhero__kind">{d.tracks.length === 1 ? 'Single' : 'Album'}</div>
            <h1 className="mhero__title">{al.title}</h1>
            <div className="mhero__facts">
              <Link to={'/music/artist/' + al.artistId}>{al.artist}</Link>
              {al.year ? ` · ${al.year}` : ''} · {plural(d.tracks.length, 'song')}, {total >= 3_600_000 ? longDuration(total) : clock(total)}
            </div>
            <PlayButtons tracks={d.tracks}>
              <HeartButton kind="album" id={al.id} loaded={al.favorite} size={24} />
              <PageMenu tracks={d.tracks} label={al.title} />
            </PlayButtons>
          </div>
        </div>
        <div className="mpage__main music__pad">
          <TrackList tracks={d.tracks} numbers="track" hideAlbum hideCover hideArtist={al.artist} header />
        </div>
      </div>
    </Page>
  )
}

/**
 * /music/playlist/:id: a playlist's songs in its order. Rows are dragged to another
 * place; the order shown changes at once and the server is told behind it.
 */
/**
 * A song's own address (music/links.ts): what "Copy link" gives and what the address bar
 * shows while "now playing" is open. It is the music home with that song in "now
 * playing", ready to be played (a page may not start sound by itself). With something
 * else playing, the link does not take the player over: the song is put next.
 */
export function MusicSongPage() {
  const { id = '' } = useParams()
  const nav = useNavigate()
  const how = useNavigationType()
  const song = useAsync('music-track:' + id, () => music.track(id), [id])
  const found = song.data
  const missing = !!song.error && !song.loading
  useEffect(() => {
    if (missing) {
      toast.show('That song is not in the library')
      nav('/music', { replace: true })
      return
    }
    if (!found) return
    const state = player.get()
    const current = state.queue.tracks[state.queue.index]
    if (current?.id === found.id) return openNowPlaying()
    if (state.playing) {
      // Come back to by the browser's Back, with another song playing by now: nothing is
      // asked for. Arrived at by a link while something plays: the song is put next.
      if (how !== 'POP') {
        player.playNext([found])
        toast.show(`"${found.title}" plays next`)
      }
      nav('/music', { replace: true })
      return
    }
    player.cue(found)
    openNowPlaying()
  }, [found?.id, missing]) // eslint-disable-line react-hooks/exhaustive-deps
  return <Music />
}

export function MusicPlaylistPage() {
  const { id = '' } = useParams()
  const nav = useNavigate()
  const changes = usePlaylistsVersion()
  const page = useAsync('music-playlist:' + id, () => music.playlist(id), [id, changes])
  // The list as the person last arranged it, ahead of the server's answer.
  const [local, setLocal] = useState<PlaylistEntry[] | null>(null)
  const [renaming, setRenaming] = useState(false)
  const [confirming, setConfirming] = useState(false)
  // Changes are sent one after another, in the order they were made (two sent at once
  // could reach the server the other way round), and the page reads the list again only
  // when the last of them is done: an answer from between two changes would show the
  // first without the second for a moment.
  const chain = useRef<Promise<unknown>>(Promise.resolve())
  const waiting = useRef(0)
  useEffect(() => {
    if (waiting.current === 0) setLocal(null)
  }, [page.data])
  useEffect(() => {
    setLocal(null)
    setConfirming(false)
  }, [id])
  const d = page.data
  if (page.loading && !d)
    return (
      <Page>
        <div className="music__pad">
          <ListSkeleton />
        </div>
      </Page>
    )
  if (!d)
    return (
      <Page>
        <LoadError error={page.error} onRetry={() => page.reload()} title="This playlist could not be opened" />
      </Page>
    )
  const p = d.playlist
  const tracks = local ?? d.tracks
  const total = tracks.reduce((ms, t) => ms + t.durationMs, 0)
  const change = (next: PlaylistEntry[], ask: () => Promise<unknown>) => {
    setLocal(next)
    waiting.current++
    let failed = false
    chain.current = chain.current
      .then(ask)
      .catch(() => {
        failed = true
        toast.show('That change was not saved')
      })
      .then(() => {
        if (--waiting.current > 0) return
        // The lists of playlists show its count and cover; this page reads its own list.
        playlistsChanged()
        if (failed) setLocal(null)
      })
  }
  const move = (from: number, to: number) => {
    const next = tracks.slice()
    const [moved] = next.splice(from, 1)
    next.splice(to, 0, moved)
    change(next, () =>
      music.orderPlaylist(
        p.id,
        next.map((t) => t.entryId),
      ),
    )
  }
  const remove = (index: number) => {
    const entry = tracks[index]
    change(
      tracks.filter((_, i) => i !== index),
      () => music.removeFromPlaylist(p.id, [entry.entryId]),
    )
  }
  return (
    <Page>
      <div className="mpage">
        <div className="mhero music__pad">
          <PlaylistCover art={p.art} size={232} className="mhero__art" />
          <div className="mhero__text">
            <div className="mhero__kind">Playlist</div>
            <h1 className="mhero__title">{p.name}</h1>
            <div className="mhero__facts">
              {plural(tracks.length, 'song')}
              {total > 0 && `, ${total >= 3_600_000 ? longDuration(total) : clock(total)}`}
            </div>
            <PlayButtons tracks={tracks}>
              <PageMenu tracks={tracks} label={p.name} />
              <button className="btn btn--grey" onClick={() => setRenaming(true)}>
                Rename
              </button>
              {confirming ? (
                <button
                  className="btn btn--danger"
                  onClick={() =>
                    music
                      .deletePlaylist(p.id)
                      .then(() => {
                        playlistsChanged()
                        toast.show(`Deleted ${p.name}`)
                        nav('/music?tab=playlists', { replace: true })
                      })
                      .catch(() => toast.show('The playlist could not be deleted'))
                  }
                  onBlur={() => setConfirming(false)}
                >
                  Delete it for good
                </button>
              ) : (
                <button className="btn btn--grey" onClick={() => setConfirming(true)}>
                  Delete
                </button>
              )}
            </PlayButtons>
          </div>
        </div>
        <div className="mpage__main music__pad">
          {tracks.length ? (
            <TrackList
              tracks={tracks}
              header
              onMove={move}
              rowKey={(t) => (t as PlaylistEntry).entryId}
              rowMenu={(_, i) => [
                {
                  label: 'Remove from this playlist',
                  onSelect: () => remove(i),
                  danger: true,
                },
              ]}
            />
          ) : (
            <Empty
              icon={<IconPlaylist size={44} />}
              title="Nothing in this playlist yet"
              action={
                <Link to="/music?tab=songs" className="btn btn--brand">
                  Find songs to add
                </Link>
              }
            >
              The three dots on any song or album have “Add to playlist”.
            </Empty>
          )}
        </div>
      </div>
      {renaming && (
        <NameDialog
          title="Rename playlist"
          action="Save"
          initial={p.name}
          onClose={() => setRenaming(false)}
          onSubmit={async (name) => {
            await music.renamePlaylist(p.id, name)
            playlistsChanged()
          }}
        />
      )}
    </Page>
  )
}
