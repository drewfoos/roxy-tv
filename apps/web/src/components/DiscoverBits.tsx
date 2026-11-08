import { Link } from 'react-router'
import type { DiscoverItem, DownloadGroup, DownloadStatus, EpisodeDownload } from '../api/types'
import { Art, useHoverPanel } from './Cards'
import { IconAdd, IconCheck, IconClose, IconDownload, IconHistory, IconPlay } from './Icons'

/*
 * Discover's shared pieces, from the phone app (PhoneDiscover.kt, PhoneDownloadStatus.kt):
 * the poster card with its corner state, and the words and card for a request's
 * download status as the server's tracker reports it from Sonarr / Radarr.
 */

/** Where a Discover title goes: its library page when the library has it, else its Discover page. */
export function discoverPath(it: DiscoverItem): string {
  if (it.libraryItemId) return it.type === 'movie' ? `/movie/${it.libraryItemId}` : `/series/${it.libraryItemId}`
  return `/discover/${it.type === 'movie' ? 'movie' : 'tv'}/${it.tmdbId}`
}

const MOVING = new Set(['downloading', 'stalled', 'importing'])
const ACTIVE = new Set(['downloading', 'importing', 'queued', 'searching', 'stalled'])
// Sonarr / Radarr are already busy with it: no point asking them to search.
const BUSY = new Set(['searching', 'downloading', 'queued', 'importing'])

type StateLike = { state?: string; have?: number; wanted?: number; upcoming?: number }

/**
 * The state to show. "complete" with nothing wanted, held or upcoming means Sonarr has
 * not listed the episodes yet, not that anything downloaded: treat it as searching.
 */
export function effectiveState(d?: StateLike | null): string {
  if (!d?.state) return ''
  if (d.state === 'complete' && !d.wanted && !d.have && !d.upcoming) return 'searching'
  return d.state
}

/** Something is in the download client right now. */
export const isMoving = (d?: DownloadStatus | null) => !!d && MOVING.has(effectiveState(d))
/** Worth refreshing every few seconds. */
export const isActive = (d?: StateLike | null) => !!d && ACTIVE.has(effectiveState(d))
/** Really done: complete with something in the library. */
export const isDone = (d?: StateLike | null) => !!d && effectiveState(d) === 'complete' && (d.have ?? 0) > 0
/** A sent request that is not done yet: worth a slow refresh (a status can appear or move). */
export const isPending = (status?: string | null, d?: StateLike | null) => status === 'sent' && !isDone(d)
const isProblem = (state: string) => state === 'failed' || state === 'attention' || state === 'stalled'

/**
 * Whether "Search again" can do anything: for one season, any of its episodes that Sonarr
 * can search; for the whole request, the same over every episode (or the request's own
 * flag when there is no episode list, as for a movie), and nothing already under way.
 */
export function canSearchFor(d: DownloadStatus | null | undefined, season?: number): boolean {
  if (!d) return false
  const eps = d.episodes ?? []
  if (season != null) return eps.some((e) => e.season === season && !!e.canSearch)
  if (BUSY.has(effectiveState(d))) return false
  return eps.length > 0 ? eps.some((e) => !!e.canSearch) : !!d.canSearch
}

export function stateColor(state: string): string {
  switch (state) {
    case 'failed':
    case 'attention':
      return 'var(--error)'
    case 'stalled':
      return '#f5a524'
    case 'downloading':
    case 'importing':
    case 'complete':
      return 'var(--accent)'
    default:
      return 'var(--muted)'
  }
}

export function stateWord(state: string): string {
  switch (state) {
    case 'downloading':
      return 'Downloading'
    case 'importing':
      return 'Adding to library'
    case 'queued':
      return 'Queued'
    case 'searching':
      return 'Searching'
    case 'notFound':
      return 'Not found yet'
    case 'waiting':
      return 'Not out yet'
    case 'stalled':
      return 'Stalled'
    case 'attention':
      return 'Needs attention'
    case 'failed':
      return 'Failed'
    case 'complete':
      return 'Downloaded'
    default:
      return state
  }
}

