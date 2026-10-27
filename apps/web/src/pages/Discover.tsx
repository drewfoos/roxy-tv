import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { api } from '../api/client'
import type { DiscoverItem } from '../api/types'
import { useAsync } from '../hooks/useAsync'
import { TopBar } from '../components/TopBar'
import { Footer } from '../components/Footer'
import { Row } from '../components/Cards'
import { RowSkeleton } from '../components/Skeleton'
import { IconCheckCircle, IconSearch } from '../components/Icons'

/**
 * Discover: TMDB rows through the server, search as you type. A title the library has
 * links to its page; others will get the request page (next milestone).
 */
export function Discover() {
  const { data, error, loading } = useAsync('discover', () => api.discover())
  const [q, setQ] = useState('')
  const [results, setResults] = useState<DiscoverItem[] | null>(null)
  const [searching, setSearching] = useState(false)

  useEffect(() => {
    const t = q.trim()
    if (!t) {
      setResults(null)
      setSearching(false)
      return
    }
    setSearching(true)
    const h = setTimeout(() => {
      api
        .discoverSearch(t)
        .then((r) => setResults(r.items))
        .catch(() => setResults([]))
        .finally(() => setSearching(false))
    }, 350)
    return () => clearTimeout(h)
  }, [q])

  return (
    <div className="page page--tab page--phone">
      <TopBar phone="tab" />
      <div className="tabhead">
        <h1>Discover</h1>
        <div className="tabhead__spacer" />
        <div className="topbar__search" style={{ background: 'var(--surface-high)', minWidth: 360 }}>
          <IconSearch size={20} />
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search shows and movies on TMDB" />
        </div>
      </div>
      {results ? (
        results.length === 0 && !searching ? (
          <div className="empty">Nothing on TMDB for "{q}".</div>
        ) : (
          <div className="grid">
            {results.map((it) => (
              <DiscoverCard key={`${it.type}:${it.tmdbId}`} item={it} />
            ))}
          </div>
        )
      ) : loading && !data ? (
        <>
          <RowSkeleton />
          <RowSkeleton />
        </>
      ) : error && !data ? (
        <div className="empty">
          <div style={{ color: 'var(--fg)', fontSize: 18 }}>Discover is unavailable</div>
          <div>{error}</div>
        </div>
      ) : (
        (data?.rows ?? []).map((r) => (
          <Row key={r.key} title={r.title}>
            {r.items.map((it) => (
              <DiscoverCard key={`${it.type}:${it.tmdbId}`} item={it} />
            ))}
          </Row>
        ))
      )}
      <Footer />
    </div>
  )
}

function DiscoverCard({ item }: { item: DiscoverItem }) {
  const to = item.libraryItemId ? (item.type === 'movie' ? `/movie/${item.libraryItemId}` : `/series/${item.libraryItemId}`) : undefined
  const body = (
    <>
      <div className="card__art">
        {item.poster ? <img className="card__img" src={item.poster} alt="" loading="lazy" /> : <div className="art-fallback">{item.title}</div>}
        {item.inLibrary && (
          <span className="badge" style={{ top: 6, right: 6, background: 'var(--accent)', color: 'var(--on-brand)', borderRadius: '50%', width: 24, height: 24, display: 'grid', placeItems: 'center', padding: 0 }}>
            <IconCheckCircle size={16} />
          </span>
        )}
      </div>
      <div className="card__name">{item.title}</div>
      <div className="card__sub">
        <span>{[item.year, item.isAnime ? 'Anime' : item.type === 'movie' ? 'Movie' : null].filter(Boolean).join('  ·  ')}</span>
      </div>
    </>
  )
  return to ? (
    <Link to={to} className="card" title={item.title}>
      {body}
    </Link>
  ) : (
    <div className="card" title={item.title}>
      {body}
    </div>
  )
}
