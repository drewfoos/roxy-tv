import { describe, expect, it } from 'vitest'
import { ageLabel, audioBadge, episodeCode, languageName, minutesLabel, nextToPlay, pickSignsSubtitle, pickSubtitle, playLabel, progressFraction, runtimeLabel, seriesPlayLabel, sortEpisodes, timeLeftLabel } from './items'
import type { Item, MediaSource, MediaStream } from './types'

const item = (p: Partial<Item>) => ({ id: 'x', type: 'episode', title: 'T', ...p }) as Item
const ep = (season: number, n: number, ud?: Partial<NonNullable<Item['userData']>>) =>
  item({ id: `s${season}e${n}`, seasonAired: season, indexAired: n, userData: ud ? ({ positionMs: 0, played: false, ...ud } as Item['userData']) : undefined })

describe('episodes', () => {
  it('codes episodes', () => {
    expect(episodeCode(ep(1, 3))).toBe('S1 E3')
    expect(episodeCode(item({ seasonAired: 2, indexAbsolute: 27 }))).toBe('S2 E27')
    expect(episodeCode(item({ seasonAired: 1 }))).toBe('S1')
    expect(episodeCode(item({ type: 'movie' }))).toBe('')
  })
  it('sorts by season, specials last', () => {
    expect(sortEpisodes([ep(0, 1), ep(2, 1), ep(1, 2), ep(1, 1)]).map((e) => e.id)).toEqual(['s1e1', 's1e2', 's2e1', 's0e1'])
  })
})

describe('nextToPlay', () => {
  it('resumes the latest episode in progress', () => {
    expect(nextToPlay([ep(1, 1, { played: true }), ep(1, 2, { positionMs: 5000 }), ep(1, 3)])?.id).toBe('s1e2')
    // Two in progress: the later one (the earlier was left part-way and moved on from).
    expect(nextToPlay([ep(2, 1, { positionMs: 9000 }), ep(1, 4, { positionMs: 5000 }), ep(1, 5)])?.id).toBe('s2e1')
  })
  it('goes on after the last watched one', () => {
    expect(nextToPlay([ep(1, 1, { played: true }), ep(1, 2), ep(1, 3, { played: true }), ep(1, 4)])?.id).toBe('s1e4')
  })
  it('starts over when everything is watched', () => {
    expect(nextToPlay([ep(1, 1, { played: true }), ep(1, 2, { played: true })])?.id).toBe('s1e1')
  })
  it('leaves specials until nothing else is left, and skips what cannot play', () => {
    expect(nextToPlay([ep(0, 1), ep(1, 1)])?.id).toBe('s1e1')
    expect(nextToPlay([ep(0, 1)])?.id).toBe('s0e1')
    expect(nextToPlay([item({ ...ep(1, 1), availability: 'missing' }), ep(1, 2)])?.id).toBe('s1e2')
    expect(nextToPlay([])).toBeNull()
  })
  it('labels the button', () => {
    expect(playLabel(ep(1, 2, { positionMs: 1 }))).toBe('Resume S1 E2')
    expect(playLabel(ep(1, 2))).toBe('Start watching S1 E2')
    expect(playLabel(item({ type: 'movie' }))).toBe('Start watching')
    expect(playLabel(null)).toBe('Nothing to play yet')
  })
  it('has no words for a series until what plays next is read', () => {
    expect(seriesPlayLabel(null, true)).toBeNull() // not "Play", which then changed
    expect(seriesPlayLabel(null, false)).toBe('Nothing to play yet')
    expect(seriesPlayLabel(ep(1, 2), true)).toBe('Start watching S1 E2') // known from an earlier visit
    expect(seriesPlayLabel(ep(1, 2, { positionMs: 1 }), false)).toBe('Resume S1 E2')
  })
})