const pct = (p?: number) => `${Math.floor((p ?? 0) * 100)}%`

/** "12 min left", "1 h 5 min left". */
export function timeLeft(sec?: number): string | null {
  if (!sec || sec <= 0) return null
  const m = Math.ceil(sec / 60)
  return m < 60 ? `${m} min left` : `${Math.floor(m / 60)} h ${m % 60} min left`
}

/** "Nov 3" (with the year when it is not this year). */
export function day(iso?: string | null): string | null {
  if (!iso) return null
  const t = new Date(iso)
  if (isNaN(t.getTime())) return null
  const sameYear = t.getFullYear() === new Date().getFullYear()
  return t.toLocaleDateString(undefined, sameYear ? { month: 'short', day: 'numeric' } : { month: 'short', day: 'numeric', year: 'numeric' })
}

function ago(iso?: string | null): string | null {
  if (!iso) return null
  const t = new Date(iso).getTime()
  if (isNaN(t)) return null
  const m = Math.max(0, Math.floor((Date.now() - t) / 60000))
  if (m < 1) return 'just now'
  if (m < 60) return `${m} min ago`
  if (m < 48 * 60) return `${Math.floor(m / 60)} h ago`
  return `${Math.floor(m / 1440)} days ago`
}

const activeEpisodes = (d: DownloadStatus) => (d.episodes ?? []).filter((e) => ['downloading', 'stalled', 'importing', 'queued'].includes(e.state))

/** One episode's line under its title: "Downloading 45% · 2 min left", "Failed · reason", "Airs Nov 3". */
export function episodeLine(e: EpisodeDownload): string {
  switch (e.state) {
    case 'downloading':
    case 'stalled':
      return [`${stateWord(e.state)} ${pct(e.progress)}`, timeLeft(e.timeLeftSec), e.state === 'stalled' ? e.message : null].filter(Boolean).join('  ·  ')
    case 'waiting': {
      const d = day(e.airDate)
      return d ? `Airs ${d}` : 'Not aired yet'
    }
    case 'failed':
    case 'attention':
      return [stateWord(e.state), e.message].filter(Boolean).join('  ·  ')
    default:
      return stateWord(e.state)
  }
}

/** A thin bar: brand blue while downloading, amber when stalled. */
export function DownloadBar({ progress, state, className = '' }: { progress?: number; state: string; className?: string }) {
  return (
    <div className={'dlbar ' + className}>
      <i style={{ width: `${Math.min(1, Math.max(0, progress ?? 0)) * 100}%`, background: stateColor(state) }} />
    </div>
  )
}

/**
 * The poster's corner. A download in progress wins over the library tick (a show half in
 * the library can be fetching its next episodes): which episodes and how far. A request
 * that is not downloading says its state ("Searching", "Not out yet"); otherwise a round
 * badge: check = in the library, cross = failed, clock = requested.
 */
function Corner({ item }: { item: DiscoverItem }) {
  const dl = item.download
  const inLibrary = !!item.libraryItemId
  const st = effectiveState(dl)
  if (dl && isMoving(dl)) {
    const active = activeEpisodes(dl).sort((a, b) => a.season - b.season || a.episode - b.episode)
    const which = item.type === 'movie' || active.length === 0 ? null : active.length === 1 ? `S${active[0].season} E${active[0].episode}` : `${active.length} eps`
    const how = st === 'importing' ? 'Adding' : pct(dl.progress)
    return (
      <span className="dcard__box" style={{ color: stateColor(st) }}>
        {[which, how].filter(Boolean).join(' · ')}
      </span>
    )
  }
  if (dl && !inLibrary) {
    const plain = ['searching', 'notFound', 'waiting', 'queued'].includes(st)
    return (
      <span className="dcard__box" style={{ color: plain ? '#fff' : stateColor(st) }}>
        {st === 'waiting' && dl.waitingFor === 'digital' ? 'In cinemas' : stateWord(st)}
      </span>
    )
  }
  const rs = item.requestStatus
  let cls: string, icon
  if (inLibrary) {
    cls = 'dcard__dot--lib'
    icon = <IconCheck size={14} />
  } else if (rs === 'failed' || rs === 'declined' || st === 'failed' || st === 'attention') {
    cls = 'dcard__dot--bad'
    icon = <IconClose size={14} />
  } else if (st === 'downloading' || st === 'importing') {
    cls = 'dcard__dot--lib'
    icon = <IconDownload size={14} />
  } else if (rs) {
    cls = 'dcard__dot--req'
    icon = <IconHistory size={14} />
  } else return null
  const label = inLibrary ? 'In your library' : rs === 'failed' || rs === 'declined' ? 'Request failed' : 'Requested'
  return (
    <span className={'dcard__dot ' + cls} title={label} aria-label={label}>
      {icon}
    </span>
  )
}

