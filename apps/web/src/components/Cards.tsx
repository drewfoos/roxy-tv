import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router'
import { api, px } from '../api/client'
import { audioBadge, episodeCode, isEpisode, isMovie, isSeries, progressFraction, runtimeLabel, timeLeftLabel, yearOf } from '../api/items'
import type { Item } from '../api/types'
import { IconBookmark, IconBookmarkOutline, IconCheckCircle, IconChevronLeft, IconChevronRight, IconMore, IconPlay, IconReplay, WitchHat } from './Icons'

/** Where a card goes: a show to its page, a film to its page, an episode straight to the player. */
export function itemPath(it: Item): string {
  if (isSeries(it)) return `/series/${it.id}`
  if (isMovie(it)) return `/movie/${it.id}`
  if (isEpisode(it)) return `/watch/${it.id}`
  if (it.seriesId) return `/series/${it.seriesId}`
  return '/'
}

/**
 * Art with the designed fallback: a gradient tinted per title, a faint hat and the name,
 * so a row of posterless titles is not a row of grey boxes.
 */
export function Art({ url, title, className = 'card__img', textSize = 13 }: { url: string | null; title: string; className?: string; textSize?: number }) {
  const [failed, setFailed] = useState(false)
  useEffect(() => setFailed(false), [url])
  const hue = useMemo(() => {
    let h = 0
    for (const c of title.toLowerCase()) h = (h * 31 + c.charCodeAt(0)) >>> 0
    return 200 + (h % 70)
  }, [title])
  if (!url || failed) {
    return (
      <div className="art-fallback" style={{ background: `linear-gradient(to bottom, hsl(${hue} 55% 30%), hsl(${hue} 45% 11%))`, fontSize: textSize }}>
        <div>
          <WitchHat size={40} />
          <div>{title.replace(/([-_.])(?=\S)/g, '$1​')}</div>
        </div>
      </div>
    )
  }
  return <img className={className} src={url} alt="" loading="lazy" onError={() => setFailed(true)} />
}

export function ProgressBar({ item }: { item: Item }) {
  const f = progressFraction(item)
  if (f == null || isWatched(item)) return null
  return (
    <div className="progress">
      <i style={{ width: `${f * 100}%` }} />
    </div>
  )
}

/**
 * Title card (Crunchyroll's): 2:3 poster with a folded corner when saved, the name, then
 * "Sub | Dub". With a mouse, resting on it for a moment slides a dark panel over the
 * card (theirs: #151515 with an 8 px ring) with the name, the rating, "3 Seasons", six
 * lines of synopsis, the poster showing faintly through, and Play / Save at the foot.
 * There is no three-dot menu on a mouse; the phone keeps its own.
 */
export function TitleCard({ item, width = 260 }: { item: Item; width?: number }) {
  const sub = audioBadge(item) ?? [yearOf(item), isMovie(item) ? 'Movie' : null].filter(Boolean).join('  ·  ')
  const [hover, setHover] = useState(false)
  const timer = useRef<number | null>(null)
  const enter = () => {
    if (!window.matchMedia('(hover: hover) and (min-width: 601px)').matches) return
    timer.current = window.setTimeout(() => setHover(true), 40) // theirs is near-instant
  }
  const leave = () => {
    if (timer.current) window.clearTimeout(timer.current)
    timer.current = null
    setHover(false)
  }
  const rating = item.communityRating != null ? (item.communityRating / 2).toFixed(1) : null
  const extent = isMovie(item) ? runtimeLabel(item) : item.seasonCount ? (item.seasonCount === 1 ? '1 Season' : `${item.seasonCount} Seasons`) : null
  return (
    <div className="card" onMouseEnter={enter} onMouseLeave={leave}>
      <Link to={itemPath(item)} className="card__art">
        <Art url={api.imageUrl(item, 'poster', px(width))} title={item.title} />
        {item.userData?.isFavorite && (
          <span className="card__saved">
            <IconBookmark size={14} />
          </span>
        )}
      </Link>
      <Link to={itemPath(item)} className="card__name" title={item.title}>
        {item.title}
      </Link>
      <div className="card__sub">
        <span>{sub}</span>
        <span className="phone-only">
          <CardMenu item={item} />
        </span>
      </div>
      {hover && (
        <div className="card__hover">
          {api.imageUrl(item, 'poster', px(width)) && <img className="card__hover-art" src={api.imageUrl(item, 'poster', px(width)) ?? undefined} alt="" />}
          <Link to={itemPath(item)} className="card__hover-body">
            <h3>{item.title}</h3>
            {(rating || extent) && (
              <div className="card__hover-meta">
                {rating && <span>★ {rating}</span>}
                {extent && <span>{extent}</span>}
              </div>
            )}
            {item.overview && <p>{item.overview}</p>}
          </Link>
          <div className="card__hover-actions">
            <Link to={itemPath(item)} className="card__hover-btn" aria-label="Play" title="Play">
              <IconPlay size={24} />
            </Link>
            <SaveButton item={item} />
          </div>
        </div>
      )}
    </div>
  )
}

