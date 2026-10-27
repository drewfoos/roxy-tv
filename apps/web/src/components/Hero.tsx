import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { api, px } from '../api/client'
import { ageLabel, audioBadge, episodeCode, isEpisode, isMovie } from '../api/items'
import type { Item } from '../api/types'
import { IconChevronLeft, IconChevronRight, IconPlay } from './Icons'
import { SaveButton, itemPath } from './Cards'

/** One slide: the series or movie to show, and what Play starts. */
export interface HeroPick {
  title: Item
  play: Item
}

const DWELL_MS = 10000

/**
 * Crunchyroll's carousel: the art fills the width with a dark fade from the left, and
 * over it the logo, "12+ • Dub | Sub • Action, Comedy", four lines of synopsis, the
 * CONTINUE WATCHING button with the bookmark, page pills at the bottom-left (the current
 * one twice as wide, filling with the brand colour as its ten seconds run) and arrows at
 * both edges. The timer keeps running under the pointer, as theirs does.
 */
export function Hero({ heroes }: { heroes: HeroPick[] }) {
  const [index, setIndex] = useState(0)
  const [round, setRound] = useState(0)
  const nav = useNavigate()
  const count = heroes.length

  useEffect(() => {
    if (count < 2) return
    const t = setTimeout(() => setIndex((i) => (i + 1) % count), DWELL_MS)
    return () => clearTimeout(t)
  }, [count, index, round])

  useEffect(() => {
    if (index >= count) setIndex(0)
  }, [count, index])

  if (!count) return <div className="hero bone" />

  const pick = (i: number) => {
    setIndex(((i % count) + count) % count)
    setRound((r) => r + 1)
  }

  return (
    <section className="hero">
      {heroes.map((h, i) => {
        const t = h.title
        // Wide art first: the phone's "hero" is tall key art and crops badly at desktop width.
        const art = api.imageUrl(t, 'backdrop', px(1800)) ?? api.imageUrl(t, 'hero', px(1800)) ?? api.imageUrl(h.play, 'thumb', px(1800)) ?? api.imageUrl(t, 'poster', px(900))
        const logo = api.imageUrl(t, 'logo', px(360))
        const genres = (t.genres ?? []).slice(0, 4).join(', ')
        const badge = audioBadge(h.play) ?? audioBadge(t)
        const resumable = (h.play.userData?.positionMs ?? 0) > 0
        const code = episodeCode(h.play)
        const ep = h.play.indexAired ?? h.play.indexAbsolute
        const label = resumable ? (isEpisode(h.play) && ep != null ? `Continue watching E${ep}` : 'Continue watching') : isEpisode(h.play) && ep != null ? `Start watching E${ep}` : code ? `Start watching ${code}` : 'Start watching'
        const playTo = isEpisode(h.play) || isMovie(h.play) ? `/watch/${h.play.id}` : itemPath(h.play)
        return (
          <div key={t.id} className={'hero__slide' + (i === index ? ' hero__slide--on' : '')} aria-hidden={i !== index}>
            {art && <img className="hero__art" src={art} alt="" />}
            <div className="hero__scrim-top" />
            <div className="hero__scrim-left" />
            <div className="hero__scrim-bottom" />
            <div className="hero__body">
              <Link to={itemPath(t)}>{logo ? <img className="hero__logo" src={logo} alt={t.title} /> : <h1 className="hero__title">{t.title}</h1>}</Link>
              <div className="hero__meta">
                {t.officialRating && <span className="age">{ageLabel(t.officialRating)}</span>}
                {badge && <span>• {badge}</span>}
                {genres && <span>• {genres}</span>}
              </div>
              {t.overview && <p className="hero__synopsis">{t.overview}</p>}
              <div className="hero__actions">
                <button className="btn btn--brand" onClick={() => nav(playTo)}>
                  <IconPlay size={20} /> {label}
                </button>
                <SaveButton item={t} />
              </div>
            </div>
          </div>
        )
      })}
      {count > 1 && (
        <>
          <button className="hero__arrow hero__arrow--left" onClick={() => pick(index - 1)} aria-label="Previous">
            <IconChevronLeft size={46} />
          </button>
          <button className="hero__arrow hero__arrow--right" onClick={() => pick(index + 1)} aria-label="Next">
            <IconChevronRight size={46} />
          </button>
          <div className="hero__pills" style={{ ['--dwell' as string]: `${DWELL_MS}ms` }}>
            {heroes.map((h, i) => (
              <button key={h.title.id + ':' + round} className={'hero__pill' + (i === index ? ' hero__pill--on' : '')} onClick={() => pick(i)} aria-label={`Slide ${i + 1}`}>
                <i />
              </button>
            ))}
          </div>
        </>
      )}
    </section>
  )
}
