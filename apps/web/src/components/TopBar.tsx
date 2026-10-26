import { useEffect, useRef, useState, type FormEvent } from 'react'
import { NavLink, useNavigate } from 'react-router'
import { api, px } from '../api/client'
import { session } from '../api/session'
import { useSession } from '../app/useSession'
import { IconBookmarkOutline, IconClose, IconEdit, IconExpand, IconHistory, IconLogout, IconSearch, IconSettings, IconSwap, WitchHat } from './Icons'

/**
 * The top bar, laid out like Crunchyroll's: the mark and name (Home), Discover and
 * Browse, then at the right a search that opens on click, the bookmark (My Lists)
 * and the avatar with a caret that opens the account drawer. Clear over Home's hero
 * and solid once scrolled.
 */
export function TopBar({ clearOverHero = false }: { clearOverHero?: boolean }) {
  const s = useSession()
  const nav = useNavigate()
  const [scrolled, setScrolled] = useState(() => window.scrollY > 40)
  const [searching, setSearching] = useState(false)
  const [q, setQ] = useState('')
  const [drawer, setDrawer] = useState(false)
  const searchRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!clearOverHero) return
    const on = () => setScrolled(window.scrollY > 160)
    on()
    window.addEventListener('scroll', on, { passive: true })
    return () => window.removeEventListener('scroll', on)
  }, [clearOverHero])
  useEffect(() => {
    if (searching) searchRef.current?.focus()
  }, [searching])
  useEffect(() => {
    if (!drawer) return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setDrawer(false)
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [drawer])

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const t = q.trim()
    if (t) {
      nav('/browse?q=' + encodeURIComponent(t))
      setSearching(false)
    }
  }

  const clear = clearOverHero && !scrolled && !drawer
  const avatar = s?.profileId && s.profileHasAvatar ? api.profileAvatarUrl(s.profileId, px(40) * 2, s.profileAvatarKey) : null
  const name = s?.profileName || s?.userName || '?'
  return (
    <>
      <header className={'topbar' + (clear ? ' topbar--clear' : '')}>
        <NavLink to="/" className="topbar__brand">
          <WitchHat size={30} />
          <span>Roxy</span>
        </NavLink>
        <nav className="topbar__nav">
          <NavLink to="/discover" className={({ isActive }) => 'topbar__link' + (isActive ? ' topbar__link--active' : '')}>
            Discover
          </NavLink>
          <NavLink to="/browse" className={({ isActive }) => 'topbar__link' + (isActive ? ' topbar__link--active' : '')}>
            Browse
          </NavLink>
          <span className="topbar__sep" />
          <NavLink to="/browse?type=movie" className="topbar__link">
            Movies
          </NavLink>
        </nav>
        <div className="topbar__spacer" />
        {searching ? (
          <form className="topbar__search" onSubmit={submit} role="search">
            <IconSearch size={20} />
            <input ref={searchRef} value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search your library" aria-label="Search" onBlur={() => !q && setSearching(false)} />
            <button type="button" className="topbar__icon topbar__icon--small" onClick={() => { setQ(''); setSearching(false) }} aria-label="Close search">
              <IconClose size={20} />
            </button>
          </form>
        ) : (
          <button className="topbar__icon" onClick={() => setSearching(true)} aria-label="Search">
            <IconSearch size={26} />
          </button>
        )}
        <NavLink to="/lists" className={({ isActive }) => 'topbar__icon' + (isActive ? ' topbar__icon--active' : '')} aria-label="My Lists" title="My Lists">
          <IconBookmarkOutline size={26} />
        </NavLink>
        <button className={'topbar__account' + (drawer ? ' topbar__account--open' : '')} onClick={() => setDrawer((d) => !d)} aria-label="Account menu" aria-expanded={drawer}>
          <Avatar name={name} url={avatar} size={34} />
          <IconExpand size={20} />
        </button>
      </header>
      {drawer && (
        <>
          <div className="drawer__backdrop" onClick={() => setDrawer(false)} />
          <aside className="drawer">
            <div className="drawer__profile">
              <Avatar name={name} url={avatar} size={56} />
              <b>{name}</b>
              <NavLink to="/account" className="drawer__edit" onClick={() => setDrawer(false)} aria-label="Edit profile">
                <IconEdit size={20} />
              </NavLink>
            </div>
            <div className="drawer__group">
              <NavLink to="/profiles" className="drawer__item" onClick={() => setDrawer(false)}>
                <IconSwap size={22} /> Switch Profile
              </NavLink>
              <NavLink to="/account" className="drawer__item" onClick={() => setDrawer(false)}>
                <IconSettings size={22} /> Settings
              </NavLink>
            </div>
            <div className="drawer__group">
              <NavLink to="/lists" className="drawer__item" onClick={() => setDrawer(false)}>
                <IconBookmarkOutline size={22} /> Watchlist
              </NavLink>
              <NavLink to="/lists?tab=history" className="drawer__item" onClick={() => setDrawer(false)}>
                <IconHistory size={22} /> History
              </NavLink>
            </div>
            <div className="drawer__group">
              <button className="drawer__item" onClick={signOut}>
                <IconLogout size={22} /> Log Out
              </button>
            </div>
          </aside>
        </>
      )}
    </>
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

export function signOut() {
  api.logout().catch(() => undefined)
  session.set(null)
}