/** Poster alone (grids). */
export function PosterTile({ item, width = 200 }: { item: Item; width?: number }) {
  return (
    <Link to={itemPath(item)} className="card" title={item.title}>
      <div className="card__art">
        <Art url={api.imageUrl(item, 'poster', px(width))} title={item.title} />
      </div>
    </Link>
  )
}

/** 16:9 still with a play ring, badge, progress; caption = show, then "S1 E5  Title". */
export function ThumbTile({ item, width = 320, big = false }: { item: Item; width?: number; big?: boolean }) {
  const movie = isMovie(item)
  const url = movie ? (api.imageUrl(item, 'backdrop', px(width)) ?? api.imageUrl(item, 'poster', px(width))) : api.imageUrl(item, 'thumb', px(width))
  const badge = audioBadge(item)
  const [played, setPlayed] = useState(!!item.userData?.played)
  const shown = useMemo(() => withPlayed(item, played), [item, played])
  const left = timeLeftLabel(shown)
  const watched = isWatched(shown)
  return (
    <div className="card">
      <Link to={itemPath(item)} className="card__art card__art--thumb">
        <Art url={url} title={episodeCode(item) || item.title} />
        {watched ? (
          <div className="watched-overlay">
            <IconReplay size={48} />
          </div>
        ) : (
          <div className={'play-ring' + (big ? ' play-ring--big' : '')}>
            <IconPlay size={big ? 32 : 28} />
          </div>
        )}
        {badge && <span className="badge badge--tl">{badge}</span>}
        {watched ? <span className="badge badge--br">Watched</span> : left ? <span className="badge badge--br">{left}</span> : null}
        <ProgressBar item={shown} />
      </Link>
      <div className="card__name">{movie ? item.title : (item.seriesTitle ?? item.title)}</div>
      <div className="card__sub">
        <span>{movie ? [yearOf(item), 'Movie'].filter(Boolean).join('  ·  ') : [episodeCode(item), item.title].filter(Boolean).join('  ')}</span>
        <CardMenu item={item} played={played} onPlayedChange={setPlayed} />
      </div>
    </div>
  )
}

/** The item as it looks once marked watched / unwatched (the server resets the position too). */
export function withPlayed(item: Item, played: boolean): Item {
  if (played === !!item.userData?.played) return item
  return { ...item, userData: { ...(item.userData ?? { positionMs: 0, playCount: 0, played: false, isFavorite: false }), played, positionMs: 0 } }
}

/** Marked played: the dimmed still, the replay disc and the "Watched" pill (as in the apps). */
export function isWatched(item: Item): boolean {
  return !!item.userData?.played
}

/**
 * The three-dot menu: Watch now, Watchlist, Mark as watched. A card that draws the
 * watched state passes `played` and `onPlayedChange` so it redraws with the menu.
 */
