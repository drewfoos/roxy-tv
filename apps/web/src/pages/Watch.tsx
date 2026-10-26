import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { api, px } from '../api/client'
import { ageLabel, audioBadge, episodeNumber, isEpisode, isMovie, isPlayable, sortEpisodes } from '../api/items'
import { useAsync } from '../hooks/useAsync'
import { TopBar } from '../components/TopBar'
import { Art, SaveButton } from '../components/Cards'
import { IconPlay, IconReplay } from '../components/Icons'
import { Spinner } from '../components/Skeleton'
import { Player } from '../player/Player'

/**
 * The watch page, laid out like Crunchyroll's: a full-width dark band with the player
 * in it; under it the series name in blue, "E1 – Title", "16+ • Sub | Dub", the air
 * date and the synopsis; NEXT EPISODE and SEE MORE EPISODES at the right. Autoplay
 * moves to the next episode when one ends.
 */
export function Watch() {
  const { id = '' } = useParams()
  const nav = useNavigate()
  const { data: item, error, loading } = useAsync(`item:${id}:full`, () => api.item(id), [id])
  const seriesId = item?.seriesId ?? null
  const { data: series } = useAsync(seriesId ? `item:${seriesId}` : null, () => api.item(seriesId!), [seriesId])
  const { data: sibsRes } = useAsync(seriesId ? `episodes-all:${seriesId}` : null, () => api.items({ type: 'episode', parent: seriesId!, recursive: true, includeSources: true, sort: 'index' }), [seriesId])
  const siblings = useMemo(() => sortEpisodes(sibsRes?.items ?? []), [sibsRes])
  const [expanded, setExpanded] = useState(false)
  useEffect(() => setExpanded(false), [id])

  if (loading && !item) return <Spinner />
  if (!item) return <div className="empty error">{error ?? 'Not found'}</div>
  if (!isPlayable(item)) return <div className="empty">This title has no file to play yet.</div>

  const at = siblings.findIndex((e) => e.id === item.id)
  const next = at >= 0 ? (siblings.slice(at + 1).find(isPlayable) ?? null) : null
  const rating = item.officialRating ?? series?.officialRating
  const movie = isMovie(item)
  const heading = movie ? item.title : [episodeNumber(item), item.title].filter(Boolean).join(' – ')
  const advisories = item.advisories?.length ? item.advisories : (series?.advisories ?? [])
  const long = (item.overview ?? '').length > 320

  return (
    <div className="page">
      <TopBar />
      <div className="watch__band">
        <Player item={item} series={series} next={next} onNext={() => next && nav(`/watch/${next.id}`)} onEnded={() => next && nav(`/watch/${next.id}`, { replace: true })} />
      </div>
      <div className="watch">
        <div>
          <div style={{ display: 'flex', alignItems: 'flex-start', gap: 16 }}>
            <div style={{ flex: 1, minWidth: 0 }}>
              {isEpisode(item) && item.seriesId ? (
                <Link to={`/series/${item.seriesId}`} className="watch__series">
                  {item.seriesTitle ?? series?.title ?? 'Series'}
                </Link>
              ) : (
                <span className="watch__series">Movie</span>
              )}
              <h1 className="watch__title">{heading}</h1>
              <div className="watch__facts">
                {rating && <span className="age">{ageLabel(rating)}</span>}
                {audioBadge(item) && <span>• {audioBadge(item)}</span>}
              </div>
              {item.airDate && <div className="watch__date">Released on {formatDate(item.airDate)}</div>}
            </div>
            {series && <SaveButton item={series} />}
          </div>
          {item.overview && (
            <>
              <p className={'watch__synopsis' + (expanded || !long ? '' : ' title__synopsis--clamped')}>{item.overview}</p>
              {long && (
                <button className="link" style={{ marginTop: 10 }} onClick={() => setExpanded((e) => !e)}>
                  {expanded ? 'Show less' : 'Show more'}
                </button>
              )}
            </>
          )}
          {rating && (
            <div className="title__kv" style={{ marginTop: 20 }}>
              <b>Content advisory:</b> {ageLabel(rating)}
              {advisories.length ? ` • ${advisories.join(', ')}` : ''}
            </div>
          )}
        </div>
        <aside className="watch__side">
          {next && (
            <>
              <h3>Next episode</h3>
              <Link to={`/watch/${next.id}`} className="upnext">
                <div className="ep__art">
                  <Art url={api.imageUrl(next, 'thumb', px(320))} title={next.title} />
                  <div className="play-ring">{next.userData?.played ? <IconReplay size={24} /> : <IconPlay size={24} />}</div>
                </div>
                <div>
                  <div className="ep__name">{[episodeNumber(next), next.title].filter(Boolean).join(' – ')}</div>
                  <div className="ep__sub">
                    <span>{audioBadge(next) ?? ''}</span>
                  </div>
                </div>
              </Link>
            </>
          )}
          {item.seriesId && (
            <Link to={`/series/${item.seriesId}`} className="btn btn--outline">
              See more episodes
            </Link>
          )}
        </aside>
      </div>
    </div>
  )
}

function formatDate(iso: string): string {
  const d = new Date(iso)
  return isNaN(d.getTime()) ? iso : d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}
