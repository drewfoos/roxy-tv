import { Component, Suspense, useEffect, type ReactNode } from 'react'
import { useLocation } from 'react-router'
import { useSession } from '../app/useSession'
import { lazyPage } from '../app/lazyPage'
import { music } from '../api/music'
import { useAsync } from '../hooks/useAsync'
import { player, restorePlayer } from './player'
import { usePlayer } from './usePlayer'

// The bar itself (with the queue, the lyrics and its styles) is fetched when there is
// something to play: a visit that never touches music does not carry it.
const MusicBar = lazyPage(() => import('./MusicBar'), (m) => m.MusicBar)

/** Whether this server has any music: the Music section is only shown where it does. */
export function useHasMusic(): boolean {
  const s = useSession()
  const home = useAsync(s?.profileId ? 'music-home' : null, () => music.home(), [], 60_000)
  return (home.data?.counts.tracks ?? 0) > 0
}

/**
 * Keeps the player bar on every page while there is a queue. A video takes over the
 * sound: on the watch page the music is paused and the bar steps aside.
 */
export function MusicBarHost() {
  const s = useSession()
  const loc = useLocation()
  const state = usePlayer()
  const profile = s?.profileId ?? null
  const video = loc.pathname.startsWith('/watch/')
  const show = profile != null && state.queue.index >= 0 && !video && loc.pathname !== '/profiles'
  useEffect(() => {
    if (profile) void restorePlayer()
  }, [profile])
  useEffect(() => {
    if (video) player.pause()
  }, [video])
  useEffect(() => {
    document.body.classList.toggle('has-mbar', show)
    return () => document.body.classList.remove('has-mbar')
  }, [show])
  if (!show) return null
  return (
    <Quiet>
      <Suspense fallback={null}>
        <MusicBar />
      </Suspense>
    </Quiet>
  )
}

/** A bar that fails to load or to draw leaves the page as it is, without a bar. */
class Quiet extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() {
    return { failed: true }
  }
  render() {
    return this.state.failed ? null : this.props.children
  }
}