export function CardMenu({ item, played, onPlayedChange }: { item: Item; played?: boolean; onPlayedChange?: (played: boolean) => void }) {
  const [open, setOpen] = useState(false)
  const [saved, setSaved] = useState<boolean | null>(isEpisode(item) ? null : (item.userData?.isFavorite ?? false))
  const [ownWatched, setOwnWatched] = useState(item.userData?.played ?? false)
  const watched = played ?? ownWatched
  const setWatched = (v: boolean) => {
    setOwnWatched(v)
    onPlayedChange?.(v)
  }
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open])
  const target = isEpisode(item) ? (item.seriesId ?? item.id) : item.id
  const toggleSave = async () => {
    const want = !(saved ?? false)
    setSaved(want)
    try {
      await api.setFavorite(target, want)
    } catch {
      setSaved(!want)
    }
  }
  const toggleWatched = async () => {
    const want = !watched
    setWatched(want)
    try {
      await api.setPlayed(item.id, want)
    } catch {
      setWatched(!want)
    }
  }
  return (
    <div ref={ref} style={{ position: 'relative' }} className="card__menu">
      <button className="icon-btn" onClick={() => setOpen((o) => !o)} aria-label="More">
        <IconMore size={20} />
      </button>
      {open && (
        <div className="menu" onClick={() => setOpen(false)}>
          <Link to={itemPath(item)}>
            <button>
              <IconPlay size={20} /> Watch now
            </button>
          </Link>
          <button onClick={toggleSave}>
            {saved ? <IconBookmark size={20} /> : <IconBookmarkOutline size={20} />} {saved ? 'Remove from Watchlist' : 'Add to Watchlist'}
          </button>
          {(isEpisode(item) || isMovie(item)) && (
            <button onClick={toggleWatched}>
              <IconCheckCircle size={20} /> {watched ? 'Mark as unwatched' : 'Mark as watched'}
            </button>
          )}
        </div>
      )}
    </div>
  )
}

/** A titled horizontal row with hover arrows; the track snaps card to card. */
export function Row({ title, children, thumbs = false, more }: { title: string; children: ReactNode; thumbs?: boolean; more?: ReactNode }) {
  const track = useRef<HTMLDivElement>(null)
  // Crunchyroll's arrows: a bare chevron in each gutter, shown only where there is more to see.
  const [ends, setEnds] = useState({ left: false, right: false })
  // The chevron centres on the art, not the whole card: the arrow is as tall as the first still.
  const [artH, setArtH] = useState(0)
  useEffect(() => {
    const el = track.current
    if (!el) return
    const measure = () => {
      const max = el.scrollWidth - el.clientWidth
      setEnds({ left: el.scrollLeft > 4, right: el.scrollLeft < max - 4 })
      setArtH(el.querySelector('.card__art, .ep__art')?.getBoundingClientRect().height ?? 0)
    }
    measure()
    el.addEventListener('scroll', measure, { passive: true })
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => {
      el.removeEventListener('scroll', measure)
      ro.disconnect()
    }
  }, [children])
  const scrollBy = (dir: 1 | -1) => {
    const el = track.current
    if (el) el.scrollBy({ left: dir * el.clientWidth * 0.8, behavior: 'smooth' })
  }
  return (
    <section className="row">
      <div className="row__head">
        <h2 className="row__title">{title}</h2>
        {more}
      </div>
      <div className="row__scroller" style={artH ? { ['--art-h' as string]: `${artH}px` } : undefined}>
        {ends.left && (
          <button className="row__arrow row__arrow--left" onClick={() => scrollBy(-1)} aria-label="Scroll previous">
            <IconChevronLeft size={40} />
          </button>
        )}
        <div ref={track} className={'row__track' + (thumbs ? ' row__track--thumbs' : '')}>
          {children}
        </div>
        {ends.right && (
          <button className="row__arrow row__arrow--right" onClick={() => scrollBy(1)} aria-label="Scroll next">
            <IconChevronRight size={40} />
          </button>
        )}
      </div>
    </section>
  )
}

/** The brand-blue round bookmark beside a Play pill. */
export function SaveButton({ item, onChanged }: { item: Item; onChanged?: (on: boolean) => void }) {
  const [on, setOn] = useState(item.userData?.isFavorite ?? false)
  useEffect(() => setOn(item.userData?.isFavorite ?? false), [item.id, item.userData?.isFavorite])
  const toggle = async () => {
    const want = !on
    setOn(want)
    try {
      await api.setFavorite(item.id, want)
      onChanged?.(want)
    } catch {
      setOn(!want)
    }
  }
  return (
    <button className="save-btn" onClick={toggle} aria-label={on ? 'Remove from Watchlist' : 'Add to Watchlist'} title={on ? 'Saved' : 'Save'}>
      {on ? <IconBookmark /> : <IconBookmarkOutline />}
    </button>
  )
}
