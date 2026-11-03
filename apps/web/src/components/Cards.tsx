import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Link } from 'react-router'
import { api, px } from '../api/client'
import { audioBadge, episodeCode, episodeNumber, isEpisode, isMovie, isSeries, progressFraction, runtimeLabel, timeLeftLabel, yearOf } from '../api/items'
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
export function TitleCard({ item, width = 260, sub: subLine }: { item: Item; width?: number; sub?: string }) {
  const sub = subLine ?? audioBadge(item) ??[yearOf(item), isMovie(item) ? 'Movie' : null].filter(Boolean).join('  ·  ')
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

/**
 * 16:9 still with a play ring, badge, progress; caption = show, then "S1 E5  Title". With
 * a mouse, resting on it slides the same dark panel as the title cards over it: the show,
 * "E2 — Title", the air date, the synopsis, then "Continue E2" and the three-dot menu.
 */
export function ThumbTile({ item, width = 320, big = false }: { item: Item; width?: number; big?: boolean }) {
  const movie = isMovie(item)
  const url = movie ? (api.imageUrl(item, 'backdrop', px(width)) ?? api.imageUrl(item, 'poster', px(width))) : api.imageUrl(item, 'thumb', px(width))
  const badge = audioBadge(item)
  const [played, setPlayed] = useState(!!item.userData?.played)
  const shown = useMemo(() => withPlayed(item, played), [item, played])
  const left = timeLeftLabel(shown)
  const watched = isWatched(shown)
  const hover = useHoverPanel()
  // The panel stays while its menu is open (the menu floats outside the card).
  const [menuOpen, setMenuOpen] = useState(false)
  const name = movie ? item.title : (item.seriesTitle ?? item.title)
  const epNo = movie ? null : episodeNumber(item)
  const epLine = movie ? null : [epNo, item.title].filter(Boolean).join(' — ')
  const date = formatAirDate(item.airDate ?? item.premiered)
  const resuming = (shown.userData?.positionMs ?? 0) > 0
  const cta = [resuming ? 'Continue' : watched ? 'Watch again' : 'Play', epNo].filter(Boolean).join(' ')
  return (
    <div className="card" onMouseEnter={hover.enter} onMouseLeave={hover.leave}>
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
      <div className="card__name">{name}</div>
      <div className="card__sub">
        <span>{movie ? [yearOf(item), 'Movie'].filter(Boolean).join('  ·  ') : [episodeCode(item), item.title].filter(Boolean).join('  ')}</span>
        <span className="phone-only">
          <CardMenu item={item} played={played} onPlayedChange={setPlayed} />
        </span>
      </div>
      {(hover.on || menuOpen) && (
        <div className="card__hover card__hover--thumb">
          <Link to={itemPath(item)} className="card__hover-body">
            <h3>{name}</h3>
            {epLine && <div className="card__hover-ep">{epLine}</div>}
            {date && <div className="card__hover-meta">{date}</div>}
            {item.overview && <p>{item.overview}</p>}
          </Link>
          <div className="card__hover-actions card__hover-actions--split">
            <Link to={itemPath(item)} className="card__hover-cta">
              <IconPlay size={22} /> {cta}
            </Link>
            <CardMenu item={item} played={played} onPlayedChange={setPlayed} onOpenChange={setMenuOpen} />
          </div>
        </div>
      )}
    </div>
  )
}

/** The mouse-only hover panel's on / off, with the title cards' short delay. */
function useHoverPanel() {
  const [on, setOn] = useState(false)
  const timer = useRef<number | null>(null)
  useEffect(() => () => {
    if (timer.current) window.clearTimeout(timer.current)
  }, [])
  return {
    on,
    enter: () => {
      if (!window.matchMedia('(hover: hover) and (min-width: 601px)').matches) return
      timer.current = window.setTimeout(() => setOn(true), 40)
    },
    leave: () => {
      if (timer.current) window.clearTimeout(timer.current)
      timer.current = null
      setOn(false)
    },
  }
}

/** "Oct 30, 2026"; a bare "2026-10-30" is that calendar day, not UTC midnight (a day early here). */
function formatAirDate(v?: string | null): string | null {
  if (!v) return null
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(v)
  const d = m ? new Date(+m[1], +m[2] - 1, +m[3]) : new Date(v)
  return isNaN(d.getTime()) ? null : d.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })
}

/** The item as it looks once marked watched / unwatched (the server resets the position too). */
export function withPlayed(item: Item, played: boolean): Item {
  if (played === !!item.userData?.played) return item
  return { ...item, userData: { ...(item.userData ?? { positionMs: 0, playCount: 0, played: false, isFavorite: false }), played, positionMs: 0 } }
}