describe('progress and runtime', () => {
  it('shows progress for a position, full for watched', () => {
    expect(progressFraction(item({ runtimeMs: 1000, userData: { positionMs: 250, played: false } as Item['userData'] }))).toBe(0.25)
    expect(progressFraction(item({ runtimeMs: 1000, userData: { positionMs: 0, played: true } as Item['userData'] }))).toBe(1)
    expect(progressFraction(item({ runtimeMs: 1000 }))).toBeNull()
  })
  it('says what is left and how long', () => {
    expect(timeLeftLabel(item({ runtimeMs: 24 * 60000, userData: { positionMs: 12 * 60000, played: false } as Item['userData'] }))).toBe('13m left')
    expect(runtimeLabel(item({ runtimeMs: 102 * 60000 }))).toBe('1h 42m')
    expect(runtimeLabel(item({ runtimeMs: 24 * 60000 }))).toBe('24m')
    expect(runtimeLabel(item({}))).toBeNull()
  })
})

describe('labels', () => {
  it('names languages from two- and three-letter codes', () => {
    expect(languageName('jpn')).toBe('Japanese')
    expect(languageName('ger')).toBe('German')
    expect(languageName('en')).toBe('English')
    expect(languageName('qqq')).toBe('QQQ')
  })
  it('keeps ratings as issued', () => {
    expect(ageLabel('TV-Y7')).toBe('TV-Y7') // not folded into "ALL"
    expect(ageLabel('us-pg13')).toBe('PG-13')
  })
  it('badges anime audio as Dub / Sub', () => {
    const anime = (langs: string[]) => item({ type: 'series', isAnime: true, audioLanguages: langs })
    expect(audioBadge(anime(['jpn', 'eng']))).toBe('Dub | Sub')
    expect(audioBadge(anime(['jpn']))).toBe('Sub')
    expect(audioBadge(anime(['eng']))).toBe('Dub')
    expect(audioBadge(item({ type: 'movie', audioLanguages: ['eng', 'fre'] }))).toBe('English, French')
  })
})

describe('pickSubtitle', () => {
  const sub = (p: Partial<MediaStream>) => ({ index: 1, type: 'subtitle', codec: 'subrip', isTextFormat: true, ...p }) as MediaStream
  const src = (streams: MediaStream[]) => ({ id: 's', streams }) as MediaSource
  it('prefers styled English dialogue over forced signs', () => {
    const got = pickSubtitle(src([sub({ index: 1, language: 'eng', isForced: true, codec: 'ass', title: 'Signs' }), sub({ index: 2, language: 'eng', codec: 'ass' }), sub({ index: 3, language: 'eng' })]))
    expect(got?.index).toBe(2)
  })
  it('trusts the detected language over the tag, and ignores image tracks', () => {
    const got = pickSubtitle(src([sub({ index: 1, language: 'eng', detectedLanguage: 'fre' }), sub({ index: 2, language: 'und', detectedLanguage: 'eng' }), sub({ index: 3, codec: 'hdmv_pgs_subtitle', isTextFormat: false, language: 'eng' })]))
    expect(got?.index).toBe(2)
    expect(pickSubtitle(src([sub({ isTextFormat: false })]))).toBeNull()
  })
  // Files tag German, French, Dutch and Chinese with the older codes (ger, fre, dut, chi) or
  // two letters; Preferences offers deu, fra, nld, zho. Compared as written, the wanted
  // language never matched and the first track (English, usually) was shown.
  it('matches a language whichever of its codes the file and the preference use', () => {
    const file = src([sub({ index: 1, language: 'eng' }), sub({ index: 2, language: 'ger' }), sub({ index: 3, language: 'fre' }), sub({ index: 4, language: 'dut' }), sub({ index: 5, language: 'chi' }), sub({ index: 6, language: 'pt-BR' })])
    expect(pickSubtitle(file, 'deu')?.index).toBe(2)
    expect(pickSubtitle(file, 'fra')?.index).toBe(3)
    expect(pickSubtitle(file, 'nld')?.index).toBe(4)
    expect(pickSubtitle(file, 'zho')?.index).toBe(5)
    expect(pickSubtitle(file, 'por')?.index).toBe(6)
    expect(pickSubtitle(file, 'eng')?.index).toBe(1)
    // The detected language still wins over the tag, in either spelling.
    expect(pickSubtitle(src([sub({ index: 1, language: 'eng' }), sub({ index: 2, language: 'und', detectedLanguage: 'fr' })]), 'fra')?.index).toBe(2)
    // No language on the track and none wanted is not a match.
    expect(pickSubtitle(src([sub({ index: 1, language: 'und' }), sub({ index: 2, language: 'eng', isDefault: true })]), '')?.index).toBe(2)
  })
})

