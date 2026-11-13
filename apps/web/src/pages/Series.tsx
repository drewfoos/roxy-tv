import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { api, px } from '../api/client'
import { audioBadge, episodeCode, isPlayable, nextToPlay, runtimeLabel, seriesPlayLabel, sortEpisodes, timeLeftLabel } from '../api/items'
import type { Item, ItemsResponse } from '../api/types'
import { useAsync } from '../hooks/useAsync'
import { Art, CardMenu, ProgressBar, SaveButton, isWatched, useWatchedState, withPlayed } from '../components/Cards'
import { IconChevronLeft, IconChevronRight, IconExpand, IconPlay, IconReplay } from '../components/Icons'
import { SectionTabs, TitleBanner, TitleBelow } from '../components/TitlePage'
import { GridSkeleton, Spinner } from '../components/Skeleton'
import { LoadError } from '../components/Connection'
import { TopBar } from '../components/TopBar'
import { Footer } from '../components/Footer'
import { DiscoverGrid } from '../components/DiscoverBits'
import { ExtrasRow } from '../components/Extras'
import { entryCompleteness, entryRows, plainEntries, rowNumber, sectionEntries, type SeasonEntry } from '../api/sections'

/**
 * A series, laid out like Crunchyroll's: the banner with the info over its left side,
 * the synopsis and languages under it, then the season dropdown with a sort, and a grid
 * of episode cards. Play picks the episode to start the way the phone does.
 */
