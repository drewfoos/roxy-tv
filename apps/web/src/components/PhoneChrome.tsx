import type { ReactNode } from 'react'
import { Link, NavLink, useNavigate } from 'react-router'
import { api, px } from '../api/client'
import { useSession } from '../app/useSession'
import { Avatar } from './TopBar'
import { IconBookmarkOutline, IconClose, IconExplore, IconGrid, IconHome, IconMore, IconSearch, WitchHat } from './Icons'

/*
 * The phone app's chrome, translated from apps/android ui/phone (PhoneRoot.kt,
 * PhoneHome.kt, PhoneSeries.kt): everything here is shown only under 601 px.
 */

/** BottomBar (PhoneRoot.kt:437): five tabs, 56 dp over the safe area, #23252B, the current one in blue. */
export function BottomNav() {
  const s = useSession()
  const avatar = s?.profileId && s.profileHasAvatar ? api.profileAvatarUrl(s.profileId, px(26) * 2, s.profileAvatarKey) : null
  const cls = ({ isActive }: { isActive: boolean }) => 'bnav__tab' + (isActive ? ' bnav__tab--on' : '')
  return (
    <nav className="bnav phone-only" aria-label="Tabs">
      <NavLink to="/" end className={cls}>
        <IconHome size={26} />
        <span>Home</span>
      </NavLink>
      <NavLink to="/discover" className={cls}>
        <IconExplore size={26} />
        <span>Discover</span>
      </NavLink>
      <NavLink to="/browse" className={cls}>
        <IconGrid size={26} />
        <span>Browse</span>
      </NavLink>
      <NavLink to="/lists" className={cls}>
        <IconBookmarkOutline size={26} />
        <span>My Lists</span>
      </NavLink>
      <NavLink to="/account" className={cls}>
        <span className="bnav__avatar">
          <Avatar name={s?.profileName || s?.userName || '?'} url={avatar} size={24} />
        </span>
        <span>Account</span>
      </NavLink>
    </nav>
  )
}

/** HomeHeader (PhoneHome.kt:265): the hat and a search glyph pinned over the hero, solid after 160 px of scroll. */
export function PhoneHomeHeader() {
  return (
    <div className="phome phone-only">
      <Link to="/" aria-label="Home">
        <WitchHat size={32} />
      </Link>
      <span className="phome__spacer" />
      <Link to="/browse?search=1" aria-label="Search">
        <IconSearch size={26} />
      </Link>
    </div>
  )
}

/** The search glyph at the right of a tab page's title (TabHeader, PhoneUi.kt:371). */
export function TabSearchLink() {
  return (
    <Link to="/browse?search=1" className="tabhead__search phone-only" aria-label="Search">
      <IconSearch size={26} />
    </Link>
  )
}

/**
 * The title page's fixed header (TitlePageScaffold, PhoneSeries.kt:408): a white X, the
 * name fading in once the logo has scrolled under it, and the ⋮ menu. The background
 * and name opacities come from `--sy` in CSS.
 */
export function PhoneTitleHeader({ title, menu }: { title: string; menu?: ReactNode }) {
  const nav = useNavigate()
  const back = () => {
    if (window.history.length > 1) nav(-1)
    else nav('/')
  }
  return (
    <>
      <div className="ptitle phone-only">
        <button className="ptitle__icon" onClick={back} aria-label="Close">
          <IconClose size={24} />
        </button>
        <span className="ptitle__name">{title}</span>
        {menu ? (
          <span className="ptitle__icon ptitle__menu">
            <IconMore size={24} />
            {menu}
          </span>
        ) : (
          <span className="ptitle__icon" />
        )}
      </div>
      <div className="ptitle__fade phone-only" />
    </>
  )
}
