import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { DiscoverItem } from '../api/types'
import { useAsync } from '../hooks/useAsync'
import { TopBar } from '../components/TopBar'
import { Footer } from '../components/Footer'
import { Row } from '../components/Cards'
import { GridSkeleton, RowSkeleton } from '../components/Skeleton'
import { IconClose, IconSearch } from '../components/Icons'
import { DiscoverCard, DiscoverGrid, isActive, isPending } from '../components/DiscoverBits'

/** How long the TMDB rows (~100 KB) are shown from memory before asking again, as on the phone. */
const ROWS_FRESH_MS = 120_000

/**
 * Discover, the phone app's tab (PhoneDiscover.kt): TMDB rows through the server, search
 * as you type, and "Your requests" first. Every poster opens a page: the library's when
 * the library has it, else the Discover title page with Request. The corner of each
 * poster says where it stands (in the library, requested, downloading and how far).
 */
export function Discover() {
  const rows = useAsync('discover', () => api.discover(), [], ROWS_FRESH_MS)
  // Requests move (their download status): refreshed on every visit, every few seconds
  // while one of them is downloading, and every minute while one is sent but not done
  // (no status yet, or Sonarr still loading its episodes).
  const reqs = useAsync('discover:requests', () => api.discoverRequests().then((r) => r.items ?? []))
  const requests = reqs.data ?? []
  const pollMs = requests.some((r) => isActive(r.download)) ? 5000 : requests.some((r) => isPending(r.requestStatus, r.download)) ? 60000 : 0
  const reloadReqs = reqs.reload
  useEffect(() => {
    if (!pollMs) return
    const h = window.setInterval(() => {
      if (!document.hidden) reloadReqs(true)
    }, pollMs)
    return () => window.clearInterval(h)
  }, [pollMs, reloadReqs])

  const [q, setQ] = useState('')
  const [results, setResults] = useState<DiscoverItem[] | null>(null)
  const [searching, setSearching] = useState(false)
  const [searchError, setSearchError] = useState<string | null>(null)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    const t = q.trim()
    setSearchError(null)
    if (!t) {
      setResults(null)
      setSearching(false)
      return
    }
    setSearching(true)
    let live = true
    const h = setTimeout(() => {
      api
        .discoverSearch(t)
        .then((r) => live && setResults(r.items ?? []))
        .catch((e: unknown) => {
          if (!live) return
          setResults(null)
          setSearchError(e instanceof Error ? e.message : String(e))
        })
        .finally(() => live && setSearching(false))
    }, 350)
    return () => {
      live = false
      clearTimeout(h)
    }
  }, [q, attempt])

  // Rows and "Your requests" appear together: requests arriving after the rows would push
  // the first row down under the reader's eyes.
  const ready = rows.data && (reqs.data || reqs.error)
  const searchActive = q.trim() !== ''

  return (
    <div className="page page--tab page--phone">
      <TopBar phone="tab" />
      <div className="tabhead">
        <h1>Discover</h1>
        <div className="tabhead__spacer" />
        <div className="topbar__search" style={{ background: 'var(--surface-high)', minWidth: 360 }}>
          <IconSearch size={20} />
          <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search shows and movies on TMDB" aria-label="Search TMDB" />
          {q && (
            <button className="icon-btn" onClick={() => setQ('')} aria-label="Clear search" style={{ color: 'var(--muted)', display: 'flex' }}>
              <IconClose size={20} />
            </button>
          )}
        </div>
      </div>
      {searchActive ? (
        searchError ? (
          <div className="empty">
            <div style={{ color: 'var(--fg)', fontSize: 16, fontWeight: 700 }}>Couldn't reach TMDB</div>
            <div style={{ margin: '6px 0 14px' }}>{searchError}</div>
            <button className="btn btn--grey" onClick={() => setAttempt((a) => a + 1)}>
              Try again
            </button>
          </div>
        ) : results == null ? (
          <GridSkeleton />
        ) : results.length === 0 && !searching ? (
          <div className="empty">Nothing on TMDB for "{q.trim()}".</div>
        ) : (
          <DiscoverGrid items={results} />
        )
      ) : rows.error && !rows.data ? (
        <div className="empty">
          <div style={{ color: 'var(--fg)', fontSize: 18, fontWeight: 700 }}>Discover is unavailable</div>
          <div style={{ margin: '8px 0 16px' }}>{rows.error}</div>
          <button className="btn btn--grey" onClick={() => rows.reload()}>
            Try again
          </button>
        </div>
      ) : !ready ? (
        <>
          <RowSkeleton />
          <RowSkeleton />
          <RowSkeleton />
        </>
      ) : (
        <>
          {requests.length > 0 && (
            <Row title="Your requests">
              {requests.map((it) => (
                <DiscoverCard key={`${it.type}:${it.tmdbId}`} item={it} />
              ))}
            </Row>
          )}
          {(rows.data?.rows ?? []).map((r) => (
            <Row key={r.key} title={r.title}>
              {r.items.map((it) => (
                <DiscoverCard key={`${it.type}:${it.tmdbId}`} item={it} />
              ))}
            </Row>
          ))}
        </>
      )}
      <Footer />
    </div>
  )
}
