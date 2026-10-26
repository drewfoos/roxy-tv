import { Link, useSearchParams } from 'react-router'
import { api, px } from '../api/client'
import { audioBadge, isMovie, yearOf } from '../api/items'
import type { Item } from '../api/types'
import { useAsync } from '../hooks/useAsync'
import { TopBar } from '../components/TopBar'
import { Footer } from '../components/Footer'
import { Art, ThumbTile, itemPath } from '../components/Cards'
import { IconBookmark } from '../components/Icons'
import { GridSkeleton } from '../components/Skeleton'

/**
 * My Lists, laid out like Crunchyroll's: the bookmark and title centred, WATCHLIST |
 * HISTORY tabs centred under it, then a centred four-wide grid of wide cards.
 */
export function Lists() {
  const [params, setParams] = useSearchParams()
  const tab = params.get('tab') === 'history' ? 'history' : 'watchlist'
  const pick = (t: 'watchlist' | 'history') => {
    const next = new URLSearchParams(params)
    if (t === 'history') next.set('tab', 'history')
    else next.delete('tab')
    setParams(next, { replace: true })
  }
  return (
    <div className="page page--tab">
      <TopBar />
      <div className="lists">
        <h1 className="lists__title">
          <IconBookmark size={30} /> My Lists
        </h1>
        <div className="lists__tabs">
          <button className={tab === 'watchlist' ? 'on' : ''} onClick={() => pick('watchlist')}>
            Watchlist
          </button>
          <button className={tab === 'history' ? 'on' : ''} onClick={() => pick('history')}>
            History
          </button>
        </div>
        {tab === 'watchlist' ? <Watchlist /> : <History />}
      </div>
      <Footer />
    </div>
  )
}

function Watchlist() {
  const { data, error, loading } = useAsync('watchlist', () => api.favorites())
  if (loading && !data) return <GridSkeleton thumbs count={8} />
  if (error && !data) return <div className="empty error">{error}</div>
  const items = data?.items ?? []
  if (!items.length) return <div className="empty">Titles you save with the bookmark on their page show up here.</div>
  return (
    <>
      <div className="lists__head">
        <h2>Recent Activity</h2>
      </div>
      <div className="lists__grid">
        {items.map((it) => (
          <WideCard key={it.id} item={it} />
        ))}
      </div>
    </>
  )
}

function History() {
  const { data, error, loading } = useAsync('history', () => api.history(200))
  if (loading && !data) return <GridSkeleton thumbs count={8} />
  if (error && !data) return <div className="empty error">{error}</div>
  const items = data?.items ?? []
  if (!items.length) return <div className="empty">Episodes and movies you watch show up here, newest first.</div>
  return (
    <>
      <div className="lists__head">
        <h2>Recently watched</h2>
      </div>
      <div className="lists__grid">
        {items.map((it) => (
          <ThumbTile key={it.id} item={it} width={320} />
        ))}
      </div>
    </>
  )
}

/** A saved title as a wide card: backdrop, name, "2023 · Anime", "Sub | Dub". */
function WideCard({ item }: { item: Item }) {
  const url = api.imageUrl(item, 'backdrop', px(320)) ?? api.imageUrl(item, 'thumb', px(320)) ?? api.imageUrl(item, 'poster', px(320))
  return (
    <div className="card">
      <Link to={itemPath(item)} className="card__art card__art--thumb">
        <Art url={url} title={item.title} />
      </Link>
      <Link to={itemPath(item)} className="card__name">
        {item.title}
      </Link>
      <div className="card__sub">
        <span>{[yearOf(item), isMovie(item) ? 'Movie' : item.isAnime ? 'Anime' : 'Series'].filter(Boolean).join(' · ')}</span>
      </div>
      <div className="card__sub">
        <span>{audioBadge(item) ?? ''}</span>
      </div>
    </div>
  )
}