describe('pickSignsSubtitle', () => {
  const sub = (p: Partial<MediaStream>) => ({ index: 1, type: 'subtitle', codec: 'subrip', isTextFormat: true, ...p }) as MediaStream
  const src = (streams: MediaStream[]) => ({ id: 's', streams }) as MediaSource
  it('finds the signs track by its flag or by its name, in the wanted language', () => {
    expect(pickSignsSubtitle(src([sub({ index: 1, language: 'eng', title: 'Full' }), sub({ index: 2, language: 'eng', isForced: true })]), 'eng')?.index).toBe(2)
    for (const title of ['Signs & Songs', 'Signs/Songs', 'English [ForcedNarrative]', '[FOR DUB]', 'Dubtitles', 'S&S'])
      expect(pickSignsSubtitle(src([sub({ index: 1, language: 'eng' }), sub({ index: 2, language: 'eng', title })]), 'eng')?.index, title).toBe(2)
    // Another language's signs track is no use; "ger" is the "deu" the preference names.
    expect(pickSignsSubtitle(src([sub({ index: 1, language: 'spa', isForced: true })]), 'eng')).toBeNull()
    expect(pickSignsSubtitle(src([sub({ index: 1, language: 'ger', title: 'Signs' })]), 'deu')?.index).toBe(1)
  })
  it('is null when there is none: full dialogue is not a stand-in, nor a picture track', () => {
    expect(pickSignsSubtitle(src([sub({ index: 1, language: 'eng', title: 'Full Subtitles' }), sub({ index: 2, language: 'eng' })]), 'eng')).toBeNull()
    expect(pickSignsSubtitle(src([sub({ index: 1, language: 'eng', isForced: true, codec: 'hdmv_pgs_subtitle', isTextFormat: false })]), 'eng')).toBeNull()
    expect(pickSignsSubtitle(undefined, 'eng')).toBeNull()
  })
  it('takes an untagged signs track only when no track has the language, and prefers styled', () => {
    expect(pickSignsSubtitle(src([sub({ index: 1, title: 'Signs' }), sub({ index: 2, language: 'eng', title: 'Dialogue' })]), 'eng')).toBeNull()
    expect(pickSignsSubtitle(src([sub({ index: 1, title: 'Signs' }), sub({ index: 2, language: 'jpn' })]), 'eng')?.index).toBe(1)
    expect(pickSignsSubtitle(src([sub({ index: 1, language: 'eng', title: 'Signs' }), sub({ index: 2, language: 'eng', title: 'Signs', codec: 'ass' })]), 'eng')?.index).toBe(2)
  })
})

// Continue Watching's tag: the time left once started, the length before (it was blank).
describe('minutesLabel', () => {
  const ep = (positionMs: number, runtimeMs?: number) => ({ id: 'e', type: 'episode', title: 'E', runtimeMs, userData: { positionMs, played: false, playCount: 0, isFavorite: false } }) as unknown as Item
  it('says the length of an episode not started', () => expect(minutesLabel(ep(0, 1_440_000))).toBe('24m'))
  it('says what is left part-way', () => expect(minutesLabel(ep(60_000, 1_440_000))).toBe('23m left'))
  it('says nothing without a length', () => expect(minutesLabel(ep(0))).toBeNull())
})

