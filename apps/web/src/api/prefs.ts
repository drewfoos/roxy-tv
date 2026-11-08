// Playback preferences for this browser (the phone keeps the same set in its DataStore;
// neither syncs to the server). One listener set so pages and the player re-render on
// change, and a one-time migration of the old `roxy.autoplay` key.
import { useSyncExternalStore } from 'react'

export type SubMode = 'signs' | 'foreign' | 'full' | 'none'
export type QualityPref = 'original' | 2160 | 1440 | 1080 | 720 | 480

export interface Prefs {
  /** ISO 639-2 of the audio to pick when a title has more than one. */
  audioLang: string
  /** ISO 639-2 of the subtitles to pick. */
  subLang: string
  subMode: SubMode
  quality: QualityPref
  autoSkip: boolean
  /** Seconds the Skip button stays; 0 = the whole intro / outro. */
  skipWindowSec: number
  autoplay: boolean
}

export const DEFAULT_PREFS: Prefs = { audioLang: 'eng', subLang: 'eng', subMode: 'full', quality: 'original', autoSkip: false, skipWindowSec: 20, autoplay: true }

export const SUB_MODES: { id: SubMode; label: string; hint: string }[] = [
  { id: 'signs', label: 'Signs & songs with a dub', hint: 'Full subtitles with the original audio; only signs and songs when you watch a dub' },
  { id: 'foreign', label: 'Foreign audio only', hint: 'Subtitles only when the audio is not in your language' },
  { id: 'full', label: 'Always full dialogue', hint: 'Every line, whatever the audio' },
  { id: 'none', label: 'Off', hint: 'No subtitles until you turn them on in the player' },
]

export const QUALITY_PREFS: { id: QualityPref; label: string }[] = [
  { id: 'original', label: 'Best (the file as it is)' },
  { id: 2160, label: '4K' },
  { id: 1440, label: '1440p' },
  { id: 1080, label: '1080p' },
  { id: 720, label: '720p' },
  { id: 480, label: '480p' },
]

export const SKIP_WINDOWS: { id: number; label: string }[] = [
  { id: 5, label: '5 seconds' },
  { id: 10, label: '10 seconds' },
  { id: 15, label: '15 seconds' },
  { id: 20, label: '20 seconds' },
  { id: 30, label: '30 seconds' },
  { id: 0, label: 'Whole intro / outro' },
]

const KEY = 'roxy.prefs'
const listeners = new Set<() => void>()
let cached: Prefs | undefined

function read(): Prefs {
  if (cached) return cached
  let p: Prefs = { ...DEFAULT_PREFS }
  try {
    const raw = localStorage.getItem(KEY)
    if (raw) p = { ...p, ...(JSON.parse(raw) as Partial<Prefs>) }
    else if (localStorage.getItem('roxy.autoplay') === 'off') p.autoplay = false
    // Defaults that changed: every save stores the whole set, so a save made before carried
    // the old default. v2 (2026-10-28): full subtitles. v3 (2026-11-08): the Skip button
    // stays 20 s, not 15 (a saved 15 was the old default; any other choice is kept).
    const v = Number(localStorage.getItem('roxy.prefs.v')) || 1
    if (v < 3) {
      if (v < 2) p.subMode = 'full'
      if (p.skipWindowSec === 15) p.skipWindowSec = 20
      localStorage.setItem('roxy.prefs.v', '3')
      if (raw) localStorage.setItem(KEY, JSON.stringify(p))
    }
  } catch {
    /* defaults */
  }
  cached = p
  return p
}

export const prefs = {
  get: read,
  set(patch: Partial<Prefs>) {
    cached = { ...read(), ...patch }
    try {
      localStorage.setItem(KEY, JSON.stringify(cached))
    } catch {
      /* private mode */
    }
    listeners.forEach((l) => l())
  },
  subscribe(l: () => void) {
    listeners.add(l)
    return () => {
      listeners.delete(l)
    }
  },
}

export function usePrefs(): Prefs {
  return useSyncExternalStore(prefs.subscribe, prefs.get, prefs.get)
}

/** Two-letter codes some files carry, folded to the three-letter ones the picker uses. */
const ISO1: Record<string, string> = { en: 'eng', ja: 'jpn', es: 'spa', fr: 'fra', de: 'deu', pt: 'por', it: 'ita', ru: 'rus', ko: 'kor', zh: 'zho', ar: 'ara', hi: 'hin', pl: 'pol', tr: 'tur', nl: 'nld', sv: 'swe' }
export function lang3(code: string | undefined | null): string {
  const c = (code ?? '').toLowerCase().split('-')[0]
  if (!c || c === 'und') return ''
  if (c.length === 2) return ISO1[c] ?? c
  if (c === 'jap') return 'jpn'
  if (c === 'ger') return 'deu'
  if (c === 'fre') return 'fra'
  if (c === 'chi') return 'zho'
  if (c === 'dut') return 'nld'
  return c
}

/** The languages the pickers offer: the usual anime set first, then more. */
export const LANGUAGES: { code: string; name: string }[] = [
  { code: 'eng', name: 'English' },
  { code: 'jpn', name: 'Japanese' },
  { code: 'spa', name: 'Spanish' },
  { code: 'por', name: 'Portuguese' },
  { code: 'fra', name: 'French' },
  { code: 'deu', name: 'German' },
  { code: 'ita', name: 'Italian' },
  { code: 'rus', name: 'Russian' },
  { code: 'kor', name: 'Korean' },
  { code: 'zho', name: 'Chinese' },
  { code: 'ara', name: 'Arabic' },
  { code: 'hin', name: 'Hindi' },
  { code: 'pol', name: 'Polish' },
  { code: 'tur', name: 'Turkish' },
  { code: 'nld', name: 'Dutch' },
  { code: 'swe', name: 'Swedish' },
  { code: 'tha', name: 'Thai' },
  { code: 'vie', name: 'Vietnamese' },
  { code: 'ind', name: 'Indonesian' },
  { code: 'tam', name: 'Tamil' },
  { code: 'tel', name: 'Telugu' },
]
