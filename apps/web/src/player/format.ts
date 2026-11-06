import { languageName } from '../api/items'
import type { MediaStream } from '../api/types'

/** 1:05:09, or 5:09 under an hour. */
export function fmt(sec: number): string {
  if (!isFinite(sec) || sec < 0) sec = 0
  const s = Math.floor(sec % 60)
  const m = Math.floor((sec / 60) % 60)
  const h = Math.floor(sec / 3600)
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}` : `${m}:${String(s).padStart(2, '0')}`
}

/** "Japanese · Signs (stereo)": a track's language, title and, for audio, its channels. */
export function trackLabel(s: MediaStream): string {
  const lang = s.detectedLanguage || s.language
  const parts = [lang ? languageName(lang) : null, s.title && !/^(und|unknown)$/i.test(s.title) ? s.title : null]
  const label = parts.filter(Boolean).join(' · ') || `Track ${s.index}`
  return s.type === 'audio' && s.channels ? `${label} (${s.channels > 2 ? `${s.channels - 1}.1` : s.channels === 2 ? 'stereo' : 'mono'})` : label
}
