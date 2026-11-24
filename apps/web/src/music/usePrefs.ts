import { useSyncExternalStore } from 'react'
import { musicPrefs, type MusicPrefs } from './prefs'

/** How music is played on this browser (quality, levelling, the visualizer's style). */
export function usePrefs(): MusicPrefs {
  return useSyncExternalStore(musicPrefs.subscribe, musicPrefs.get, musicPrefs.get)
}
