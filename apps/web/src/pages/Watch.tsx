import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { api, px } from '../api/client'
import { ageLabel, audioBadge, episodeNumber, isEpisode, isMovie, isPlayable, languageName, primarySource, sortEpisodes, timeLeftLabel } from '../api/items'
import type { Item } from '../api/types'
import { useAsync } from '../hooks/useAsync'
import { TopBar } from '../components/TopBar'
import { Footer } from '../components/Footer'
import { Art, ProgressBar, SaveButton } from '../components/Cards'
import { IconPlay, IconReplay } from '../components/Icons'
import { Spinner } from '../components/Skeleton'
import { Player } from '../player/Player'

/**
 * The watch page, laid out like Crunchyroll's: a full-width dark band with the player;
 * under it the series name in blue, "E2 – Title", "16+ • Sub | Dub", the air date, the
 * synopsis and SHOW MORE, which opens the Audio / Subtitles / Content Advisory table;
 * NEXT EPISODE, PREVIOUS EPISODE and SEE MORE EPISODES at the right. Autoplay moves
 * to the next episode when one ends.
 */
export function Watch() {
  const { id = '' } = useParams()
  const nav = useNavigate()
  const { data: item, error, loading } = useAsync(`item:${id}:full`, () => api.item(id), [id])
  const seriesId = item?.seriesId ?? null
  const { data: series } = useAsync(seriesId ? `item:${seriesId}` : null, () => api.item(seriesId!), [seriesId])
  const { data: sibsRes } = useAsync(seriesId ? `episodes-all:${seriesId}` : null, () => api.items({ type: 'episode', parent: seriesId!, recursive: true, includeSources: true, sort: 'index' }), [seriesId])
  const siblings = useMemo(() => sortEpisodes(sibsRes?.items ?? []), [sibsRes])
  const [more, setMore] = useState(false)
  useEffect(() => setMore(false), [id])

  if (loading && !item) return <Spinner />
  if (!item) return <div className="empty error">{error ?? 'Not found'}</div>
  if (!isPlayable(item)) return <div className="empty">This title has no file to play yet.</div>

  const at = siblings.findIndex((e) => e.id === item.id)
  const next = at >= 0 ? (siblings.slice(at + 1).find(isPlayable) ?? null) : null
  const prev = at > 0 ? (siblings.slice(0, at).reverse().find(isPlayable) ?? null) : null
  const rating = item.officialRating ?? series?.officialRating
  const movie = isMovie(item)
  const heading = movie ? item.title : [episodeNumber(item), item.title].filter(Boolean).join(' – ')
  const advisories = item.advisories?.length ? item.advisories : (series?.advisories ?? [])
  const src = primarySource(item)
  const audio = Array.from(new Set((src?.streams ?? []).filter((s) => s.type === 'audio').map((s) => s.language ?? '').filter((l) => l && l !== 'und')))
  const subs = Array.from(new Set((src?.streams ?? []).filter((s) => s.type === 'subtitle').map((s) => s.detectedLanguage || s.language || '').filter((l) => l && l !== 'und')))

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
          {item.overview && <p className={'watch__synopsis' + (more ? '' : ' watch__synopsis--clamped')}>{item.overview}</p>}
          {more && (
            <div className="kv">
              {audio.length > 0 && (
                <div className="kv__row">
                  <b>Audio</b>
                  <span>{audio.map(languageName).join(', ')}</span>
                </div>
              )}
              {subs.length > 0 && (
                <div className="kv__row">
                  <b>Subtitles</b>
                  <span>{subs.map(languageName).join(', ')}</span>
                </div>
              )}
              {rating && (
                <div className="kv__row">
                  <b>Content Advisory</b>
                  <span>
                    {ageLabel(rating)}
                    {advisories.length > 0 && (
                      <>
                        <br />
                        {advisories.join(', ')}
                      </>
                    )}
                  </span>
                </div>
              )}
              {src?.container && (
                <div className="kv__row">
                  <b>File</b>
                  <span>
                    {[src.streams?.find((s) => s.type === 'video')?.codec?.toUpperCase(), src.streams?.find((s) => s.type === 'video')?.height ? `${src.streams.find((s) => s.type === 'video')!.height}p` : null, src.container.toUpperCase(), src.size ? `${(src.size / 1073741824).toFixed(2)} GB` : null].filter(Boolean).join(' · ')}
                  </span>
                </div>
              )}
            </div>
          )}
          <button className="link" style={{ marginTop: 14 }} onClick={() => setMore((m) => !m)}>
            {more ? 'Show less' : 'Show more'}
          </button>
        </div>
        <aside className="watch__side">
          {next && <SideEpisode label="Next episode" ep={next} />}
          {prev && <SideEpisode label="Previous episode" ep={prev} />}
          {item.seriesId && (
            <Link to={`/series/${item.seriesId}`} className="btn btn--outline">
              See more episodes
            </Link>
          )}
        </aside>
      </div>
      <Footer />
    </div>
  )
}

/** The small episode card in the right column: still with time left, "E3 – Title", "Dub | Sub". */
function SideEpisode({ label, ep }: { label: string; ep: Item }) {
  const left = timeLeftLabel(ep)
  const watched = !!ep.userData?.played && !left
  return (
    <div className="upnext-block">
      <h3>{label}</h3>
      <Link to={`/watch/${ep.id}`} className="upnext">
        <div className="ep__art">
          <Art url={api.imageUrl(ep, 'thumb', px(320))} title={ep.title} />
          <div className={watched ? 'watched-ring' : 'play-ring'}>{watched ? <IconReplay size={24} /> : <IconPlay size={24} />}</div>
          {watched ? <span className="badge badge--br">Watched</span> : left ? <span className="badge badge--br">{left}</span> : null}
          <ProgressBar item={ep} />
        </div>
        <div>
          <div className="ep__name">{[episodeNumber(ep), ep.title].filter(Boolean).join(' – ')}</div>
          <div className="ep__sub">
            <span>{audioBadge(ep) ?? ''}</span>
          </div>
        </div>
      </Link>
    </div>
  )
}

function formatDate(iso: string): string {
  const d = new Date(iso)
  return isNaN(d.getTime()) ? iso : d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}
