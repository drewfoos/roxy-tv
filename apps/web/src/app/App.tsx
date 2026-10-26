import { Navigate, Route, Routes, useLocation } from 'react-router'
import { useSession } from './useSession'
import { Login } from '../pages/Login'
import { Profiles } from '../pages/Profiles'
import { Home } from '../pages/Home'
import { Browse } from '../pages/Browse'
import { Discover } from '../pages/Discover'
import { Lists } from '../pages/Lists'
import { Account } from '../pages/Account'
import { Series } from '../pages/Series'
import { Movie } from '../pages/Movie'
import { Watch } from '../pages/Watch'
import { Legal } from '../pages/Legal'
import { useEffect, useState } from 'react'
import { api } from '../api/client'
import { Spinner } from '../components/Skeleton'

/** Signed out -> login; signed in without a profile -> "Who's watching?"; else the app. */
export default function App() {
  const s = useSession()
  const loc = useLocation()
  useEffect(() => {
    window.scrollTo(0, 0)
  }, [loc.pathname])
  // Browser-loaded art and video authenticate by cookie: get it before the first page
  // asks for a poster, or the first paint would be a row of broken images.
  const [cookieFor, setCookieFor] = useState<string | null>(null)
  useEffect(() => {
    const token = s?.token
    if (!token) return
    let live = true
    api
      .cookie()
      .catch(() => undefined)
      .then(() => live && setCookieFor(token))
    return () => {
      live = false
    }
  }, [s?.token])

  if (!s) return <Login />
  if (cookieFor !== s.token) return <Spinner />
  if (!s.profileId && loc.pathname !== '/profiles') return <Navigate to="/profiles" replace />

  return (
    <Routes>
      <Route path="/profiles" element={<Profiles />} />
      <Route path="/" element={<Home />} />
      <Route path="/browse" element={<Browse />} />
      <Route path="/discover" element={<Discover />} />
      <Route path="/lists" element={<Lists />} />
      <Route path="/account" element={<Account />} />
      <Route path="/series/:id" element={<Series />} />
      <Route path="/movie/:id" element={<Movie />} />
      <Route path="/watch/:id" element={<Watch />} />
      <Route path="/legal/:doc" element={<Legal />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
