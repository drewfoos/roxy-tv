// The profile's playlists changed (one was made, renamed, deleted, added to): the lists
// of them that are on screen ask the server again. A change is often made from a menu
// far from the page that shows its result (a song's ⋮ on the playlist's own page).
import { useSyncExternalStore } from 'react'

let version = 0
const listeners = new Set<() => void>()

export function playlistsChanged() {
  version++
  listeners.forEach((l) => l())
}

/** A number that goes up with every change: put it among a load's dependencies. */
export function usePlaylistsVersion(): number {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l)
      return () => {
        listeners.delete(l)
      }
    },
    () => version,
    () => version,
  )
}
