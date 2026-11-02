import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { api, px } from '../api/client'
import { audioBadge, isPlayable, runtimeLabel, timeLeftLabel } from '../api/items'
import type { Item } from '../api/types'
import { useAsync } from '../hooks/useAsync'
import { Art, ProgressBar, Row, SaveButton, TitleCard, isWatched } from '../components/Cards'
import { IconPlay, IconReplay } from '../components/Icons'
import { SectionTabs, TitleBanner, TitleBelow } from '../components/TitlePage'
import { Spinner } from '../components/Skeleton'
import { TopBar } from '../components/TopBar'
import { Footer } from '../components/Footer'

/** A film: the same frame as a series, the film as one card under a "Movie" tab, and "Part of" links. */
export function Movie() {
  const { id = '' } = useParams()
  const nav = useNavigate()
  const { data: movie, error, loading, reload } = useAsync(`item:${id}`, () => api.item(id), [id])
  const { data: related } = useAsync(`related:${id}`, () => api.related(id).catch(() => ({ related: [], partOf: [] })), [id])
  const [tab, setTab] = useState(0)
  const { data: similar } = useAsync(tab === 1 ? `similar:${id}` : null, () => api.similar(id), [id])

  useEffect(() => {
    const onFocus = () => reload(true)
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [reload])

  if (loading && !movie) return <Spinner />
  if (!movie) return <div className="empty error">{error ?? 'Not found'}</div>

  const playable = isPlayable(movie)
  const resumable = (movie.userData?.positionMs ?? 0) > 0
  const partOf = (related?.partOf ?? []) as Item[]
  const left = timeLeftLabel(movie)
  const relatedItems = (related?.related ?? []).map((r) => r.item).filter((x): x is Item => !!x)

  return (
    <div className="page page--phone">
      <TopBar phone="title" />
      <TitleBanner
        item={movie}
        badge={audioBadge(movie)}
        extra={runtimeLabel(movie)}
        actions={
          <>
            <button className="btn btn--brand" disabled={!playable} onClick={() => nav(`/watch/${movie.id}`)}>
              <IconPlay size={20} /> {playable ? (resumable ? 'Resume' : 'Start watching') : movie.availability === 'unaired' ? 'Not out yet' : 'Not available'}
            </button>
            <SaveButton item={movie} onChanged={() => reload(true)} />
          </>
        }
      />
      <TitleBelow item={movie}>
        {partOf.length > 0 && (
          <div className="muted" style={{ marginTop: 14 }}>
            Part of{' '}
            {partOf.map((p, i) => (
              <span key={p.id}>
                {i > 0 && ', '}
                <Link className="link link--plain" to={`/series/${p.id}`}>
                  {p.title}
                </Link>
              </span>
            ))}
          </div>
        )}
      </TitleBelow>
      <SectionTabs tabs={['Movie', 'More like this']} selected={tab} onSelect={setTab} />
      {tab === 0 ? (
        <>
          <div className="grid grid--episodes" style={{ marginTop: 28 }}>
            <div className={'ep' + (playable ? '' : ' ep--unavailable')}>
              <Link to={playable ? `/watch/${movie.id}` : '#'}>
                <div className="ep__art">
                  <Art url={api.imageUrl(movie, 'backdrop', px(420)) ?? api.imageUrl(movie, 'poster', px(420))} title={movie.title} />
                  {playable &&
                    (isWatched(movie) ? (
                      <div className="watched-overlay">
                        <IconReplay size={48} />
                      </div>
                    ) : (
                      <div className="play-ring">
                        <IconPlay size={28} />
                      </div>
                    ))}
                  {isWatched(movie) ? <span className="badge badge--br">Watched</span> : (left ?? runtimeLabel(movie)) && <span className="badge badge--br">{left ?? runtimeLabel(movie)}</span>}
                  <ProgressBar item={movie} />
                </div>
                <div className="ep__name">{movie.title}</div>
              </Link>
              <div className="ep__sub">
                <span>{audioBadge(movie) ?? ''}</span>
              </div>
            </div>
          </div>
          {relatedItems.length > 0 && (
            <Row title="Related">
              {relatedItems.map((it) => (
                <TitleCard key={it.id} item={it} />
              ))}
            </Row>
          )}
        </>
      ) : (
        <div className="grid" style={{ marginTop: 28 }}>
          {(similar?.items ?? []).map((c) => {
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
      <Footer />
    </div>
  )
}
