import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { api } from '../api/client'
import { isEpisode, isMovie, isSeries } from '../api/items'
import type { HomeRow, Item } from '../api/types'
import { useAsync } from '../hooks/useAsync'
import { TopBar } from '../components/TopBar'
import { Footer } from '../components/Footer'
import { Hero, type HeroPick } from '../components/Hero'
import { Row, ThumbTile, TitleCard } from '../components/Cards'
import { RowSkeleton, Spinner } from '../components/Skeleton'
import { WitchHat } from '../components/Icons'

const MAX_HEROES = 6

/**
 * Carousel picks (the phone's rule): what you are in the middle of first, one slide per
 * show, then the newest arrivals, then the library's series. Episodes resolve to their
 * series for the art and logo; the episode stays what Play starts.
 */
async function resolveHeroes(rows: HomeRow[]): Promise<HeroPick[]> {
  const picks = new Map<string, Item>()
  for (const key of ['continue', 'recentEpisodes', 'recentMovies', 'series']) {
    for (const it of rows.find((r) => r.key === key)?.items ?? []) {
      if (picks.size >= MAX_HEROES) break
      const k = isEpisode(it) ? (it.seriesId ?? it.id) : it.id
      if (!picks.has(k)) picks.set(k, it)
    }
  }
  const out = await Promise.all(
    Array.from(picks.values()).map(async (play): Promise<HeroPick | null> => {
      if (!isEpisode(play)) return { title: play, play }
      if (!play.seriesId) return null
      try {
        return { title: await api.item(play.seriesId), play }
      } catch {
        return null
      }
    }),
  )
  return out.filter((h): h is HeroPick => h != null)
}

export function Home() {
  const { data, error, loading, reload } = useAsync('home', () => api.home())
  const [heroes, setHeroes] = useState<HeroPick[]>([])

  useEffect(() => {
    if (!data) return
    let live = true
    resolveHeroes(data.rows).then((h) => live && setHeroes(h))
    return () => {
      live = false
    }
  }, [data])

  useEffect(() => {
    const onFocus = () => reload(true)
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [reload])

  const rows = (data?.rows ?? []).filter((r) => r.items.length)
  const empty = data && rows.length === 0

  const recent = [...(rows.find((r) => r.key === 'series')?.items ?? []), ...(rows.find((r) => r.key === 'recentMovies')?.items ?? [])]
    .sort((a, b) => (b.dateCreated ?? '').localeCompare(a.dateCreated ?? ''))
    .slice(0, 20)
  const cont = rows.find((r) => r.key === 'continue')
  const series = rows.find((r) => r.key === 'series')
  const movies = rows.find((r) => r.key === 'movies')

  return (
    <div className={'page' + (empty ? '' : ' page--hero')}>
      <TopBar clearOverHero={!empty} />
      {loading && !data ? (
        <Spinner />
      ) : error && !data ? (
        <div className="empty">
          <div style={{ fontSize: 18, color: 'var(--fg)', marginBottom: 8 }}>Could not load the library</div>
          <div>{error}</div>
          <button className="btn btn--grey" style={{ marginTop: 16 }} onClick={() => reload()}>
            Retry
          </button>
        </div>
      ) : empty ? (
        <div className="empty" style={{ paddingTop: 160 }}>
          <WitchHat size={72} />
          <div style={{ fontSize: 22, fontWeight: 700, color: 'var(--fg)', marginTop: 20 }}>Nothing to watch yet</div>
          <div style={{ marginTop: 8 }}>
            The server has no shows or movies yet. <Link className="link" to="/discover">Request some in Discover</Link>; they show up here once they're in.
          </div>
        </div>
      ) : (
        <>
          <Hero heroes={heroes} />
          {recent.length > 0 && (
            <Row title="Recently added">
              {recent.map((it) => (isEpisode(it) ? <ThumbTile key={it.id} item={it} /> : <TitleCard key={it.id} item={it} />))}
            </Row>
          )}
          {cont && (
            <Row title="Continue Watching" thumbs>
              {cont.items.map((it) => (
                <ThumbTile key={it.id} item={it} width={420} big />
              ))}
            </Row>
          )}
          {series && (
            <Row title="Series" more={<Link className="link" to="/browse">See all</Link>}>
              {series.items.map((it) => (
                <TitleCard key={it.id} item={it} />
              ))}
            </Row>
          )}
          {movies && (
            <Row title="Movies" more={<Link className="link" to="/browse?type=movie">See all</Link>}>
              {movies.items.map((it) => (
                <TitleCard key={it.id} item={it} />
              ))}
            </Row>
          )}
          {rows.filter((r) => !['continue', 'series', 'movies', 'recentMovies', 'recentEpisodes'].includes(r.key)).map((r) => (
            <Row key={r.key} title={r.title || r.key}>
              {r.items.map((it) => (isSeries(it) || isMovie(it) ? <TitleCard key={it.id} item={it} /> : <ThumbTile key={it.id} item={it} />))}
            </Row>
          ))}
          {loading && <RowSkeleton />}
        </>
      )}
      <Footer />
    </div>
  )
}
