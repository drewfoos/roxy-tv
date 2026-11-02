import { useEffect, useMemo, useRef, type KeyboardEvent } from 'react'
import { useSearchParams } from 'react-router'
import { api } from '../api/client'
import { isMovie, isSeries, yearOf } from '../api/items'
import type { Item } from '../api/types'
import { useAsync } from '../hooks/useAsync'
import { TopBar } from '../components/TopBar'
import { TabSearchLink } from '../components/PhoneChrome'
import { IconSearch } from '../components/Icons'
import { Footer } from '../components/Footer'
import { TitleCard } from '../components/Cards'
import { GridSkeleton } from '../components/Skeleton'

/** The library list is ~1000 titles; a visit within this long of the last one reuses it. */
const LIBRARY_FRESH_MS = 5 * 60_000

/** A way of slicing the library (the TV's TvBrowse.kt): a URL key, a label, the rule. */
interface Category {
  key: string
  label: string
  genre?: boolean
  pick: (all: Item[]) => Item[]
}

const sortKey = (it: Item) => (it.sortTitle || it.title).toLowerCase()
const byTitle = (a: Item, b: Item) => sortKey(a).localeCompare(sortKey(b))

/** Newly Added, A-Z, Series, Movies, then every genre in the library, most titles first. */
function categories(all: Item[]): Category[] {
  const fixed: Category[] = [
    { key: 'new', label: 'Newly Added', pick: (l) => [...l].sort((a, b) => (b.dateCreated ?? '').localeCompare(a.dateCreated ?? '') || byTitle(a, b)) },
    { key: 'az', label: 'A-Z', pick: (l) => [...l].sort(byTitle) },
    { key: 'series', label: 'Series', pick: (l) => l.filter(isSeries).sort(byTitle) },
    { key: 'movies', label: 'Movies', pick: (l) => l.filter(isMovie).sort(byTitle) },
  ]
  const counts = new Map<string, number>()
  for (const it of all) for (const g of it.genres ?? []) counts.set(g, (counts.get(g) ?? 0) + 1)
  const genres = [...counts.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])).map(([g]) => g)
  return [...fixed, ...genres.map((g) => ({ key: 'genre:' + g, label: g, genre: true, pick: (l: Item[]) => l.filter((x) => x.genres?.includes(g)).sort(byTitle) }))]
}

/** "2019 · Series" under each poster. */
const subLine = (it: Item) => [yearOf(it), isMovie(it) ? 'Movie' : 'Series'].filter(Boolean).join('  ·  ')

/**
 * The whole library, laid out like the TV's Browse: the categories at the left (a chip row
 * across the top on a phone) and the posters at the right, each with its name and
 * "year · Series" under it. The category lives in the URL (?c=new | az | series | movies |
 * genre:Action); ?q= is the search from the top bar and searches every title.
 */
