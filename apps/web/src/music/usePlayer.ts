import { useSyncExternalStore } from 'react'
import { player, type PlayerState, type Progress } from './player'

/** The player's state: the queue, playing or paused, shuffle, repeat, volume. */
export function usePlayer(): PlayerState {
  return useSyncExternalStore(player.subscribe, player.get, player.get)
}

/**
 * Where the song is, a few times a second. Its own hook, so that only what draws the
 * time (the seek bar, the lyrics) is drawn again as it moves.
 */
export function useProgress(): Progress {
  return useSyncExternalStore(player.subscribeProgress, player.progress, player.progress)
}

/** The id of the song playing (or paused on), for lists that mark it. */
export function useCurrentTrackId(): string | null {
  return useSyncExternalStore(
    player.subscribe,
    () => {
      const s = player.get()
      return s.queue.index >= 0 ? (s.queue.tracks[s.queue.index]?.id ?? null) : null
    },
    () => null,
  )
}
