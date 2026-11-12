import { Fragment, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { api } from '../api/client'
import type { DiscoverDetail, DiscoverEpisode, DiscoverFilm, DiscoverSeason, Item, ItemDetails } from '../api/types'
import { useAsync, markStale } from '../hooks/useAsync'
import { useSession } from '../app/useSession'
import { Art, Row } from '../components/Cards'
import { TitleBanner, TitleBelow } from '../components/TitlePage'
import { TopBar } from '../components/TopBar'
import { LoadError } from '../components/Connection'
import { Footer } from '../components/Footer'
import { PhoneTitleHeader } from '../components/PhoneChrome'
import { IconCheck, IconCheckCircle, IconExpand, IconHistory, IconPlay, IconSettings } from '../components/Icons'
import { buttonFilms, filmLine, filmState, filmsSuffix, placeFilms } from './discoverFilms'
import { DownloadBar, DownloadStatusCard, canSearchFor, effectiveState, episodeLine, isActive, isPending, stateColor } from '../components/DiscoverBits'

/**
 * The Discover title page (PhoneDiscoverDetail, PhoneDiscover.kt): a TMDB title in the
 * library title page's frame. Watch when the library has it, Request when it does not
 * (or "Request N more seasons" when it has some), the seasons with what the library
 * holds, the request's state and download progress. While Sonarr / Radarr are not set
 * up there is no Request: the page says so, and admins get a way to the settings.
 */
export function DiscoverTitle() {
  const { type: rawType = 'tv', tmdbId = '' } = useParams()
  const type = rawType === 'movie' ? 'movie' : 'tv'
  const isMovie = type === 'movie'
  const nav = useNavigate()
  const admin = !!useSession()?.isAdmin
  const { data: d, error, loading, reload } = useAsync(`discover:${type}:${tmdbId}`, () => api.discoverDetail(type, tmdbId), [type, tmdbId])
  // A movie has no seasons: its page carries the cast and the facts instead.
  const { data: more } = useAsync(isMovie ? `discover:details:${type}:${tmdbId}` : null, () => api.discoverMoreDetails(type, tmdbId), [type, tmdbId])
  // An anime series' films, placed in its season list by the server (which also says
  // which of them the Request button brings). Asked for apart from the page: the first
  // look reads AniList and can take a while, and the page does not wait for it.
  const filmsKey = !isMovie && d?.isAnime ? `discover:films:${tmdbId}` : null
  const { data: films, reload: reloadFilms } = useAsync(filmsKey, () => api.discoverFilms(tmdbId), [filmsKey])
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)

  useEffect(() => {
    if (!notice) return
    const h = window.setTimeout(() => setNotice(null), 6000)
    return () => window.clearTimeout(h)
  }, [notice])

  // A search was just asked for: poll fast for a minute so its result shows up.
  const [burst, setBurst] = useState(false)
  useEffect(() => {
    if (!burst) return
    const h = window.setTimeout(() => setBurst(false), 60000)
    return () => window.clearTimeout(h)
  }, [burst])

  // Only poll while a request is moving: every few seconds while something downloads or
  // is being searched (or was just asked to), every 15 s while a sent request has no
  // status yet, every 45 s while it is sent but not done. Nothing else.
  const dl = d?.download
  const sent = d?.request?.status
  const pollMs = isActive(dl) || (burst && sent === 'sent') ? 5000 : sent === 'sent' && !dl ? 15000 : isPending(sent, dl) ? 45000 : 0
  useEffect(() => {
    if (!pollMs) return
    const h = window.setInterval(() => {
      if (!document.hidden) reload(true)
    }, pollMs)
    return () => window.clearInterval(h)
  }, [pollMs, reload])

  /** Runs a change, then reloads the page (and lets Discover's rows ask again). */
  const act = async (fn: () => Promise<unknown>, after?: () => void) => {
    if (busy) return
    setBusy(true)
    try {
      await fn()
      after?.()
    } catch (e) {
      setNotice(e instanceof Error ? e.message : String(e))
    }
    markStale('discover')
    reload(true)
    reloadFilms(true)
    setBusy(false)
  }

  if (loading && !d) return <DetailSkeleton />
  if (!d) {
    return (
      <div className="page">
        <TopBar />
        <LoadError title="Couldn't load this title" error={error ?? 'Not found'} onRetry={() => reload()} />
      </div>
    )
  }

  const item = asItem(d)
  const seasons = d.seasons ?? []
  const mainSeasons = seasons.filter((s) => !s.special)
  const missing = mainSeasons.filter((s) => !complete(s) && !s.requested)
  const inLibrary = !!d.libraryItemId
  const req = d.request ?? null
  const reqLive = req != null && req.status !== 'failed' && req.status !== 'declined'
  const arr = isMovie ? 'Radarr' : 'Sonarr'
  const openLibrary = () => d.libraryItemId && nav(isMovie ? `/movie/${d.libraryItemId}` : `/series/${d.libraryItemId}`)
  const request = (seasonNums?: number[], episodes?: string[]) => act(() => api.discoverRequest(type, d.tmdbId, seasonNums, episodes))
  // The page's main button also asks for the films the server put on it; a season's own
  // button and a special's never do.
  const withFilms = isMovie ? [] : buttonFilms(films)
  const requestAll = (seasonNums?: number[]) => act(() => api.discoverRequest(type, d.tmdbId, seasonNums, undefined, withFilms))
  const requestFilm = (f: DiscoverFilm) => f.tmdbId && act(() => api.discoverRequest('movie', f.tmdbId as number))
  const placed = placeFilms(films?.films, seasons.map((s) => s.number))
  const filmRows = (list: DiscoverFilm[] | undefined) =>
    (list ?? []).map((f) => <FilmRow key={f.anilistId} f={f} enabled={!!films?.requestsEnabled} busy={busy} onRequest={() => requestFilm(f)} />)
  // Searches: errors (409 "searched a moment ago…") land in the notice; success polls fast.
  const search = (fn: () => Promise<unknown>) => act(fn, () => setBurst(true))
  // Roxy forgets the request; what Sonarr / Radarr already have stays with them.
  const withdraw = (id: string) => {
    if (!window.confirm(`Remove this request from Roxy? ${arr} keeps the title and anything it is downloading; remove it there to stop it.`)) return
    act(() => api.deleteRequest(id))
  }
  const plural = (n: number) => `${n} season${n === 1 ? '' : 's'}`

  const notSetUp = admin ? (
    <Link to="/account/arr" className="btn btn--grey">
      <IconSettings size={18} /> Set up requests
    </Link>
  ) : (
    <button className="btn btn--grey" disabled>
      <IconSettings size={18} /> Requests aren't set up
    </button>
  )

  let actions
  if (inLibrary && missing.length === 0) {
    actions = (
      <button className="btn btn--brand" onClick={openLibrary}>
        <IconPlay size={20} /> Watch
      </button>
    )
  } else if (inLibrary) {
    actions = (
      <>
        <button className="btn btn--brand" onClick={openLibrary}>
          <IconPlay size={20} /> Watch
        </button>
        {d.requestsEnabled ? (
          <button className="btn btn--grey" disabled={busy} onClick={() => requestAll(missing.map((s) => s.number))}>
            {busy ? 'Requesting…' : `Request ${plural(missing.length)}${filmsSuffix(withFilms.length)}`}
          </button>
        ) : (
          notSetUp
        )}
      </>
    )
  } else if (reqLive && missing.length === 0) {
    actions = (
      <>
        <span className="btn btn--grey dtitle__done">
          <IconCheck size={18} /> {req.status === 'approval' ? 'Waiting for approval' : 'Requested'}
        </span>
        <button className="btn btn--outline" disabled={busy} onClick={() => withdraw(req.id)}>
          Remove request
        </button>
      </>
    )
  } else if (!d.requestsEnabled) {
    actions = notSetUp
  } else {
    const label = busy
      ? 'Requesting…'
      : req?.status === 'failed' || req?.status === 'declined'
        ? 'Request again'
        : missing.length > 0 && missing.length < mainSeasons.length
          ? `Request ${missing.length} more season${missing.length === 1 ? '' : 's'}${filmsSuffix(withFilms.length)}`
          : `Request${filmsSuffix(withFilms.length)}`
    actions = (
      <button className="btn btn--brand" disabled={busy} onClick={() => requestAll()}>
        {label}
      </button>
    )
  }

  // The request's line, unless the download card says more.
  const showCard = dl != null && req?.status === 'sent' && !(inLibrary && effectiveState(dl) === 'complete')
  let reqLine: string | null = null
  if (!showCard && req) {
    const who = req.userName || 'someone'
    reqLine =
      req.status === 'sent'
        ? `Requested by ${who}  ·  sent to ${arr}`
        : req.status === 'failed'
          ? `Request failed  ·  ${req.error || 'unknown error'}`
          : req.status === 'approval'
            ? `Requested by ${who}  ·  waiting for an admin to approve`
            : req.status === 'declined'
              ? 'Request declined' + (req.error ? `  ·  ${req.error}` : '')
              : `Requested by ${who}  ·  waiting for ${arr}`
  }
  const reqBad = req?.status === 'failed' || req?.status === 'declined'
  const sentId = req?.status === 'sent' ? req.id : null

  return (
    <div className="page page--phone">
      <TopBar phone="title" />
      <TitleBanner item={item} badge={extentLabel(d)} extra={statusWord(d.status)} actions={actions} />
      <TitleBelow item={item}>
        {d.tagline && <p className="dtitle__tagline">{d.tagline}</p>}
        {reqLine && <div className={'dtitle__line' + (reqBad ? ' dtitle__line--bad' : '')}>{reqLine}</div>}
        {req?.status === 'approval' && admin && (
          <div className="dtitle__row">
            <button className="btn btn--grey" disabled={busy} onClick={() => act(() => api.declineRequest(req.id))}>
              Decline
            </button>
            <button className="btn btn--brand" disabled={busy} onClick={() => act(() => api.approveRequest(req.id))}>
              <IconCheck size={18} /> Approve
            </button>
          </div>
        )}
        {showCard && dl && req && (
          <DownloadStatusCard
            d={dl}
            isMovie={isMovie}
            admin={admin}
            busy={busy}
            onSearchAgain={() => search(() => api.searchAgain(req.id))}
            onTryAnother={() => search(() => api.tryAnotherRelease(req.id))}
          />
        )}
        {!d.requestsEnabled && (!inLibrary || missing.length > 0) && (
          <div className="notice dtitle__off">
            <b>Requests aren't set up.</b>{' '}
            {admin ? `Choose a root folder and quality profile for ${arr} under Account → Sonarr & Radarr to turn them on.` : `Requests go to ${arr}, which isn't set up on this server yet. Ask whoever runs it.`}
          </div>
        )}
        {notice && <div className="notice notice--error dtitle__off">{notice}</div>}
      </TitleBelow>

      {seasons.length > 0 && (
        <section className="dseasons">
          <h2 className="row__title">Seasons</h2>
          {filmRows(placed.before)}
          {seasons.map((sn) => (
            <Fragment key={sn.number}>
            <SeasonRow
              sn={sn}
              tmdbId={d.tmdbId}
              refreshKey={dl?.updatedAt ?? null}
              enabled={d.requestsEnabled}
              busy={busy}
              admin={admin}
              searchAgain={sentId != null && canSearchFor(dl, sn.number)}
              onRequest={() => request([sn.number])}
              onSearchAgain={() => sentId && search(() => api.searchAgain(sentId, { seasons: [sn.number] }))}
              onRequestEpisode={(code) => request(undefined, [code])}
              onEpisodeAction={sentId ? (code, retry) => search(() => (retry ? api.tryAnotherRelease(sentId, [code]) : api.searchAgain(sentId, { episodes: [code] }))) : null}
            />
            {filmRows(placed.after.get(sn.number))}
            </Fragment>
          ))}
          {placed.more.length > 0 && <h3 className="dseasons__more">More films</h3>}
          {filmRows(placed.more)}
          {films?.auto && <p className="dseasons__auto">Films between the seasons you requested are requested as they are announced.</p>}
        </section>
      )}

      {isMovie && <MovieDetails details={more} />}
      <Footer />
    </div>
  )
}

