import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { Link } from 'react-router'
import { api, px } from '../api/client'
import { audioBadge, episodeCode, isEpisode, isMovie, isSeries, progressFraction, timeLeftLabel, yearOf } from '../api/items'
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
  if (f == null) return null
  return (
    <div className="progress">
      <i style={{ width: `${f * 100}%` }} />
    </div>
  )
}

/** Title card (Crunchyroll's): 2:3 poster with a folded corner when saved, the name, then "Sub | Dub" with the menu. */
export function TitleCard({ item, width = 260 }: { item: Item; width?: number }) {
  const sub = audioBadge(item) ?? [yearOf(item), isMovie(item) ? 'Movie' : null].filter(Boolean).join('  ·  ')
  return (
    <div className="card">
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
        <CardMenu item={item} />
      </div>
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
  const left = timeLeftLabel(item)
  const watched = item.userData?.played && !left
  return (
    <div className="card">
      <Link to={itemPath(item)} className="card__art card__art--thumb">
        <Art url={url} title={episodeCode(item) || item.title} />
        {watched ? (
          <div className="watched-ring">
            <IconReplay size={28} />
          </div>
        ) : (
          <div className={'play-ring' + (big ? ' play-ring--big' : '')}>
            <IconPlay size={big ? 32 : 28} />
          </div>
        )}
        {badge && <span className="badge badge--tl">{badge}</span>}
        {watched ? <span className="badge badge--br">Watched</span> : left ? <span className="badge badge--br">{left}</span> : null}
        <ProgressBar item={item} />
      </Link>
      <div className="card__name">{movie ? item.title : (item.seriesTitle ?? item.title)}</div>
      <div className="card__sub">
        <span>{movie ? [yearOf(item), 'Movie'].filter(Boolean).join('  ·  ') : [episodeCode(item), item.title].filter(Boolean).join('  ')}</span>
        <CardMenu item={item} />
      </div>
    </div>
  )
}

/** The three-dot menu: Watch now, Watchlist, Mark as watched. */
export function CardMenu({ item }: { item: Item }) {
  const [open, setOpen] = useState(false)
  const [saved, setSaved] = useState<boolean | null>(isEpisode(item) ? null : (item.userData?.isFavorite ?? false))
  const [watched, setWatched] = useState(item.userData?.played ?? false)
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
      <div className="row__scroller">
        <button className="row__arrow row__arrow--left" onClick={() => scrollBy(-1)} aria-label="Scroll left">
          <IconChevronLeft size={36} />
        </button>
        <div ref={track} className={'row__track' + (thumbs ? ' row__track--thumbs' : '')}>
          {children}
        </div>
        <button className="row__arrow row__arrow--right" onClick={() => scrollBy(1)} aria-label="Scroll right">
          <IconChevronRight size={36} />
        </button>
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
