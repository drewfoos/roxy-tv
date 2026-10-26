import { useMemo } from 'react'
import { useSearchParams } from 'react-router'
import { api } from '../api/client'
import { isMovie } from '../api/items'
import { useAsync } from '../hooks/useAsync'
import { TopBar } from '../components/TopBar'
import { Footer } from '../components/Footer'
import { PosterTile } from '../components/Cards'
import { GridSkeleton } from '../components/Skeleton'

/** The whole library as a poster grid: Series | Movies, and the search from the top bar. */
export function Browse() {
  const [params, setParams] = useSearchParams()
  const movies = params.get('type') === 'movie'
  const q = (params.get('q') ?? '').trim().toLowerCase()
  const { data, error, loading } = useAsync('library', () => api.items({ type: 'series,movie', recursive: true, sort: 'title', limit: 2000 }))

  const shown = useMemo(() => {
    const all = data?.items ?? []
    const list = q ? all.filter((it) => it.title.toLowerCase().includes(q) || (it.genres ?? []).some((g) => g.toLowerCase().includes(q))) : all.filter((it) => isMovie(it) === movies)
    return list
  }, [data, q, movies])

  const setType = (m: boolean) => {
    const next = new URLSearchParams(params)
    if (m) next.set('type', 'movie')
    else next.delete('type')
    next.delete('q')
    setParams(next, { replace: true })
  }

  return (
    <div className="page page--tab">
      <TopBar />
      <div className="tabhead">
        <h1>{q ? `Results for "${params.get('q')}"` : 'Browse'}</h1>
      </div>
      {!q && (
        <div className="chips">
          <button className={'chip' + (!movies ? ' chip--on' : '')} onClick={() => setType(false)}>
            Series
          </button>
          <button className={'chip' + (movies ? ' chip--on' : '')} onClick={() => setType(true)}>
            Movies
          </button>
        </div>
      )}
      {loading && !data ? (
        <GridSkeleton />
      ) : error && !data ? (
        <div className="empty error">{error}</div>
      ) : shown.length === 0 ? (
        <div className="empty">{q ? `No matches for "${params.get('q')}".` : 'Nothing here yet.'}</div>
      ) : (
        <div className="grid">
          {shown.map((it) => (
            <PosterTile key={it.id} item={it} />
          ))}
        </div>
      )}
      <Footer />
    </div>
  )
}