const complete = (s: DiscoverSeason) => s.episodes > 0 && s.have >= s.episodes

/** The Discover title as the library's Item, so the title page's frame can draw it. */
function asItem(d: DiscoverDetail): Item {
  const images: Record<string, string> = {}
  // The server sends w780 backdrops (enough for a row); the banner wants more.
  if (d.backdrop) images.backdrop = d.backdrop.replace('/w780/', '/w1280/')
  if (d.poster) images.poster = d.poster
  if (d.logo) images.logo = d.logo
  return {
    id: `tmdb:${d.type}:${d.tmdbId}`,
    type: d.type === 'movie' ? 'movie' : 'series',
    title: d.title,
    year: d.year ?? null,
    overview: d.overview ?? null,
    genres: d.genres ?? [],
    communityRating: d.rating ? d.rating : null,
    officialRating: d.officialRating || null,
    studio: (d.networks ?? []).join(', ') || null,
    images,
    isAnime: d.isAnime,
  }
}

function extentLabel(d: DiscoverDetail): string | null {
  if (d.type === 'movie') {
    const m = d.runtimeMinutes ?? 0
    if (m <= 0) return 'Movie'
    return m < 60 ? `${m}m` : `${Math.floor(m / 60)}h ${m % 60}m`
  }
  const n = d.seasonCount ?? 0
  return n > 0 ? (n === 1 ? '1 Season' : `${n} Seasons`) : null
}

