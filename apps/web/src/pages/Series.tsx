import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { api, px } from '../api/client'
import { audioBadge, episodeCode, isPlayable, nextToPlay, playLabel, runtimeLabel, sortEpisodes, timeLeftLabel } from '../api/items'
import type { Item } from '../api/types'
import { useAsync } from '../hooks/useAsync'
import { Art, CardMenu, ProgressBar, Row, SaveButton, TitleCard, isWatched, useWatchedState, withPlayed } from '../components/Cards'
import { IconChevronLeft, IconChevronRight, IconExpand, IconPlay, IconReplay } from '../components/Icons'
import { SectionTabs, TitleBanner, TitleBelow } from '../components/TitlePage'
import { GridSkeleton, Spinner } from '../components/Skeleton'
import { TopBar } from '../components/TopBar'
import { Footer } from '../components/Footer'
import { DiscoverGrid } from '../components/DiscoverBits'
import { ExtrasRow } from '../components/Extras'

/**
 * A series, laid out like Crunchyroll's: the banner with the info over its left side,
 * the synopsis and languages under it, then the season dropdown with a sort, and a grid
 * of episode cards. Play picks the episode to start the way the phone does.
 */
export function Series() {
  const { id = '' } = useParams()
  const nav = useNavigate()
  const { data: series, error, loading, reload } = useAsync(`item:${id}`, () => api.item(id), [id])
  const { data: seasonsRes } = useAsync(`seasons:${id}`, () => api.items({ type: 'season', parent: id, sort: 'index' }), [id])
  const seasons = useMemo(() => {
    const list = seasonsRes?.items ?? []
    const key = (s: Item) => {
      const n = s.indexAired ?? Number.MAX_SAFE_INTEGER
      return n === 0 ? Number.MAX_SAFE_INTEGER - 1 : n
    }
    return list.slice().sort((a, b) => key(a) - key(b))
  }, [seasonsRes])
  const [seasonIndex, setSeasonIndex] = useState(0)
  const [newest, setNewest] = useState(false)
  const [tab, setTab] = useState(0)
  useEffect(() => setSeasonIndex(0), [id])

  const flat = seasonsRes != null && seasons.length === 0
  const season = seasons[Math.min(seasonIndex, Math.max(0, seasons.length - 1))]
  const epsKey = flat ? `episodes:${id}` : season ? `episodes:${season.id}` : null
  const { data: epsRes, loading: epsLoading } = useAsync(epsKey, () => api.items({ type: 'episode', parent: flat ? id : season!.id, recursive: flat, includeSources: true, sort: 'index' }), [epsKey])
  const episodes = useMemo(() => sortEpisodes(epsRes?.items ?? []), [epsRes])
  const shown = useMemo(() => (newest ? episodes.slice().reverse() : episodes), [episodes, newest])
  // Play looks across every season, like the apps, not just the one picked in the dropdown.
  const allKey = seasonsRes == null ? null : flat ? epsKey : `episodes-all:${id}`
  const { data: allRes, loading: allLoading } = useAsync(allKey, () => api.items({ type: 'episode', parent: id, recursive: true, includeSources: true, sort: 'index' }), [allKey])

  const { data: related } = useAsync(`related:${id}`, () => api.related(id).catch(() => ({ related: [] })), [id])
  const { data: similar } = useAsync(tab === 1 ? `similar:${id}` : null, () => api.similar(id), [id])

  useEffect(() => {
    const onFocus = () => reload(true)
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [reload])

  if (loading && !series) return <Spinner />
  if (!series) return <div className="empty error">{error ?? 'Not found'}</div>

  const next = nextToPlay(allRes?.items ?? episodes)
  const label = playLabel(next, epsLoading || allLoading || seasonsRes == null)
  const seasonCount = series.seasonCount ?? seasons.length
  const relatedItems = (related?.related ?? []).map((r) => r.item).filter((x): x is Item => !!x)

  return (
    <div className="page page--phone">
      <TopBar phone="title" />
      <TitleBanner
        item={series}
        badge={audioBadge(series) ?? audioBadge(episodes.find(isPlayable))}
        extra={seasonCount > 0 ? (seasonCount === 1 ? '1 Season' : `${seasonCount} Seasons`) : null}
        actions={
          <>
            <button className="btn btn--brand" disabled={!next} onClick={() => next && nav(`/watch/${next.id}`)}>
              <IconPlay size={20} /> {label}
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
            ) : (
              <span style={{ fontWeight: 700, fontSize: 17 }}>{series.title}</span>
            )}
            <div className="seasonrow__spacer" />
            <button className="seasonrow__tool" onClick={() => setNewest((n) => !n)}>
              <IconExpand size={18} style={{ transform: newest ? 'rotate(180deg)' : undefined }} /> {newest ? 'Newest' : 'Oldest'}
            </button>
          </div>
          {epsLoading && !epsRes ? (
            <GridSkeleton thumbs count={10} />
          ) : episodes.length === 0 ? (
            <div className="empty">No episodes in this season.</div>
          ) : (
            <div className="grid grid--episodes">
              {shown.map((ep) => (
                <EpisodeCard key={ep.id} ep={ep} seriesTitle={series.title} />
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
          {relatedItems.length > 0 && (
            <Row title="Movies & OVAs">
              {relatedItems.map((it) => (
                <TitleCard key={it.id} item={it} />
              ))}
            </Row>
          )}
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

function seasonName(s: Item, i: number): string {
  return s.title || `Season ${s.indexAired ?? i + 1}`
}

/**
 * Crunchyroll's season picker: a caret and the season name in the brand colour; clicking
 * it drops a dark panel of rows, each the season name (cut with an ellipsis) and its
 * episode count at the right, the current one in white. Counts load once it opens.
 */
function SeasonSelect({ seasons, index, onSelect }: { seasons: Item[]; index: number; onSelect: (i: number) => void }) {
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
      if (counts[s.id] != null) continue
      api
        .items({ type: 'episode', parent: s.id, limit: 1 })
        .then((r) => live && r.total != null && setCounts((c) => ({ ...c, [s.id]: r.total! })))
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
        <span>{current ? seasonName(current, index) : ''}</span>
      </button>
      {open && (
        <div className="seasonsel__menu" role="listbox">
          {seasons.map((s, i) => {
            const n = counts[s.id]
            return (
              <button
                key={s.id}
                role="option"
                aria-selected={i === index}
                className={'seasonsel__row' + (i === index ? ' seasonsel__row--on' : '')}
                onClick={() => {
                  onSelect(i)
                  setOpen(false)
                }}
              >
                <span className="seasonsel__name">{seasonName(s, i)}</span>
                {n != null && <span className="seasonsel__count">{n === 1 ? '1 Episode' : `${n} Episodes`}</span>}
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
export function EpisodeCard({ ep: given, seriesTitle }: { ep: Item; seriesTitle?: string }) {
  const [played, setPlayed] = useWatchedState(given)
  const ep = useMemo(() => withPlayed(given, played), [given, played])
  const playable = isPlayable(ep)
  const left = timeLeftLabel(ep)
  const watched = isWatched(ep)
  const n = ep.indexAired ?? ep.indexAbsolute
  const art = (
    <div className="ep__art">
      <Art url={api.imageUrl(ep, 'thumb', px(420))} title={episodeCode(ep) || ep.title} />
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
