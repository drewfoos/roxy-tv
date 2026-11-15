import { useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { api, px } from '../api/client'
import { ageLabel, audioBadge, isEpisode, isMovie, playLabel } from '../api/items'
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
 * Crunchyroll's carousel, placed as theirs is at every width (measured 2026-11-15; the
 * rules are beside `.hero` in app.css). The card's height comes from the page's WIDTH:
 * 20:7 from 800 px up, 1:1.15 below, where the art turns into a tall poster and the words
 * are centred. The art is wider than the card is tall and runs on under the pills and the
 * first row. The logo and the words hang from the card's bottom edge on a 12-column grid;
 * the synopsis shows from 1024 px. Page pills sit under the card (the current one twice as
 * wide, filling with the brand colour as its ten seconds run), arrows at both edges from
 * 800 px. The timer keeps running under the pointer, as theirs does.
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

  if (!count)
    return (
      <div className="hero">
        <div className="hero__cards bone" />
        <div className="hero__pills" />
      </div>
    )

  const pick = (i: number) => {
    setIndex(((i % count) + count) % count)
    setRound((r) => r + 1)
  }

  return (
    <section className="hero">
      <div className="hero__cards">
        {heroes.map((h, i) => {
          const t = h.title
          // Wide art first: the phone's "hero" is tall key art and crops badly at desktop width.
          const art = api.imageUrl(t, 'backdrop', px(1800)) ?? api.imageUrl(t, 'hero', px(1800)) ?? api.imageUrl(h.play, 'thumb', px(1800)) ?? api.imageUrl(t, 'poster', px(900))
          // Under 800 px the art is a tall poster: textless key art, else the poster, else the still.
          const tall = api.imageUrl(t, 'hero', px(800)) ?? api.imageUrl(t, 'poster', px(800)) ?? api.imageUrl(h.play, 'thumb', px(800))
          const rating = t.communityRating != null ? (t.communityRating / 2).toFixed(1) : null
          const logo = api.imageUrl(t, 'logo', px(360))
          const genres = (t.genres ?? []).slice(0, 4).join(', ')
          const badge = audioBadge(h.play) ?? audioBadge(t)
          const label = playLabel(h.play)
          const playTo = isEpisode(h.play) || isMovie(h.play) ? `/watch/${h.play.id}` : itemPath(h.play)
          return (
            <div key={t.id} className={'hero__slide' + (i === index ? ' hero__slide--on' : '')} aria-hidden={i !== index}>
              {art && (
                <div className="hero__bg">
                  <picture>
                    {tall && <source media="(max-width: 799px)" srcSet={tall} />}
                    <img className="hero__art" src={art} alt="" />
                  </picture>
                </div>
              )}
              <div className="hero__grid">
                <div className="hero__head">
                  <Link to={itemPath(t)}>{logo ? <img className="hero__logo" src={logo} alt={t.title} /> : <h1 className="hero__title">{t.title}</h1>}</Link>
                </div>
                <div className="hero__body">
                  <div className="hero__meta">
                    {rating && <span className="hero__rating phone-only">★ {rating}</span>}
                    {t.officialRating && <span className="age">{ageLabel(t.officialRating)}</span>}
                    {badge && <span>• {badge}</span>}
                    {genres && <span>• {genres}</span>}
                  </div>
                  {t.overview && <p className="hero__synopsis">{t.overview}</p>}
                  <div className="hero__actions">
                    <button className="btn btn--brand" onClick={() => nav(playTo)}>
                      <IconPlay size={24} /> {label}
                    </button>
                    <SaveButton item={t} />
                  </div>
                </div>
              </div>
            </div>
          )
        })}
      </div>
      {count > 1 && (
        <>
          <button className="hero__arrow hero__arrow--left" onClick={() => pick(index - 1)} aria-label="Previous">
            <IconChevronLeft size={46} />
          </button>
          <button className="hero__arrow hero__arrow--right" onClick={() => pick(index + 1)} aria-label="Next">
            <IconChevronRight size={46} />
          </button>
        </>
      )}
      {/* Always there, so one slide and six leave the first row at the same place. */}
      <div className="hero__pills" style={{ ['--dwell' as string]: `${DWELL_MS}ms` }}>
        {count > 1 &&
          heroes.map((h, i) => (
            <button key={h.title.id + ':' + round} className={'hero__pill' + (i === index ? ' hero__pill--on' : i < index ? ' hero__pill--done' : '')} onClick={() => pick(i)} aria-label={`Slide ${i + 1}`}>
              <i />
            </button>
          ))}
      </div>
    </section>
  )
}