/** TMDB's status phrases, shortened for the facts line. */
function statusWord(status?: string | null): string | null {
  switch ((status ?? '').trim().toLowerCase()) {
    case 'returning series':
      return 'Airing'
    case 'ended':
      return 'Ended'
    case 'canceled':
    case 'cancelled':
      return 'Cancelled'
    case 'in production':
    case 'planned':
    case 'post production':
      return 'Upcoming'
    case 'released':
    case '':
      return null
    default:
      return status ?? null
  }
}

/**
 * A film of the series in the season list: its title with what it is ("Side story",
 * "Comes with the request") and, on the right, where it stands or a Request of its own.
 * The title opens the film's page when TMDB's film is known.
 */
function FilmRow({ f, enabled, busy, onRequest }: { f: DiscoverFilm; enabled: boolean; busy: boolean; onRequest: () => void }) {
  const text = (
    <span className="dseason__text">
      <b>{f.title}</b>
      <span>{filmLine(f)}</span>
    </span>
  )
  let state
  switch (filmState(f, enabled)) {
    case 'library':
      state = (
        <span className="dseason__state dseason__state--lib">
          <IconCheck size={18} /> In library
        </span>
      )
      break
    case 'requested':
    case 'approval':
      state = (
        <span className="dseason__state">
          <IconHistory size={18} /> {f.requestStatus === 'approval' ? 'Waiting for approval' : 'Requested'}
        </span>
      )
      break
    case 'failed':
      state = <span className="dseason__state">Request failed</span>
      break
    case 'radarr':
      state = <span className="dseason__state">In Radarr</span>
      break
    case 'request':
      state = (
        <button className="chip" disabled={busy} onClick={onRequest}>
          Request
        </button>
      )
      break
    default:
      state = null
  }
  return (
    <div className="dseason dfilm">
      <div className="dseason__head">
        {f.tmdbId ? (
          <Link className="dseason__toggle" to={`/discover/movie/${f.tmdbId}`}>
            {text}
          </Link>
        ) : (
          <div className="dseason__toggle">{text}</div>
        )}
        {state}
      </div>
    </div>
  )
}

