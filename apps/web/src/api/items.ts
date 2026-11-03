// Item helpers shared by every page, mirroring the Android app's Item extensions so the
// two clients read a title the same way.
import type { Item, MediaSource, MediaStream } from './types'

export const isEpisode = (it: Item) => it.type === 'episode'
export const isSeries = (it: Item) => it.type === 'series'
export const isMovie = (it: Item) => it.type === 'movie'
export const isSeason = (it: Item) => it.type === 'season'

export const isPlayable = (it: Item) => (it.hasMedia ?? true) && it.availability !== 'missing' && it.availability !== 'unaired'

export function primarySource(it: Item): MediaSource | undefined {
  const s = it.mediaSources ?? []
  return s.find((x) => x.isPrimary) ?? s[0]
}

const pad2 = (n: number) => String(n).padStart(2, '0')

/** "S01E03" for episodes ("S01" when the episode number is unknown), "" otherwise. */
export function episodeCode(it: Item): string {
  if (!isEpisode(it)) return ''
  const e = it.indexAired ?? it.indexAbsolute
  const s = it.seasonAired ?? 0
  return e != null ? `S${pad2(s)}E${pad2(e)}` : `S${pad2(s)}`
}

/** "E3" style number for the episode page header. */
export function episodeNumber(it: Item): string | null {
  const e = it.indexAired ?? it.indexAbsolute
  return e != null ? `E${e}` : null
}

/** Whole minutes, or null when unknown. */
export function runtimeMinutes(it: Item): number | null {
  const ms = it.runtimeMs ?? primarySource(it)?.durationMs ?? 0
  if (ms <= 0) return null
  const m = Math.round(ms / 60000)
  return m > 0 ? m : null
}

/** "1h 42m" / "24m". */
export function runtimeLabel(it: Item): string | null {
  const m = runtimeMinutes(it)
  if (m == null) return null
  return m >= 60 ? `${Math.floor(m / 60)}h ${m % 60}m` : `${m}m`
}

/** 0..1 for a resumable position, 1 when played, null otherwise (the bar under a thumb). */
export function progressFraction(it: Item): number | null {
  const ud = it.userData
  if (!ud) return null
  const len = it.runtimeMs ?? primarySource(it)?.durationMs ?? 0
  if (ud.positionMs > 0 && len > 0) return Math.min(1, Math.max(0, ud.positionMs / len))
  return ud.played ? 1 : null
}

/** "12m left" for a resumable episode. */
export function timeLeftLabel(it: Item): string | null {
  const ud = it.userData
  const len = it.runtimeMs ?? primarySource(it)?.durationMs ?? 0
  if (!ud || ud.positionMs <= 0 || len <= ud.positionMs) return null
  return `${Math.max(1, Math.floor((len - ud.positionMs) / 60000) + 1)}m left`
}

export function languageName(code: string): string {
  try {
    const n = new Intl.DisplayNames(['en'], { type: 'language' }).of(code.length === 3 ? iso3to2(code) : code)
    return n && n.toLowerCase() !== code.toLowerCase() ? n : code.toUpperCase()
  } catch {
    return code.toUpperCase()
  }
}

const ISO3: Record<string, string> = { eng: 'en', jpn: 'ja', ger: 'de', deu: 'de', fre: 'fr', fra: 'fr', spa: 'es', ita: 'it', por: 'pt', kor: 'ko', chi: 'zh', zho: 'zh', rus: 'ru', ara: 'ar', hin: 'hi', pol: 'pl', dut: 'nl', nld: 'nl', swe: 'sv', tur: 'tr', tha: 'th', vie: 'vi', ind: 'id', ukr: 'uk', ces: 'cs', cze: 'cs', hun: 'hu', fin: 'fi', dan: 'da', nor: 'no', ell: 'el', gre: 'el', heb: 'he', ron: 'ro', rum: 'ro' }
function iso3to2(code: string): string {
  return ISO3[code.toLowerCase()] ?? code
}

/**
 * "Dub | Sub" from the audio tracks: English plus another language = Dub | Sub, another
 * language only = Sub, English only = Dub (anime). Other titles list the language names.
 */