/**
 * Poster with its corner state, the name, then year and type. With a mouse, resting on it
 * slides the same dark panel over it as the library's cards: name, rating, year and kind,
 * genres, the synopsis, and Play (in the library) / Request / the request's state. All of it
 * comes with the list (TMDB's list entries carry synopsis and rating): no request per hover.
 */
export function DiscoverCard({ item }: { item: DiscoverItem }) {
  const dl = item.download
  const kind = item.isAnime ? 'Anime' : item.type === 'movie' ? 'Movie' : 'Series'
  const sub = [item.year, item.isAnime ? 'Anime' : item.type === 'movie' ? 'Movie' : null].filter(Boolean).join('  ·  ')
  const hover = useHoverPanel()
  const libPath = item.libraryItemId ? (item.type === 'movie' ? `/movie/${item.libraryItemId}` : `/series/${item.libraryItemId}`) : null
  const rating = item.rating ? (item.rating / 2).toFixed(1) : null
  const rs = item.requestStatus
  const asked = rs && rs !== 'failed' && rs !== 'declined'
  return (
    <div className="card dcard" onMouseEnter={hover.enter} onMouseLeave={hover.leave}>
      <Link to={discoverPath(item)} className="card__art" title={item.title}>
        <Art url={item.poster ?? null} title={item.title} />
        <Corner item={item} />
        {dl && isMoving(dl) && <DownloadBar progress={dl.progress} state={dl.state} className="dcard__bar" />}
      </Link>
      <Link to={discoverPath(item)} className="card__name" title={item.title}>
        {item.title}
      </Link>
      <div className="card__sub">
        <span>{sub}</span>
      </div>
      {hover.on && (
        <div className="card__hover">
          {item.poster && <img className="card__hover-art" src={item.poster} alt="" />}
          <Link to={discoverPath(item)} className="card__hover-body">
            <h3>{item.title}</h3>
            <div className="card__hover-meta">
              {rating && <span>★ {rating}</span>}
              {item.year && <span>{item.year}</span>}
              <span>{kind}</span>
            </div>
            {!!item.genres?.length && <div className="card__hover-genres">{item.genres.slice(0, 3).join(', ')}</div>}
            {item.overview && <p>{item.overview}</p>}
          </Link>
          <div className="card__hover-actions">
            {libPath ? (
              <Link to={libPath} className="card__hover-cta">
                <IconPlay size={22} /> Play
              </Link>
            ) : (
              <Link to={discoverPath(item)} className="card__hover-cta">
                {asked ? <IconCheck size={20} /> : <IconAdd size={20} />} {asked ? 'Requested' : 'Request'}
              </Link>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

/**
 * The status card under a requested title's facts: headline and percentage, a bar, a line
 * of detail, the arr app's message when something is wrong, one row per state when the
 * request has several downloads, and the actions that fit.
 */
export function DownloadStatusCard({
  d,
  isMovie,
  admin,
  busy,
  onSearchAgain,
  onTryAnother,
}: {
  d: DownloadStatus
  isMovie: boolean
  admin: boolean
  busy: boolean
  onSearchAgain: () => void
  onTryAnother: () => void
}) {
  const arr = isMovie ? 'Radarr' : 'Sonarr'
  const episodes = !isMovie && (d.wanted ?? 0) > 1
  const state = effectiveState(d)
  const headline = (() => {
    switch (state) {
      case 'searching':
        return 'Looking for a release'
      case 'notFound':
        return 'No download found yet'
      case 'waiting':
        return !isMovie ? 'Not aired yet' : d.waitingFor === 'digital' ? 'In cinemas' : 'Not released yet'
      case 'queued':
        return 'Queued'
      case 'downloading':
        return 'Downloading'
      case 'stalled':
        return 'Download stalled'
      case 'importing':
        return 'Adding to your library'
      case 'attention':
        return 'Needs attention'
      case 'failed':
        return 'Download failed'
      case 'complete':
        return !isMovie && (d.upcoming ?? 0) > 0 ? 'Up to date' : 'Downloaded'
      default:
        return state
    }
  })()
  const first = (() => {
    switch (state) {
      case 'searching':
        return `${arr} is searching`
      case 'notFound': {
        const a = ago(d.lastSearch)
        return a ? `Searched ${a} · ${arr} keeps checking` : `${arr} keeps checking for new releases`
      }
      case 'waiting': {
        const n = day(d.nextAir)
        if (!isMovie) return n ? `First episode airs ${n}` : null
        if (d.waitingFor === 'digital') return n ? `Radarr fetches it once it is out digitally (${n})` : 'Radarr fetches it once it is out digitally'
        return n ? `Releases ${n}` : null
      }
      case 'downloading':
      case 'stalled':
        return timeLeft(d.timeLeftSec)
      case 'complete': {
        if (!isMovie && (d.upcoming ?? 0) > 0) {
          const n = day(d.nextAir)
          return n ? `Next episode ${n}` : null
        }
        return 'Adding to your library'
      }
      default:
        return null
    }
  })()
  const detail = [first, episodes ? `${d.have ?? 0} of ${d.wanted} episodes` : null].filter(Boolean).join('  ·  ')
  const color = stateColor(state)
  const groups = d.groups ?? []
  const showGroups = groups.length > 1 || (groups.length === 1 && !isMovie && (groups[0].count ?? 0) > 1)
  const canSearch = canSearchFor(d)
  const canReplace = admin && !!d.canRetry
  return (
    <div className="dlcard">
      <div className="dlcard__head">
        <i style={{ background: color }} />
        <b>{headline}</b>
        {(state === 'downloading' || state === 'stalled') && <span style={{ color }}>{pct(d.progress)}</span>}
      </div>
      {(state === 'downloading' || state === 'stalled' || state === 'importing') && <DownloadBar progress={state === 'importing' ? 1 : d.progress} state={state} />}
      {detail && <div className="muted">{detail}</div>}
      {d.message && <div style={{ color: isProblem(state) ? color : 'var(--muted)' }}>{d.message}</div>}
      {showGroups && (
        <div className="dlcard__groups">
          {groups.map((g: DownloadGroup, i) => (
            <div key={i}>
              <b style={{ color: stateColor(g.state) }}>{stateWord(g.state)}</b>
              <span>{g.label}</span>
              {(g.state === 'downloading' || g.state === 'stalled') && <em>{pct(g.progress)}</em>}
            </div>
          ))}
        </div>
      )}
      {(canSearch || canReplace) && (
        <div className="dlcard__actions">
          {canReplace && (
            <button className="btn btn--brand" disabled={busy} onClick={onTryAnother}>
              Try another release
            </button>
          )}
          {canSearch && (
            <button className="btn btn--grey" disabled={busy} onClick={onSearchAgain}>
              Search again
            </button>
          )}
        </div>
      )}
    </div>
  )
}

/** A grid of Discover posters ("More like this" on the library's title pages, search results). */
export function DiscoverGrid({ items }: { items: DiscoverItem[] }) {
  return (
    <div className="grid">
      {items.map((c) => (
        <DiscoverCard key={`${c.type}:${c.tmdbId}`} item={c} />
      ))}
    </div>
  )
}
