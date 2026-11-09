import { describe, expect, it } from 'vitest'
import { ageLabel, audioBadge, episodeCode, languageName, minutesLabel, nextToPlay, pickSubtitle, playLabel, progressFraction, runtimeLabel, sortEpisodes, timeLeftLabel } from './items'
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
})

// Continue Watching's tag: the time left once started, the length before (it was blank).
describe('minutesLabel', () => {
  const ep = (positionMs: number, runtimeMs?: number) => ({ id: 'e', type: 'episode', title: 'E', runtimeMs, userData: { positionMs, played: false, playCount: 0, isFavorite: false } }) as unknown as Item
  it('says the length of an episode not started', () => expect(minutesLabel(ep(0, 1_440_000))).toBe('24m'))
  it('says what is left part-way', () => expect(minutesLabel(ep(60_000, 1_440_000))).toBe('23m left'))
  it('says nothing without a length', () => expect(minutesLabel(ep(0))).toBeNull())
})