export function Series() {
  const { id = '' } = useParams()
  const nav = useNavigate()
  const { data: series, error, loading, reload } = useAsync(`item:${id}`, () => api.item(id), [id])
  const { data: seasonsRes, error: seasonsError, loading: seasonsLoading, reload: reloadSeasons } = useAsync(`seasons:${id}`, () => api.items({ type: 'season', parent: id, sort: 'index' }), [id])
  // The season list in release order: the seasons, the specials between them ("OVAs") and
  // the franchise's films, as entries of their own (the plain list from an older server).
  // A failed read (or an older server without it) falls back to the plain list. It is not
  // cached as "no sections": a passing failure asked again on the next visit, and that
  // visit's picker swapped lists under the viewer when the silent refresh worked.
  const { data: sections, loading: sectionsLoading, reload: reloadSections } = useAsync(`sections:${id}`, () => api.sections(id), [id])
  const seasonsSettled = seasonsRes != null && !(sectionsLoading && sections == null)
  const seasons: SeasonEntry[] = useMemo(() => {
    if (!seasonsSettled) return []
    const list = seasonsRes?.items ?? []
    return sectionEntries(sections, list) ?? plainEntries(list)
  }, [seasonsRes, sections, seasonsSettled])
  // The picked entry belongs to its show: another show starts on its first entry from its
  // first render (an effect resetting it ran a render late and read the wrong season once).
  // Held by the entry's key (stable across a list read again, see sectionKey), with its
  // place as the fallback: a list that changed under the viewer keeps the same entry.
  const [picked, setPicked] = useState<{ id: string; key?: string; index: number }>({ id, index: 0 })
  const byKey = picked.id === id && picked.key ? seasons.findIndex((s) => s.key === picked.key) : -1
  const seasonIndex = picked.id !== id ? 0 : byKey >= 0 ? byKey : Math.min(picked.index, Math.max(0, seasons.length - 1))
  const setSeasonIndex = (f: number | ((i: number) => number)) => {
    const index = typeof f === 'function' ? f(seasonIndex) : f
    setPicked({ id, key: seasons[index]?.key, index })
  }
  const [newest, setNewest] = useState(false)
  const [tab, setTab] = useState(0)

  // The season list is known once both reads have answered: the plain list first and the
  // release-order one a moment later reshuffled the picker (and read a season twice).
  const listReady = seasonsSettled
  const flat = listReady && seasons.length === 0
  const season = listReady ? seasons[seasonIndex] : undefined
  // Entry keys are not unique across shows (a show's plain seasons aside): the key names the
  // show, or a film entry of one show was served from another's cache.
  const whole = flat
  const epsKey = !listReady ? null : whole ? `episodes:${id}:whole` : season ? `episodes:${id}:${season.key}` : null
  const { data: epsRes, loading: epsLoading, error: epsError, reload: reloadEpisodes } = useAsync(
    epsKey,
    () =>
      // A film of the franchise: its one row (with its file, to play).
      season?.movie && !whole
        ? api.item(season.movie.id).then((it) => ({ items: [it] }))
        : api.allItems({ type: 'episode', parent: whole ? id : season!.seasonId!, recursive: whole, includeSources: true, sort: 'index' }),
    [epsKey],
  )
  const episodes = useMemo(() => {
    const items = epsRes?.items ?? []
    if (season?.movie && !whole) return items
    // A group of specials: its own episodes, in its order.
    return season?.ids && !whole ? entryRows(season, items) : sortEpisodes(items)
  }, [epsRes, season, whole])
  const shown = useMemo(() => (newest ? episodes.slice().reverse() : episodes), [episodes, newest])
  // Part of the list did not load (the read was cut short, or a group of specials is missing
  // one it names): the page says so instead of a partial list passing for the whole.
  const retryEpisodes = () => {
    if (seasonsRes == null) return reloadSeasons()
    if (season?.ids) reloadSections()
    reloadEpisodes()
  }
  // Nothing to show yet: the season list or this entry's episodes are still on their way.
  const listFailed = seasonsRes == null && !!seasonsError && !seasonsLoading
  const completeness = entryCompleteness(season?.movie || whole ? undefined : season, episodes, epsRes as ItemsResponse | undefined)
  // Play looks across every season, like the apps, not just the one picked in the dropdown.
  // What Play plays, worked out by the server (it used to need every episode of the show).
  const { data: nextRes, error: nextError, loading: nextLoading, reload: reloadNext } = useAsync(seasonsRes == null ? null : `next:${id}`, () => api.next(id), [id, seasonsRes == null])

  const { data: similar } = useAsync(tab === 1 ? `similar:${id}` : null, () => api.similar(id), [id])

  useEffect(() => {
    // Back from another tab (watched there): the show and what Play plays.
    const onFocus = () => { reload(true); reloadNext(true) }
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [reload, reloadNext])

  if (loading && !series) return <Spinner />
  if (!series) return <div className="page"><TopBar /><LoadError error={error ?? 'Not found'} onRetry={() => reload()} /></div>

  // The server's answer; when it could not be had, the open season's own pick (useAsync's
  // data is null, never undefined, so the old fallback never ran: Play said "Nothing to
  // play yet" over a list of episodes).
  const next = nextError && nextRes == null ? nextToPlay(episodes) : nextRes
  const label = seriesPlayLabel(next, epsLoading || nextLoading || seasonsRes == null)
  const seasonCount = series.seasonCount ?? seasons.filter((s) => !s.ids && !s.movie).length

  return (
    <div className="page page--phone">
      <TopBar phone="title" />
      <TitleBanner
        item={series}
        badge={audioBadge(series) ?? audioBadge(episodes.find(isPlayable))}
        extra={seasonCount > 0 ? (seasonCount === 1 ? '1 Season' : `${seasonCount} Seasons`) : null}
        actions={
          <>
            <button className="btn btn--brand" disabled={!next} aria-busy={label == null} onClick={() => next && nav(`/watch/${next.id}`)}>
              <IconPlay size={20} /> {label ?? <span className="btn__pending" data-pending="play" />}
            </button>
            <SaveButton item={series} onChanged={() => reload(true)} />
          </>
        }
      />
      <TitleBelow item={series} />
      <SectionTabs tabs={['Episodes', 'More like this']} selected={tab} onSelect={setTab} />
      {tab === 0 ? (
        <>
          <div className="seasonrow">
            {seasons.length > 0 ? (
              <SeasonSelect seasons={seasons} index={Math.min(seasonIndex, seasons.length - 1)} onSelect={setSeasonIndex} />
            ) : !listReady && !listFailed ? (
              // Not read yet: a line where the season's name will be, never the series'
              // title that then turns into "Season 1".
              <div className="bone bone--line" data-pending="season" style={{ width: 130, height: 20 }} />
            ) : (
              <span style={{ fontWeight: 700, fontSize: 17 }}>{series.title}</span>
            )}
            <div className="seasonrow__spacer" />
            <button className="seasonrow__tool" onClick={() => setNewest((n) => !n)}>
              <IconExpand size={18} style={{ transform: newest ? 'rotate(180deg)' : undefined }} /> {newest ? 'Newest' : 'Oldest'}
            </button>
          </div>
          {/* Three states: loaded; part loaded (the list says how much, Try again reads it
              again); failed (Try again). Try again reads this list only, not the page; for a
              group of specials the season list too, since it names them. */}
          {!epsLoading && epsRes && !epsError && !completeness.complete && (
            <div className="notice notice--action" style={{ margin: '0 var(--gutter) 16px' }}>
              <span>Some episodes could not be loaded ({episodes.length} of {completeness.expected}).</span>
              <button className="btn btn--grey" onClick={retryEpisodes}>Try again</button>
            </div>
          )}
          {!epsLoading && epsError && (
            <div className="notice notice--error notice--action" style={{ margin: '0 var(--gutter) 16px' }}>
              <span>{epsRes ? 'Could not refresh the episodes; showing the last ones loaded.' : "Couldn't load the episodes."}</span>
              <button className="btn btn--grey" onClick={retryEpisodes}>Try again</button>
            </div>
          )}
          {listFailed && (
            <div className="notice notice--error notice--action" style={{ margin: '0 var(--gutter) 16px' }}>
              <span>Couldn't load the seasons.</span>
              <button className="btn btn--grey" onClick={retryEpisodes}>Try again</button>
            </div>
          )}
          {listFailed ? null : (epsLoading || !listReady) && !epsRes ? (
            <GridSkeleton thumbs count={10} />
          ) : episodes.length === 0 && epsError ? null : episodes.length === 0 ? (
            <div className="empty">No episodes in this season.</div>
          ) : (
            <div className="grid grid--episodes">
              {shown.map((ep) => (
                <EpisodeCard key={ep.id} ep={ep} seriesTitle={series.title} number={whole ? undefined : rowNumber(season, episodes, ep)} />
              ))}
            </div>
          )}
          {seasons.length > 1 && (
            <div className="seasonnav">
              <button className="seasonnav__btn" disabled={seasonIndex <= 0} onClick={() => setSeasonIndex((i) => i - 1)}>
                <IconChevronLeft size={20} /> Previous season
              </button>
              <button className="seasonnav__btn" disabled={seasonIndex >= seasons.length - 1} onClick={() => setSeasonIndex((i) => i + 1)}>
                Next season <IconChevronRight size={20} />
              </button>
            </div>
          )}
          <ExtrasRow id={id} />
        </>
      ) : (
        <div style={{ marginTop: 28 }}>
          {!similar ? (
            <GridSkeleton count={12} />
          ) : similar.items.length === 0 ? (
            <div className="empty">Nothing to suggest for this title yet.</div>
          ) : (
            <DiscoverGrid items={similar.items} />
          )}
        </div>
      )}
      <Footer />
    </div>
  )
}

function seasonName(s: SeasonEntry): string {
  return s.title
}

/**
 * Crunchyroll's season picker: a caret and the season name in the brand colour; clicking
 * it drops a dark panel of rows, each the season name (cut with an ellipsis) and its
 * episode count at the right, the current one in white. Counts load once it opens.
 */
function SeasonSelect({ seasons, index, onSelect }: { seasons: SeasonEntry[]; index: number; onSelect: (i: number) => void }) {
  const [open, setOpen] = useState(false)
  const [counts, setCounts] = useState<Record<string, number>>({})
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false)
    }
    const key = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    document.addEventListener('mousedown', close)
    document.addEventListener('keydown', key)
    return () => {
      document.removeEventListener('mousedown', close)
      document.removeEventListener('keydown', key)
    }
  }, [open])
  useEffect(() => {
    if (!open) return
    let live = true
    for (const s of seasons) {
      // The server's count when it gave one; a film says "Film" instead.
      if (counts[s.key] != null || s.count != null || s.label || !s.seasonId) continue
      api
        .items({ type: 'episode', parent: s.seasonId, limit: 1 })
        .then((r) => live && r.total != null && setCounts((c) => ({ ...c, [s.key]: r.total! })))
        .catch(() => undefined)
    }
    return () => {
      live = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, seasons])
  const current = seasons[index]
  return (
    <div ref={ref} className="seasonsel">
      <button className="seasonsel__btn" onClick={() => setOpen((o) => !o)} aria-haspopup="listbox" aria-expanded={open} aria-label="Seasons">
        <IconExpand size={18} />
        <span>{current ? seasonName(current) : ''}</span>
      </button>
      {open && (
        <div className="seasonsel__menu" role="listbox">
          {seasons.map((s, i) => {
            const n = s.count ?? counts[s.key]
            return (
              <button
                key={s.key}
                role="option"
                aria-selected={i === index}
                className={'seasonsel__row' + (i === index ? ' seasonsel__row--on' : '')}
                onClick={() => {
                  onSelect(i)
                  setOpen(false)
                }}
              >
                <span className="seasonsel__name">{seasonName(s)}</span>
                {s.label ? <span className="seasonsel__count">{s.label}</span> : n != null && <span className="seasonsel__count">{n === 1 ? '1 Episode' : `${n} Episodes`}</span>}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}

/**
 * Episode card (Crunchyroll's): 16:9 still with a play ring, or a dark overlay with a
 * replay arrow and a "Watched" pill, "23m left" and the progress bar; under it the
 * series name in tiny capitals, "E1 – Title", then "Dub | Sub" with the menu. Marking
 * it watched from the menu redraws the card at once.
 */
export function EpisodeCard({ ep: given, seriesTitle, number }: { ep: Item; seriesTitle?: string; number?: number | null }) {
  const [played, setPlayed] = useWatchedState(given)
  const ep = useMemo(() => withPlayed(given, played), [given, played])
  const playable = isPlayable(ep)
  const left = timeLeftLabel(ep)
  const watched = isWatched(ep)
  // The caller's number when it has one (a group of specials numbers by place; a film, none).
  const n = number !== undefined ? number : (ep.indexAired ?? ep.indexAbsolute)
  const art = (
    <div className="ep__art">
      {/* A film's row (a film entry of the season list) has no still: its backdrop, else its poster. */}
      <Art url={api.imageUrl(ep, 'thumb', px(420)) ?? api.imageUrl(ep, 'backdrop', px(420)) ?? api.imageUrl(ep, 'poster', px(420))} title={episodeCode(ep) || ep.title} />
      {playable &&
        (watched ? (
          <div className="watched-overlay">
            <IconReplay size={48} />
          </div>
        ) : (
          <div className="play-ring">
            <IconPlay size={28} />
          </div>
        ))}
      {watched ? <span className="badge badge--br">Watched</span> : (left ?? runtimeLabel(ep)) ? <span className="badge badge--br">{left ?? runtimeLabel(ep)}</span> : null}
      {!playable && <span className="badge badge--tl">{ep.availability === 'unaired' ? 'Not out yet' : 'Missing'}</span>}
      <ProgressBar item={ep} />
    </div>
  )
  const caption = (
    <>
      <div className="ep__series">{seriesTitle ?? ep.seriesTitle ?? ''}</div>
      <div className="ep__name">{n != null ? `E${n} – ${ep.title}` : ep.title}</div>
    </>
  )
  return (
    <div className={'ep' + (playable ? '' : ' ep--unavailable')}>
      {playable ? (
        <Link to={`/watch/${ep.id}`}>
          {art}
          {caption}
        </Link>
      ) : (
        <>
          {art}
          {caption}
        </>
      )}
      <div className="ep__sub">
        <span>{audioBadge(ep) ?? runtimeLabel(ep) ?? ''}</span>
        {playable && <CardMenu item={ep} played={played} onPlayedChange={setPlayed} />}
      </div>
    </div>
  )
}