export function Browse() {
  const [params, setParams] = useSearchParams()
  const q = (params.get('q') ?? '').trim().toLowerCase()
  // ?type=movie is the old Movies link; old bookmarks still land on Movies.
  const selected = params.get('c') ?? (params.get('type') === 'movie' ? 'movies' : 'new')
  const { data, error, loading } = useAsync('library', () => api.items({ type: 'series,movie', recursive: true, sort: 'title', limit: 2000 }), [], LIBRARY_FRESH_MS)

  const all = useMemo(() => data?.items ?? [], [data])
  const cats = useMemo(() => categories(all), [all])
  const cat = cats.find((c) => c.key === selected) ?? cats[0]

  const shown = useMemo(() => {
    if (q) return all.filter((it) => it.title.toLowerCase().includes(q) || (it.genres ?? []).some((g) => g.toLowerCase().includes(q))).sort(byTitle)
    return cat.pick(all)
  }, [all, q, cat])

  const choose = (key: string) => {
    const next = new URLSearchParams(params)
    if (key === 'new') next.delete('c')
    else next.set('c', key)
    next.delete('type')
    next.delete('q')
    next.delete('search')
    setParams(next, { replace: true })
    // A long grid left scrolled far down would open the next category part-way through.
    const top = document.querySelector('.browse')?.getBoundingClientRect().top ?? 0
    if (top < 0) window.scrollBy({ top: top - 80 })
  }

  // The category list: arrow keys walk it (up/down at the left, left/right as chips), Home/End jump.
  const listRef = useRef<HTMLDivElement>(null)
  const onListKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const btns = Array.from(listRef.current?.querySelectorAll<HTMLButtonElement>('button') ?? [])
    const at = btns.indexOf(document.activeElement as HTMLButtonElement)
    if (at < 0) return
    let to = -1
    if (e.key === 'ArrowDown' || e.key === 'ArrowRight') to = Math.min(btns.length - 1, at + 1)
    else if (e.key === 'ArrowUp' || e.key === 'ArrowLeft') to = Math.max(0, at - 1)
    else if (e.key === 'Home') to = 0
    else if (e.key === 'End') to = btns.length - 1
    if (to < 0) return
    e.preventDefault()
    btns[to].focus()
    btns[to].click()
  }

  // As chips (phone), bring the chosen one into view, scrolling only the row, never the page.
  const activeKey = q ? null : cat?.key
  useEffect(() => {
    const row = listRef.current
    const on = row?.querySelector<HTMLElement>('.browse__cat--on')
    if (!row || !on || row.scrollWidth <= row.clientWidth) return
    row.scrollLeft = on.offsetLeft - (row.clientWidth - on.offsetWidth) / 2
  }, [activeKey, cats.length])

  const heading = q ? `Results for "${params.get('q')}"` : cat.label
  const ready = !(loading && !data) && !(error && !data)

  return (
    <div className="page page--tab page--phone">
      <TopBar phone="tab" />
      <div className="tabhead">
        <h1>Browse</h1>
        <div className="tabhead__spacer" />
        <TabSearchLink />
      </div>
      {(params.get('search') === '1' || q) && (
        <form
          className="psearch phone-only"
          onSubmit={(e) => {
            e.preventDefault()
            const v = (new FormData(e.currentTarget).get('q') as string).trim()
            const next = new URLSearchParams(params)
            if (v) next.set('q', v)
            else next.delete('q')
            next.delete('search')
            setParams(next, { replace: true })
          }}
        >
          <IconSearch size={20} />
          <input name="q" defaultValue={params.get('q') ?? ''} placeholder="Search series, movies, genres" autoFocus />
        </form>
      )}
      <div className="browse">
        <nav className="browse__cats" aria-label="Browse categories">
          {ready ? (
            <div ref={listRef} className="browse__list" onKeyDown={onListKey}>
              {cats.map((c, i) => {
                const on = c.key === activeKey
                return (
                  <span key={c.key} className="browse__item">
                    {c.genre && !cats[i - 1]?.genre && <span className="browse__group">Genres</span>}
                    <button className={'browse__cat' + (on ? ' browse__cat--on' : '')} aria-pressed={on} tabIndex={on || (activeKey == null && i === 0) ? 0 : -1} onClick={() => choose(c.key)}>
                      {c.label}
                    </button>
                  </span>
                )
              })}
            </div>
          ) : (
            <div className="browse__list" aria-hidden>
              {Array.from({ length: 9 }).map((_, i) => (
                <span key={i} className="browse__item">
                  <span className="bone browse__bone" />
                </span>
              ))}
            </div>
          )}
        </nav>
        <section className="browse__main" aria-live="polite">
          {ready && (
            <div className="browse__head">
              <h2>{heading}</h2>
              <span>{shown.length === 1 ? '1 title' : `${shown.length} titles`}</span>
            </div>
          )}
          {loading && !data ? (
            <GridSkeleton />
          ) : error && !data ? (
            <div className="empty error">{error}</div>
          ) : shown.length === 0 ? (
            <div className="empty">{q ? `No matches for "${params.get('q')}".` : 'Nothing here yet.'}</div>
          ) : (
            <div className="grid browse__grid">
              {shown.map((it) => (
                <TitleCard key={it.id} item={it} width={220} sub={subLine(it)} />
              ))}
            </div>
          )}
        </section>
      </div>
      <Footer />
    </div>
  )
}