/**
 * One season: name and size, its state (In library, Requested) or a Request button, and a
 * chevron that unfolds the episode list with a tick on each one the library holds.
 */
function SeasonRow({
  sn,
  tmdbId,
  refreshKey,
  enabled,
  busy,
  admin,
  searchAgain,
  onRequest,
  onSearchAgain,
  onRequestEpisode,
  onEpisodeAction,
}: {
  sn: DiscoverSeason
  tmdbId: number
  refreshKey: string | null
  enabled: boolean
  busy: boolean
  admin: boolean
  searchAgain: boolean
  onRequest: () => void
  onSearchAgain: () => void
  onRequestEpisode: (code: string) => void
  onEpisodeAction: ((code: string, retry: boolean) => void) | null
}) {
  const [open, setOpen] = useState(false)
  const [eps, setEps] = useState<DiscoverEpisode[] | null>(null)
  const [error, setError] = useState(false)
  // Loaded when opened, and again as the request's download status moves (per-episode
  // progress), keeping the list on screen meanwhile.
  useEffect(() => {
    if (!open) return
    let live = true
    api
      .discoverEpisodes(tmdbId, sn.number)
      .then((r) => live && (setEps(r.episodes ?? []), setError(false)))
      .catch(() => live && setError(true))
    return () => {
      live = false
    }
  }, [open, tmdbId, sn.number, refreshKey])
  const name = sn.special ? 'Specials' : sn.name || `Season ${sn.number}`
  const count = sn.episodes <= 0 ? null : sn.have > 0 && sn.have < sn.episodes ? `${sn.have} of ${sn.episodes} episodes` : `${sn.episodes} episode${sn.episodes === 1 ? '' : 's'}`
  // Announced, or airing later: requesting it means Sonarr grabs it when it airs.
  const today = new Date().toISOString().slice(0, 10)
  const notOut = !sn.special && (!sn.airDate || sn.airDate >= today || today.startsWith(sn.airDate))
  const sub = [count, sn.airDate?.slice(0, 4), notOut ? 'Not out yet' : null].filter(Boolean).join('  ·  ')

  let state
  if (complete(sn)) {
    state = (
      <span className="dseason__state dseason__state--lib">
        <IconCheck size={18} /> In library
      </span>
    )
  } else if (sn.requested && !sn.special && searchAgain && enabled) {
    state = (
      <button className="chip" disabled={busy} onClick={onSearchAgain}>
        Search again
      </button>
    )
  } else if (sn.requested) {
    state = (
      <span className="dseason__state">
        <IconHistory size={18} /> Requested
      </span>
    )
  } else if (enabled) {
    state = (
      <button className="chip" disabled={busy} onClick={onRequest}>
        {sn.special ? (sn.have > 0 ? 'Request the rest' : 'Request all') : sn.have > 0 ? 'Request rest' : 'Request'}
      </button>
    )
  } else state = null

  return (
    <div className={'dseason' + (open ? ' dseason--open' : '')}>
      <div className="dseason__head">
        <button className="dseason__toggle" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
          <IconExpand size={24} className="dseason__chev" />
          <span className="dseason__text">
            <b>{name}</b>
            {sub && <span>{sub}</span>}
          </span>
        </button>
        {state}
      </div>
      {open &&
        (eps == null ? (
          error ? (
            <div className="dseason__empty">No episode list on TMDB yet.</div>
          ) : (
            <div className="dseason__eps">
              {[0, 1, 2].map((i) => (
                <div key={i} className="bone bone--line" style={{ width: 240, margin: '10px 0' }} />
              ))}
            </div>
          )
        ) : eps.length === 0 ? (
          <div className="dseason__empty">No episode list on TMDB yet.</div>
        ) : (
          <div className="dseason__eps">
            {eps.map((e) => (
              <EpisodeRow
                key={e.number}
                e={e}
                special={!!sn.special}
                enabled={enabled}
                busy={busy}
                admin={admin}
                onRequest={() => onRequestEpisode(`S0E${e.number}`)}
                onAction={onEpisodeAction ? (retry) => onEpisodeAction(`S${sn.number}E${e.number}`, retry) : null}
              />
            ))}
          </div>
        ))}
    </div>
  )
}

