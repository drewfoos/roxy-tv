import { useState } from 'react'
import { api } from '../api/client'
import { useAsync } from '../hooks/useAsync'
import { TopBar } from '../components/TopBar'
import { PosterTile, ThumbTile } from '../components/Cards'
import { GridSkeleton } from '../components/Skeleton'

/** My Lists: Watchlist (saved titles) and History (what this profile played, newest first). */
export function Lists() {
  const [tab, setTab] = useState<'watchlist' | 'history'>('watchlist')
  return (
    <div className="page page--tab">
      <TopBar />
      <div className="tabhead">
        <h1>My Lists</h1>
      </div>
      <div className="sectiontabs" style={{ marginTop: 0, marginBottom: 20 }}>
        <button className={tab === 'watchlist' ? 'on' : ''} onClick={() => setTab('watchlist')}>
          Watchlist
        </button>
        <button className={tab === 'history' ? 'on' : ''} onClick={() => setTab('history')}>
          History
        </button>
      </div>
      {tab === 'watchlist' ? <Watchlist /> : <History />}
    </div>
  )
}

function Watchlist() {
  const { data, error, loading } = useAsync('watchlist', () => api.favorites())
  if (loading && !data) return <GridSkeleton />
  if (error && !data) return <div className="empty error">{error}</div>
  const items = data?.items ?? []
  if (!items.length) return <div className="empty">Titles you save with the bookmark on their page show up here.</div>
  return (
    <div className="grid">
      {items.map((it) => (
        <PosterTile key={it.id} item={it} />
      ))}
    </div>
  )
}

function History() {
  const { data, error, loading } = useAsync('history', () => api.history(200))
  if (loading && !data) return <GridSkeleton thumbs count={8} />
  if (error && !data) return <div className="empty error">{error}</div>
  const items = data?.items ?? []
  if (!items.length) return <div className="empty">Episodes and movies you watch show up here, newest first.</div>
  return (
    <div className="grid grid--thumbs">
      {items.map((it) => (
        <ThumbTile key={it.id} item={it} />
      ))}
    </div>
  )
}
