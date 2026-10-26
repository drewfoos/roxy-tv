import { useEffect, useState, type FormEvent } from 'react'
import { NavLink, useNavigate } from 'react-router'
import { api, px } from '../api/client'
import { session } from '../api/session'
import { useSession } from '../app/useSession'
import { IconSearch, WitchHat } from './Icons'

/**
 * The top bar: hat and name, Home / Discover / Browse / My Lists, search, the profile
 * avatar. Clear over Home's hero and solid once the page has scrolled, like the phone's.
 */
export function TopBar({ clearOverHero = false }: { clearOverHero?: boolean }) {
  const s = useSession()
  const nav = useNavigate()
  const [scrolled, setScrolled] = useState(() => window.scrollY > 40)
  const [q, setQ] = useState('')

  useEffect(() => {
    if (!clearOverHero) return
    const on = () => setScrolled(window.scrollY > 160)
    on()
    window.addEventListener('scroll', on, { passive: true })
    return () => window.removeEventListener('scroll', on)
  }, [clearOverHero])

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const t = q.trim()
    if (t) nav('/browse?q=' + encodeURIComponent(t))
  }

  const clear = clearOverHero && !scrolled
  return (
    <header className={'topbar' + (clear ? ' topbar--clear' : '')}>
      <NavLink to="/" className="topbar__brand">
        <WitchHat size={30} />
        <span>Roxy</span>
      </NavLink>
      <nav className="topbar__nav">
        {[
          ['/', 'Home'],
          ['/discover', 'Discover'],
          ['/browse', 'Browse'],
          ['/lists', 'My Lists'],
        ].map(([to, label]) => (
          <NavLink key={to} to={to} end={to === '/'} className={({ isActive }) => 'topbar__link' + (isActive ? ' topbar__link--active' : '')}>
            {label}
          </NavLink>
        ))}
      </nav>
      <div className="topbar__spacer" />
      <form className="topbar__search" onSubmit={submit} role="search">
        <IconSearch size={20} />
        <input value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search your library" aria-label="Search" />
      </form>
      <NavLink to="/account" className="topbar__icon" title={s?.profileName ?? 'Account'}>
        <Avatar name={s?.profileName ?? s?.userName ?? '?'} url={s?.profileId && s.profileHasAvatar ? api.profileAvatarUrl(s.profileId, px(32) * 2, s.profileAvatarKey) : null} size={32} />
      </NavLink>
    </header>
  )
}

export function Avatar({ name, url, size, selected = false }: { name: string; url: string | null; size: number; selected?: boolean }) {
  const [broken, setBroken] = useState(false)
  useEffect(() => setBroken(false), [url])
  return (
    <span className={'avatar' + (selected ? ' avatar--selected' : '')} style={{ width: size, height: size, fontSize: size * 0.42 }}>
      {url && !broken ? <img src={url} alt="" onError={() => setBroken(true)} /> : (name.trim()[0] ?? '?').toUpperCase()}
    </span>
  )
}

/** The X / Cast / menu header over a title page's art; solid with the name once scrolled past the logo. */
export function TitleHeader({ title, solidAfter = 420, children }: { title: string; solidAfter?: number; children?: React.ReactNode }) {
  const nav = useNavigate()
  const [solid, setSolid] = useState(false)
  useEffect(() => {
    const on = () => setSolid(window.scrollY > solidAfter)
    on()
    window.addEventListener('scroll', on, { passive: true })
    return () => window.removeEventListener('scroll', on)
  }, [solidAfter])
  return (
    <div className={'title__header' + (solid ? ' title__header--solid' : '')}>
      <button className="title__hicon" onClick={() => (window.history.length > 1 ? nav(-1) : nav('/'))} aria-label="Close">
        <svg width="24" height="24" viewBox="0 0 24 24" fill="currentColor"><path d="M19 6.41 17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z" /></svg>
      </button>
      <h2>{title}</h2>
      {children}
    </div>
  )
}

export function signOut() {
  api.logout().catch(() => undefined)
  session.set(null)
}
