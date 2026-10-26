import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { api, px } from '../api/client'
import { audioBadge, episodeCode, isPlayable, nextToPlay, runtimeLabel, sortEpisodes, timeLeftLabel } from '../api/items'
import type { Item } from '../api/types'
import { useAsync } from '../hooks/useAsync'
import { Art, CardMenu, ProgressBar, Row, SaveButton, TitleCard } from '../components/Cards'
import { IconExpand, IconPlay, IconReplay } from '../components/Icons'
import { SectionTabs, TitleBanner, TitleBelow } from '../components/TitlePage'
import { GridSkeleton, Spinner } from '../components/Skeleton'
import { TopBar } from '../components/TopBar'

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

  const { data: related } = useAsync(`related:${id}`, () => api.related(id).catch(() => ({ related: [] })), [id])
  const { data: similar } = useAsync(tab === 1 ? `similar:${id}` : null, () => api.similar(id), [id])

  useEffect(() => {
    const onFocus = () => reload(true)
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [reload])

  if (loading && !series) return <Spinner />
  if (!series) return <div className="empty error">{error ?? 'Not found'}</div>

  const next = nextToPlay(episodes)
  const nextEp = next ? (next.indexAired ?? next.indexAbsolute) : null
  const resumable = next ? (next.userData?.positionMs ?? 0) > 0 : false
  const label = !next ? (epsLoading || seasonsRes == null ? 'Play' : 'Nothing to play yet') : resumable ? `Continue watching${nextEp != null ? ` E${nextEp}` : ''}` : `Start watching${nextEp != null ? ` E${nextEp}` : ''}`
  const seasonCount = series.seasonCount ?? seasons.length
  const relatedItems = (related?.related ?? []).map((r) => r.item).filter((x): x is Item => !!x)

  return (
    <div className="page">
      <TopBar />
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
              <>
                <IconExpand size={22} />
                <select value={season?.id ?? ''} onChange={(e) => setSeasonIndex(seasons.findIndex((s) => s.id === e.target.value))} aria-label="Season">
                  {seasons.map((s, i) => (
                    <option key={s.id} value={s.id}>
                      {s.title || `Season ${s.indexAired ?? i + 1}`}
                    </option>
                  ))}
                </select>
              </>
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
            <div className="grid">
              {similar.items.map((c) => {
                const to = c.libraryItemId ? (c.type === 'movie' ? `/movie/${c.libraryItemId}` : `/series/${c.libraryItemId}`) : null
                const body = (
                  <>
                    <div className="card__art">{c.poster ? <img className="card__img" src={c.poster} alt="" loading="lazy" /> : <div className="art-fallback">{c.title}</div>}</div>
                    <div className="card__name">{c.title}</div>
                    <div className="card__sub">
                      <span>{[c.year, c.inLibrary ? 'In your library' : null].filter(Boolean).join('  ·  ')}</span>
                    </div>
                  </>
                )
                return to ? (
                  <Link key={`${c.type}:${c.tmdbId}`} to={to} className="card">
                    {body}
                  </Link>
                ) : (
                  <div key={`${c.type}:${c.tmdbId}`} className="card">
                    {body}
                  </div>
                )
              })}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

/**
 * Episode card (Crunchyroll's): 16:9 still with a play ring, or a replay ring and a
 * "Watched" pill, "23m left" and the progress bar; under it the series name in tiny
 * capitals, "E1 – Title", then "Dub | Sub" with the menu.
 */
export function EpisodeCard({ ep, seriesTitle }: { ep: Item; seriesTitle?: string }) {
  const playable = isPlayable(ep)
  const left = timeLeftLabel(ep)
  const watched = !!ep.userData?.played && !left
  const n = ep.indexAired ?? ep.indexAbsolute
  const art = (
    <div className="ep__art">
      <Art url={api.imageUrl(ep, 'thumb', px(420))} title={episodeCode(ep) || ep.title} />
      {playable &&
        (watched ? (
          <div className="watched-ring">
            <IconReplay size={28} />
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
        {playable && <CardMenu item={ep} />}
      </div>
    </div>
  )
}