/**
 * Watched with nothing to resume: the dimmed still, the replay disc and "Watched". A
 * watched episode with a position again is a rewatch under way, so it shows as in
 * progress (the bar and "Nm left"), the same rule as the apps' Item.isWatched.
 */
export function isWatched(item: Item): boolean {
  return !!item.userData?.played && (item.userData?.positionMs ?? 0) <= 0
}

/**
 * The three-dot menu: Watch now, Watchlist, Mark as watched. A card that draws the
 * watched state passes `played` and `onPlayedChange` so it redraws with the menu.
 * The menu floats over the page (a portal): inside a row it would be cut off by the
 * row's sideways scroll, and it used to turn the row into a vertical scroller instead.
 */
export function CardMenu({ item, played, onPlayedChange, onOpenChange }: { item: Item; played?: boolean; onPlayedChange?: (played: boolean) => void; onOpenChange?: (open: boolean) => void }) {
  const [open, setOpenState] = useState(false)
  const [pos, setPos] = useState<{ top?: number; bottom?: number; right: number } | null>(null)
  const btn = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLDivElement>(null)
  const setOpen = (v: boolean | ((o: boolean) => boolean)) => {
    setOpenState((o) => {
      const next = typeof v === 'function' ? v(o) : v
      if (next && btn.current) {
        // Below the dots, or above them when there is no room underneath.
        const r = btn.current.getBoundingClientRect()
        const right = Math.max(8, window.innerWidth - r.right)
        setPos(r.bottom + 190 > window.innerHeight ? { bottom: window.innerHeight - r.top + 4, right } : { top: r.bottom + 4, right })
      }
      if (next !== o) onOpenChange?.(next)
      return next
    })
  }
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
      const t = e.target as Node
      if (!ref.current?.contains(t) && !menu.current?.contains(t)) setOpen(false)
    }
    // A fixed menu would drift away from its card: close it when anything scrolls.
    const gone = () => setOpen(false)
    document.addEventListener('mousedown', close)
    window.addEventListener('scroll', gone, true)
    window.addEventListener('resize', gone)
    return () => {
      document.removeEventListener('mousedown', close)
      window.removeEventListener('scroll', gone, true)
      window.removeEventListener('resize', gone)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
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
      <button ref={btn} className="icon-btn" onClick={() => setOpen((o) => !o)} aria-label="More" aria-expanded={open}>
        <IconMore size={20} />
      </button>
      {open && pos && createPortal(
        <div ref={menu} className="menu menu--float" style={pos} onClick={() => setOpen(false)}>
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
        </div>,
        document.body,
      )}
    </div>
  )
}

/**
 * Click and drag a row sideways with the mouse (touch and trackpads scroll natively).
 * A press that moves less than a few pixels is still a click; a real drag swallows the
 * click that follows, so letting go over a card doesn't open it. Snapping is off while
 * dragging (it fights the pointer) and settles on a card after.
 */
function useDragScroll(track: { current: HTMLDivElement | null }) {
  useEffect(() => {
    const el = track.current
    if (!el) return
    let startX = 0
    let startLeft = 0
    let dragging = false
    let pressed = false
    const move = (e: PointerEvent) => {
      if (!pressed) return
      const dx = e.clientX - startX
      if (!dragging && Math.abs(dx) < 6) return
      dragging = true
      el.classList.add('row__track--dragging')
      el.scrollLeft = startLeft - dx
    }
    const up = () => {
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
      if (!pressed) return
      pressed = false
      if (!dragging) return
      // Swallow the click this release produces, then let snapping settle on a card.
      const swallow = (ev: MouseEvent) => {
        ev.preventDefault()
        ev.stopPropagation()
      }
      el.addEventListener('click', swallow, { capture: true, once: true })
      window.setTimeout(() => el.removeEventListener('click', swallow, { capture: true }), 0)
      el.classList.remove('row__track--dragging')
      dragging = false
    }
    const down = (e: PointerEvent) => {
      if (e.pointerType !== 'mouse' || e.button !== 0) return
      // The three-dot button and its menu keep their own clicks.
      if ((e.target as HTMLElement).closest('.card__menu, .menu')) return
      pressed = true
      startX = e.clientX
      startLeft = el.scrollLeft
      window.addEventListener('pointermove', move)
      window.addEventListener('pointerup', up)
    }
    // Links and images would start the browser's own drag-and-drop instead.
    const noNativeDrag = (e: DragEvent) => e.preventDefault()
    el.addEventListener('pointerdown', down)
    el.addEventListener('dragstart', noNativeDrag)
    return () => {
      el.removeEventListener('pointerdown', down)
      el.removeEventListener('dragstart', noNativeDrag)
      window.removeEventListener('pointermove', move)
      window.removeEventListener('pointerup', up)
    }
  }, [track])
}

/** A titled horizontal row with hover arrows; the track snaps card to card; drag it with the mouse. */
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
  useDragScroll(track)
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
