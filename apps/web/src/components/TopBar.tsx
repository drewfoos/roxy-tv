import { useEffect, useRef, useState, type FormEvent } from 'react'
import { NavLink, useNavigate } from 'react-router'
import { api, px } from '../api/client'
import { session } from '../api/session'
import { useSession } from '../app/useSession'
import { IconBookmarkOutline, IconClose, IconEdit, IconExpand, IconExplore, IconGrid, IconHistory, IconHome, IconLogout, IconMenu, IconSearch, IconSettings, IconSwap, WitchHat } from './Icons'
import { BottomNav, PhoneHomeHeader } from './PhoneChrome'

/**
 * The top bar, measured from Crunchyroll's: a solid grey bar the full width with no
 * gutters, every control a "tile" the bar's full height that darkens on hover. Left:
 * the mark and name (Home), then Discover, Browse | Movies. Right, flush with the edge:
 * search (opens a field), the bookmark (My Lists), the avatar with a caret (account
 * drawer). Under 1024 px the links move into a menu behind a hamburger at the left.
 *
 * Under 601 px the page follows the phone app instead (`phone`): 'home' = the pinned
 * hat + search header and the bottom tabs; 'tab' = the bottom tabs under the page's
 * own title; 'title' = nothing here (the title page draws its own X header); 'bar'
 * keeps this bar (watch, legal, sign-in pages); 'page' = a pushed page that draws its
 * own back header (account sub-pages).
 */
export type PhoneChrome = 'bar' | 'home' | 'tab' | 'title' | 'page'

export function TopBar({ phone = 'bar' }: { phone?: PhoneChrome }) {
  const s = useSession()
  const nav = useNavigate()
  const [searching, setSearching] = useState(false)
  const [q, setQ] = useState('')
  const [drawer, setDrawer] = useState<'none' | 'account' | 'menu'>('none')
  const searchRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (searching) searchRef.current?.focus()
  }, [searching])
  useEffect(() => {
    if (drawer === 'none') return
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setDrawer('none')
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
  const close = () => setDrawer('none')
  const avatar = s?.profileId && s.profileHasAvatar ? api.profileAvatarUrl(s.profileId, px(40) * 2, s.profileAvatarKey) : null
  const name = s?.profileName || s?.userName || '?'
  const link = ({ isActive }: { isActive: boolean }) => 'tile tile--link' + (isActive ? ' tile--active' : '')

  return (
    <>
      {phone === 'home' && <PhoneHomeHeader />}
      {(phone === 'home' || phone === 'tab') && <BottomNav />}
      <header className={'topbar' + (phone !== 'bar' ? ' topbar--desk' : '')}>
        <button className="tile tile--icon topbar__menu" onClick={() => setDrawer((d) => (d === 'menu' ? 'none' : 'menu'))} aria-label="Menu" aria-expanded={drawer === 'menu'}>
          <IconMenu size={26} />
        </button>
        <NavLink to="/" className="tile topbar__brand">
          <WitchHat size={30} />
          <span>Roxy</span>
        </NavLink>
        <nav className="topbar__nav">
          <NavLink to="/discover" className={link}>
            Discover
          </NavLink>
          <NavLink to="/browse" end className={link}>
            Browse
          </NavLink>
          <span className="topbar__sep" />
          <NavLink to="/browse?type=movie" className={link}>
            Movies
          </NavLink>
        </nav>
        <div className="topbar__spacer" />
        {searching ? (
          <form className="topbar__search" onSubmit={submit} role="search">
            <IconSearch size={22} />
            <input ref={searchRef} value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search your library" aria-label="Search" />
            <button type="button" className="topbar__search-close" onClick={() => { setQ(''); setSearching(false) }} aria-label="Close search">
              <IconClose size={22} />
            </button>
          </form>
        ) : (
          <button className="tile tile--icon" onClick={() => setSearching(true)} aria-label="Search">
            <IconSearch size={24} />
          </button>
        )}
        <NavLink to="/lists" className={({ isActive }) => 'tile tile--icon' + (isActive ? ' tile--active' : '')} aria-label="My Lists" title="My Lists">
          <IconBookmarkOutline size={24} />
        </NavLink>
        <button className={'tile tile--account' + (drawer === 'account' ? ' tile--open' : '')} onClick={() => setDrawer((d) => (d === 'account' ? 'none' : 'account'))} aria-label="Account menu" aria-expanded={drawer === 'account'}>
          <Avatar name={name} url={avatar} size={32} />
          <IconExpand size={20} />
        </button>
      </header>
      {drawer !== 'none' && <div className="drawer__backdrop" onClick={close} />}
      {drawer === 'account' && (
        <aside className="drawer drawer--right">
          <div className="drawer__profile">
            <Avatar name={name} url={avatar} size={56} />
            <b>{name}</b>
            <NavLink to="/account/profiles" className="drawer__edit" onClick={close} aria-label="Edit profile">
              <IconEdit size={20} />
            </NavLink>
          </div>
          <div className="drawer__group">
            <NavLink to="/profiles" className="drawer__item" onClick={close}>
              <IconSwap size={22} /> Switch Profile
            </NavLink>
            <NavLink to="/account/preferences" className="drawer__item" onClick={close}>
              <IconSettings size={22} /> Settings
            </NavLink>
          </div>
          <div className="drawer__group">
            <NavLink to="/lists" className="drawer__item" onClick={close}>
              <IconBookmarkOutline size={22} /> Watchlist
            </NavLink>
            <NavLink to="/lists?tab=history" className="drawer__item" onClick={close}>
              <IconHistory size={22} /> History
            </NavLink>
          </div>
          <div className="drawer__group">
            <button className="drawer__item" onClick={signOut}>
              <IconLogout size={22} /> Log Out
            </button>
          </div>
        </aside>
      )}
      {drawer === 'menu' && (
        <aside className="drawer drawer--left">
          <div className="drawer__group">
            <NavLink to="/" end className="drawer__item" onClick={close}>
              <IconHome size={22} /> Home
            </NavLink>
            <NavLink to="/discover" className="drawer__item" onClick={close}>
              <IconExplore size={22} /> Discover
            </NavLink>
            <NavLink to="/browse" end className="drawer__item" onClick={close}>
              <IconGrid size={22} /> Browse Series
            </NavLink>
            <NavLink to="/browse?type=movie" className="drawer__item" onClick={close}>
              <IconGrid size={22} /> Browse Movies
            </NavLink>
          </div>
          <div className="drawer__group">
            <NavLink to="/lists" className="drawer__item" onClick={close}>
              <IconBookmarkOutline size={22} /> My Lists
            </NavLink>
            <NavLink to="/account/preferences" className="drawer__item" onClick={close}>
              <IconSettings size={22} /> Settings
            </NavLink>
          </div>
        </aside>
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