/**
 * One episode: tick or ring, the title with the air date (or its download state) under
 * it. Clicking unfolds TMDB's still and synopsis.
 */
function EpisodeRow({
  e,
  special,
  enabled,
  busy,
  admin,
  onRequest,
  onAction,
}: {
  e: DiscoverEpisode
  special: boolean
  enabled: boolean
  busy: boolean
  admin: boolean
  onRequest: () => void
  onAction: ((retry: boolean) => void) | null
}) {
  const [open, setOpen] = useState(false)
  const hasMore = !!(e.overview || e.still)
  const dl = !e.have ? e.download : null
  let action = null
  if (onAction && dl?.canRetry && admin) action = <SmallAction label="Retry" disabled={busy} onClick={() => onAction(true)} />
  else if (onAction && dl?.canSearch) action = <SmallAction label="Search" disabled={busy} onClick={() => onAction(false)} />
  else if (special && !e.have && e.requested) action = <span className="muted dep__req">Requested</span>
  else if (special && !e.have && enabled) action = <SmallAction label="Request" disabled={busy} onClick={onRequest} />
  return (
    <div className="dep">
      <div className="dep__row">
        <span className={'dep__tick' + (e.have ? ' dep__tick--have' : '')}>{e.have ? <IconCheckCircle size={18} /> : <i />}</span>
        <button className="dep__text" onClick={() => hasMore && setOpen((o) => !o)} style={{ cursor: hasMore ? 'pointer' : 'default' }}>
          <span className={e.have ? '' : 'muted'}>
            {e.number}. {e.title || `Episode ${e.number}`}
          </span>
          {dl ? (
            <>
              <small style={{ color: stateColor(dl.state) }}>{episodeLine(dl)}</small>
              {(dl.state === 'downloading' || dl.state === 'stalled') && <DownloadBar progress={dl.progress} state={dl.state} />}
            </>
          ) : (
            (e.airDate || (hasMore && !open)) && <small>{[e.airDate, hasMore && !open ? 'more' : null].filter(Boolean).join('  ·  ')}</small>
          )}
        </button>
        {action}
      </div>
      {open && (
        <div className="dep__more">
          {e.still && <img src={e.still} alt="" loading="lazy" />}
          {e.overview && <p>{e.overview}</p>}
        </div>
      )}
    </div>
  )
}

