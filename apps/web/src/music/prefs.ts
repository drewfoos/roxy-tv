// How music is played on this browser: remembered here, not on the server (another
// computer has other speakers and another connection).
export type Quality = 'original' | 320 | 192 | 128 | 96
export type Levelling = 'off' | 'on'
export type VisualizerStyle = 'bars' | 'mirror' | 'wave' | 'ring' | 'milkdrop'

export interface MusicPrefs {
  /** What is asked of the server: the file itself, or a conversion at about this many kbit/s. */
  quality: Quality
  /** Songs brought to the same loudness. */
  levelling: Levelling
  /** Songs of an album that run into each other joined without a gap. */
  gapless: boolean
  visualizer: VisualizerStyle
}

export const QUALITIES: { id: Quality; label: string; hint: string }[] = [
  { id: 'original', label: 'Original', hint: 'The file as it is' },
  { id: 320, label: 'High', hint: 'About 320 kbit/s' },
  { id: 192, label: 'Normal', hint: 'About 192 kbit/s' },
  { id: 128, label: 'Low', hint: 'About 128 kbit/s' },
  { id: 96, label: 'Data saver', hint: 'About 96 kbit/s' },
]

export const VISUALIZERS: { id: VisualizerStyle; label: string }[] = [
  { id: 'bars', label: 'Bars' },
  { id: 'mirror', label: 'Mirror' },
  { id: 'wave', label: 'Wave' },
  { id: 'ring', label: 'Ring' },
  // The pictures Winamp drew to music (music/Milkdrop.tsx): fetched when picked.
  { id: 'milkdrop', label: 'Milkdrop' },
]

const KEY = 'roxy.music.prefs'
const defaults: MusicPrefs = { quality: 'original', levelling: 'on', gapless: true, visualizer: 'bars' }

function read(): MusicPrefs {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? 'null') as Partial<MusicPrefs> | null
    if (!v || typeof v !== 'object') return defaults
    return {
      quality: QUALITIES.some((q) => q.id === v.quality) ? (v.quality as Quality) : defaults.quality,
      levelling: v.levelling === 'off' ? 'off' : 'on',
      gapless: v.gapless !== false,
      visualizer: VISUALIZERS.some((x) => x.id === v.visualizer) ? (v.visualizer as VisualizerStyle) : defaults.visualizer,
    }
  } catch {
    return defaults
  }
}

let current: MusicPrefs | null = null
const listeners = new Set<() => void>()

export const musicPrefs = {
  get(): MusicPrefs {
    return (current ??= read())
  },
  set(patch: Partial<MusicPrefs>) {
    current = { ...musicPrefs.get(), ...patch }
    try {
      localStorage.setItem(KEY, JSON.stringify(current))
    } catch {
      /* not remembered */
    }
    listeners.forEach((l) => l())
  },
  subscribe(l: () => void) {
    listeners.add(l)
    return () => {
      listeners.delete(l)
    }
  },
  /** For tests: as a browser that has never been here. */
  reset() {
    current = null
  },
}