export function audioBadge(it: Item | null | undefined): string | null {
  if (!it) return null
  const fromServer = (it.audioLanguages ?? []).map((l) => l.toLowerCase())
  const streams = primarySource(it)?.streams ?? []
  const langs = (fromServer.length ? fromServer : streams.filter((s) => s.type === 'audio').map((s) => (s.language ?? '').toLowerCase()).filter(Boolean)).filter((l) => l !== 'und')
  const uniq = Array.from(new Set(langs))
  if (!uniq.length) return null
  const eng = uniq.some((l) => l === 'eng' || l === 'en')
  const other = uniq.some((l) => l !== 'eng' && l !== 'en')
  const anime = it.isAnime || (it.genres ?? []).some((g) => g.toLowerCase() === 'anime')
  if (anime) {
    if (eng && other) return 'Dub | Sub'
    if (other) return 'Sub'
    return 'Dub'
  }
  return uniq.slice(0, 3).map(languageName).join(', ')
}

/** TV and MPAA ratings folded into Crunchyroll's short labels. */
/**
 * The rating as the board issued it (TV-Y7, TV-14, PG-13), not folded into short badges:
 * "ALL" for TV-Y7 claimed a 7+ show was for everyone. Only spelling is tidied.
 */
export function ageLabel(rating: string): string {
  const r = rating.trim().toUpperCase().replace(/^US-/, '')
  if (r === 'PG13') return 'PG-13'
  if (r === 'NC17') return 'NC-17'
  return r.startsWith('TV') || ['G', 'PG', 'PG-13', 'R', 'NC-17', 'NR'].includes(r) ? r : rating.trim()
}

export const yearOf = (it: Item): number | null => it.year ?? (it.premiered ? Number(it.premiered.slice(0, 4)) || null : null)

/** The subtitle track to render: ASS first, the detected language over the tag, English preferred. */
export function pickSubtitle(source: MediaSource | undefined, wantLang = 'eng'): MediaStream | null {
  const subs = (source?.streams ?? []).filter((s) => s.type === 'subtitle' && s.isTextFormat)
  if (!subs.length) return null
  const lang = (s: MediaStream) => (s.detectedLanguage || s.language || '').toLowerCase()
  const score = (s: MediaStream) => {
    let n = 0
    if (s.codec === 'ass' || s.codec === 'ssa') n += 4
    if (lang(s) === wantLang || (wantLang === 'eng' && lang(s) === 'en')) n += 2
    if (s.isDefault) n += 1
    if (s.isForced) n -= 3
    if (/sign|song/i.test(s.title ?? '')) n -= 2
    return n
  }
  return subs.slice().sort((a, b) => score(b) - score(a))[0] ?? null
}

/** Episodes in the order they aired: specials (season 0) last, by episode number. */
export function sortEpisodes(eps: Item[]): Item[] {
  const key = (e: Item) => {
    const s = e.seasonAired ?? 0
    return s === 0 ? Number.MAX_SAFE_INTEGER : s
  }
  return eps.slice().sort((a, b) => key(a) - key(b) || (a.indexAired ?? a.indexAbsolute ?? 0) - (b.indexAired ?? b.indexAbsolute ?? 0))
}

/**
 * The episode a title page's Play means, across every season (upNextOf in the apps): the
 * latest one in progress, else the first unwatched after the last watched, else the first
 * unwatched, else the first (watch again). Specials count only when there is nothing else.
 */
export function nextToPlay(eps: Item[]): Item | null {
  const playable = eps.filter(isPlayable)
  const regular = playable.filter((e) => (e.seasonAired ?? 1) !== 0)
  const ordered = (regular.length ? regular : playable)
    .slice()
    .sort((a, b) => (a.seasonAired ?? 1) - (b.seasonAired ?? 1) || (a.indexAired ?? a.indexAbsolute ?? 0) - (b.indexAired ?? b.indexAbsolute ?? 0))
  const started = ordered.filter((e) => (e.userData?.positionMs ?? 0) > 0)
  if (started.length) return started[started.length - 1]
  let lastWatched = -1
  ordered.forEach((e, i) => {
    if (e.userData?.played) lastWatched = i
  })
  return ordered.slice(lastWatched + 1).find((e) => !e.userData?.played) ?? ordered.find((e) => !e.userData?.played) ?? ordered[0] ?? null
}

export function playLabel(next: Item | null, loading = false): string {
  if (!next) return loading ? 'Play' : 'Nothing to play yet'
  const resumable = (next.userData?.positionMs ?? 0) > 0
  const code = episodeCode(next)
  if (resumable) return isEpisode(next) ? `Resume ${code}` : 'Resume'
  return isEpisode(next) && code ? `Start watching ${code}` : 'Start watching'
}