function SmallAction({ label, disabled, onClick }: { label: string; disabled: boolean; onClick: () => void }) {
  return (
    <button className="dep__action" disabled={disabled} onClick={onClick}>
      {label}
    </button>
  )
}

/** A film's cast and facts (what "More details" holds on the phone). */
function MovieDetails({ details }: { details: ItemDetails | null }) {
  if (!details) {
    return (
      <section className="dmeta">
        <div className="bone bone--line" style={{ width: 120, height: 18 }} />
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="bone bone--line" style={{ width: 'min(300px, 80%)', marginTop: 12 }} />
        ))}
      </section>
    )
  }
  const t = details.tmdb
  const cast = t?.cast ?? []
  const facts: [string, string][] = []
  if (t?.originalTitle && t.originalTitle !== details.item.title) facts.push(['Original title', t.originalTitle])
  if (t?.firstAired) facts.push(['Released', t.firstAired])
  if (t?.companies?.length) facts.push(['Studio', t.companies.slice(0, 3).join(', ')])
  if (t?.countries?.length) facts.push(['Country', t.countries.join(', ')])
  if (t?.languages?.length) facts.push(['Language', t.languages.join(', ')])
  const links = Object.entries(details.links ?? {})
  const linkName: Record<string, string> = { tmdb: 'TMDB', imdb: 'IMDb', tvdb: 'TheTVDB' }
  return (
    <>
      {cast.length > 0 && (
        <Row title="Cast">
          {cast.slice(0, 20).map((c, i) => (
            <div key={`${c.name}:${i}`} className="card dcast">
              <div className="card__art">
                <Art url={c.photo ?? null} title={c.name} />
              </div>
              <div className="card__name">{c.name}</div>
              {c.character && (
                <div className="card__sub">
                  <span>{c.character}</span>
                </div>
              )}
            </div>
          ))}
        </Row>
      )}
      {(facts.length > 0 || links.length > 0 || (t?.keywords?.length ?? 0) > 0) && (
        <section className="dmeta">
          <h2 className="row__title">Details</h2>
          <dl>
            {facts.map(([k, v]) => (
              <div key={k}>
                <dt>{k}</dt>
                <dd>{v}</dd>
              </div>
            ))}
          </dl>
          {(t?.keywords?.length ?? 0) > 0 && (
            <div className="chips chips--small dmeta__chips">
              {t!.keywords!.slice(0, 12).map((k) => (
                <span key={k} className="chip">
                  {k}
                </span>
              ))}
            </div>
          )}
          {links.length > 0 && (
            <div className="dmeta__links">
              {links.map(([k, url]) => (
                <a key={k} className="link-brand" href={url} target="_blank" rel="noreferrer noopener">
                  {linkName[k] ?? k}
                </a>
              ))}
            </div>
          )}
        </section>
      )}
    </>
  )
}

function DetailSkeleton() {
  return (
    <div className="page page--phone">
      <TopBar phone="title" />
      <div className="title__banner">
        <PhoneTitleHeader title="" />
        <div className="bone" style={{ position: 'absolute', inset: 0 }} />
      </div>
      <div style={{ padding: '16px var(--gutter)' }}>
        {[0, 1, 2].map((i) => (
          <div key={i} className="bone bone--line" style={{ width: 'min(520px, 90%)', marginTop: 12 }} />
        ))}
      </div>
    </div>
  )
}
