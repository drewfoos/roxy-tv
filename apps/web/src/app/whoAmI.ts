import { useEffect } from 'react'
import { api } from '../api/client'
import { session } from '../api/session'

/** How often, at most, the server is asked who this login is now. */
export const WHO_GAP_MS = 60_000

/**
 * The login's name and level as the server has them now. The browser remembered "admin or
 * not" from the moment it signed in: someone made an admin saw no Server pages until they
 * signed out and in, and someone no longer an admin kept the links (each page then said
 * "admin only"). The answer is only used for the login it was asked for.
 */
export async function refreshLogin(): Promise<void> {
  const before = session.get()
  if (!before) return
  const me = await api.me()
  const now = session.get()
  if (!now || now.token !== before.token) return
  const isAdmin = !!me.isAdmin
  const userName = me.name || now.userName
  if (now.isAdmin !== isAdmin || now.userName !== userName) session.update({ isAdmin, userName })
}

/** Asks when the app opens and whenever the tab is come back to, at most once a minute. */
export function useLoginRefresh(token: string | undefined) {
  useEffect(() => {
    if (!token) return
    let last = 0
    const look = () => {
      if (document.hidden || Date.now() - last < WHO_GAP_MS) return
      last = Date.now()
      void refreshLogin().catch(() => undefined) // away or refused: the pages say so themselves
    }
    look()
    document.addEventListener('visibilitychange', look)
    window.addEventListener('focus', look)
    return () => {
      document.removeEventListener('visibilitychange', look)
      window.removeEventListener('focus', look)
    }
  }, [token])
}
