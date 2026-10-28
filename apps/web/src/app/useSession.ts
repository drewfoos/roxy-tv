import { useSyncExternalStore } from 'react'
import { session, type Session } from '../api/session'

/** The signed-in state as React state: re-renders on sign in, profile switch, sign out. */
export function useSession(): Session | null {
  return useSyncExternalStore(session.subscribe, session.get, session.get)
}
