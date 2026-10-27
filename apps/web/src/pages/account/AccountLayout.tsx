import { Link, NavLink, Navigate, Route, Routes, useLocation } from 'react-router'
import { useEffect, useState } from 'react'
import { IconBack } from '../../components/Icons'
import { PhoneAccountIndex } from './PhoneIndex'
import { useSession } from '../../app/useSession'
import { TopBar } from '../../components/TopBar'
import { Footer } from '../../components/Footer'
import { ProfilesPage } from './Profiles'
import { PreferencesPage } from './Preferences'
import { DevicesPage } from './Devices'
import { AboutPage } from './About'
import { PasswordPage } from './Password'
import { LogoutPage } from './Logout'
import { RequestsPage } from './Requests'
import { UsersPage } from './Users'
import { LibraryPage } from './Library'
import { ArrPage } from './Arr'
import { TranscodingPage } from './Transcoding'
import { KeysPage } from './Keys'
import { WebhooksPage } from './Webhooks'

interface NavItem {
  to: string
  label: string
}

const GENERAL: NavItem[] = [
  { to: 'profiles', label: 'Profiles' },
  { to: 'preferences', label: 'Preferences' },
  { to: 'devices', label: 'Device Management' },
  { to: 'about', label: 'About' },
]
const ACCOUNT: NavItem[] = [
  { to: 'password', label: 'Password' },
  { to: 'logout', label: 'Log Out' },
]
const SERVER: NavItem[] = [
  { to: 'requests', label: 'Requests' },
  { to: 'users', label: 'Users' },
  { to: 'library', label: 'Library' },
  { to: 'arr', label: 'Sonarr & Radarr' },
  { to: 'transcoding', label: 'Transcoding' },
  { to: 'keys', label: 'TMDB & fanart.tv' },
  { to: 'webhooks', label: 'Webhooks' },
]

/**
 * Account Settings, laid out like Crunchyroll's: the title, a left column of grouped
 * links (General, Account, and Server for admins) and a dark panel with a centred
 * heading. Under 1024 px the links become a scrolling strip above the panel.
 */
/** True under 601 px, tracking the viewport, so the index can be the phone's list there. */
function usePhone() {
  const [phone, setPhone] = useState(() => window.matchMedia('(max-width: 600px)').matches)
  useEffect(() => {
    const mq = window.matchMedia('(max-width: 600px)')
    const on = () => setPhone(mq.matches)
    mq.addEventListener('change', on)
    return () => mq.removeEventListener('change', on)
  }, [])
  return phone
}

export function AccountSection() {
  const s = useSession()
  const phone = usePhone()
  const loc = useLocation()
  const atIndex = /^\/account\/?$/.test(loc.pathname)
  const current = [...GENERAL, ...ACCOUNT, ...SERVER].find((it) => loc.pathname.startsWith('/account/' + it.to))
  if (!s) return null
  const group = (title: string, items: NavItem[]) => (
    <div className="acc-nav__group" key={title}>
      <h2>{title}</h2>
      {items.map((it) => (
        <NavLink key={it.to} to={'/account/' + it.to} className={({ isActive }) => 'acc-nav__link' + (isActive ? ' acc-nav__link--on' : '')}>
          {it.label}
        </NavLink>
      ))}
    </div>
  )
  return (
    <div className="page page--phone">
      <TopBar phone={atIndex ? 'tab' : 'page'} />
      {phone && atIndex ? (
        <PhoneAccountIndex />
      ) : (
      <div className="acc">
        {!atIndex && (
          <div className="acc-phonehead phone-only">
            <Link to="/account" aria-label="Back to Account">
              <IconBack size={24} />
            </Link>
            <span>{current?.label ?? 'Account'}</span>
          </div>
        )}
        <h1 className="acc__title">Account Settings</h1>
        <div className="acc__body">
          <nav className="acc-nav" aria-label="Account settings">
            {group('General', GENERAL)}
            {group('Account', ACCOUNT)}
            {s.isAdmin && group('Server', SERVER)}
          </nav>
          <div className="acc-panel">
            <Routes>
              <Route index element={phone ? <PhoneAccountIndex /> : <Navigate to="profiles" replace />} />
              <Route path="profiles" element={<ProfilesPage />} />
              <Route path="preferences" element={<PreferencesPage />} />
              <Route path="devices" element={<DevicesPage />} />
              <Route path="about" element={<AboutPage />} />
              <Route path="password" element={<PasswordPage />} />
              <Route path="logout" element={<LogoutPage />} />
              {s.isAdmin && (
                <>
                  <Route path="requests" element={<RequestsPage />} />
                  <Route path="users" element={<UsersPage />} />
                  <Route path="library" element={<LibraryPage />} />
                  <Route path="arr" element={<ArrPage />} />
                  <Route path="transcoding" element={<TranscodingPage />} />
                  <Route path="keys" element={<KeysPage />} />
                  <Route path="webhooks" element={<WebhooksPage />} />
                </>
              )}
              <Route path="*" element={<Navigate to="profiles" replace />} />
            </Routes>
          </div>
        </div>
      </div>
      )}
      <Footer />
    </div>
  )
}
