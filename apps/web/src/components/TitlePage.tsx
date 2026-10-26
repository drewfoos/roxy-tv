import { useState, type ReactNode } from 'react'
import { Link } from 'react-router'
import { api, px } from '../api/client'
import { ageLabel, languageName, primarySource } from '../api/items'
import type { Item } from '../api/types'
import { IconMore } from './Icons'

/**
 * The frame every title page shares, laid out like Crunchyroll's: the key art fills the
 * top with a dark fade from the left; over its left side the logo (or name), the line
 * "16+ • Dub | Sub • Adventure, Comedy", the star rating, and the actions row. Under
 * the art, two columns: the synopsis with MORE DETAILS, and the audio / subtitle
 * languages.
 */
export function TitleBanner({ item, badge, extra, actions, menu }: { item: Item; badge: string | null; extra?: string | null; actions?: ReactNode; menu?: ReactNode }) {
  const url = api.imageUrl(item, 'backdrop', px(1800)) ?? api.imageUrl(item, 'poster', px(1000))
  const logo = api.imageUrl(item, 'logo', px(360))
  const genres = (item.genres ?? []).slice(0, 4)
  return (
    <div className="title__banner">
      {url && <img src={url} alt="" />}
      {menu && (
        <div className="title__more">
          <IconMore size={20} /> More{menu}
        </div>
      )}
      <div className="title__over">
        {logo ? <img className="title__logo" src={logo} alt={item.title} /> : <h1 className="title__name">{item.title}</h1>}
        <div className="title__facts">
          {item.officialRating && <span className="age">{ageLabel(item.officialRating)}</span>}
          {badge && <span>• {badge}</span>}
          {genres.length > 0 && (
            <span>
              •{' '}
              {genres.map((g, i) => (
                <span key={g}>
                  {i > 0 && ', '}
                  <Link to={'/browse?q=' + encodeURIComponent(g)}>{g}</Link>
                </span>
              ))}
            </span>
          )}
          {extra && <span>• {extra}</span>}
        </div>
        {item.communityRating != null && (
          <div className="title__facts">
            <Stars value={item.communityRating / 2} />
            <b>Average rating: {(item.communityRating / 2).toFixed(1)}</b>
            {item.year && <span className="muted">• {item.year}</span>}
          </div>
        )}
        {actions && <div className="title__actions">{actions}</div>}
      </div>
    </div>
  )
}

function Stars({ value }: { value: number }) {
  return (
    <span aria-label={`${value.toFixed(1)} of 5`} style={{ color: '#fff', letterSpacing: 2, fontSize: 18, lineHeight: '18px' }}>
      {Array.from({ length: 5 }).map((_, i) => (
        <span key={i} style={{ opacity: i < Math.round(value) ? 1 : 0.3 }}>
          ★
        </span>
      ))}
    </span>
  )
}

export function TitleBelow({ item, children }: { item: Item; children?: ReactNode }) {
  const [expanded, setExpanded] = useState(false)
  const src = primarySource(item)
  const audio = Array.from(new Set((item.audioLanguages?.length ? item.audioLanguages : (src?.streams ?? []).filter((s) => s.type === 'audio').map((s) => s.language ?? '')).filter((l) => l && l !== 'und')))
  const subs = Array.from(new Set((src?.streams ?? []).filter((s) => s.type === 'subtitle').map((s) => s.detectedLanguage || s.language || '').filter((l) => l && l !== 'und')))
  const long = (item.overview ?? '').length > 320
  return (
    <div className="title__below">
      <div>
        {item.overview && <p className={'title__synopsis' + (expanded || !long ? '' : ' title__synopsis--clamped')}>{item.overview}</p>}
        {long && (
          <button className="link" style={{ marginTop: 10 }} onClick={() => setExpanded((e) => !e)}>
            {expanded ? 'Fewer details' : 'More details'}
          </button>
        )}
        {children}
      </div>
      <div className="title__kv">
        {audio.length > 0 && (
          <div>
            <b>Audio:</b> {audio.map(languageName).join(', ')}
          </div>
        )}
        {subs.length > 0 && (
          <div>
            <b>Subtitles:</b> {subs.map(languageName).join(', ')}
          </div>
        )}
        {item.studio && (
          <div>
            <b>Studio:</b> {item.studio}
          </div>
        )}
        {item.status && (
          <div>
            <b>Status:</b> {item.status}
          </div>
        )}
      </div>
    </div>
  )
}

/** Crunchyroll's tab strip under the info block. */
export function SectionTabs({ tabs, selected, onSelect }: { tabs: string[]; selected: number; onSelect: (i: number) => void }) {
  return (
    <div className="sectiontabs">
      {tabs.map((t, i) => (
        <button key={t} className={i === selected ? 'on' : ''} onClick={() => onSelect(i)}>
          {t}
        </button>
      ))}
    </div>
  )
}
