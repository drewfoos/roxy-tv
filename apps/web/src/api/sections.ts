// A series' season list in release order (Crunchyroll's): its seasons, the specials that
// came out between them ("OVAs"), and the franchise's films the library holds, from
// GET /v1/items/{id}/sections. Kept apart from the page so it is tested (sections.test.ts).
import type { Item, Section } from './types'

/** One entry of the season list, and where its rows come from. */
export interface SeasonEntry {
  key: string
  title: string
  /** A season: all its episodes. Specials: those of this season listed in `ids`. */
  seasonId?: string
  ids?: string[]
  /** A film: its one row. */
  movie?: Item
  /** Episodes, when the server said ("2 Episodes"). */
  count?: number
  /** Said instead of a count ("Film · 2014"). */
  label?: string
}

/** The plain list: the library's seasons by number, Specials last. */
export function plainEntries(seasons: Item[]): SeasonEntry[] {
  const key = (s: Item) => {
    const n = s.indexAired ?? Number.MAX_SAFE_INTEGER
    return n === 0 ? Number.MAX_SAFE_INTEGER - 1 : n
  }
  return seasons
    .slice()
    .sort((a, b) => key(a) - key(b))
    .map((s, i) => ({ key: s.id, title: s.title || `Season ${s.indexAired ?? i + 1}`, seasonId: s.id }))
}

/**
 * The entries from the server's sections, or null when they cannot be used (none; a
 * season it names is not among `seasons`): the plain list then.
 */
export function sectionEntries(sections: Section[] | null | undefined, seasons: Item[]): SeasonEntry[] | null {
  if (!sections?.length) return null
  const byId = new Map(seasons.map((s) => [s.id, s]))
  const out: SeasonEntry[] = []
  for (const [i, sec] of sections.entries()) {
    if (sec.kind === 'season') {
      const s = sec.seasonId ? byId.get(sec.seasonId) : undefined
      if (!s) return null
      out.push({ key: s.id, title: sec.title || s.title || `Season ${s.indexAired ?? i + 1}`, seasonId: s.id, count: sec.episodes })
    } else if (sec.kind === 'specials') {
      if (!sec.seasonId) return null
      out.push({ key: `section:${i}`, title: sec.title, seasonId: sec.seasonId, ids: sec.episodeIds ?? [], count: sec.episodes })
    } else if (sec.kind === 'movie' && sec.item) {
      const year = sec.year ?? sec.item.year
      out.push({ key: `section:${i}`, title: sec.title || sec.item.title, movie: sec.item, label: year ? `Film · ${year}` : 'Film' })
    }
  }
  return out
}

/** An entry's rows from its season's episodes: a group of specials keeps its own, in its order. */
export function entryRows(entry: SeasonEntry, episodes: Item[]): Item[] {
  if (!entry.ids) return episodes
  const byId = new Map(episodes.map((e) => [e.id, e]))
  return entry.ids.map((id) => byId.get(id)).filter((e): e is Item => !!e)
}

/**
 * The number a row shows: in a group of specials its place in the group (1, 2, 3: the
 * TheTVDB numbers files carry, KonoSuba's 3 to 7, mean nothing on screen), none on a
 * film's row, the episode's own number in a season.
 */
export function rowNumber(entry: SeasonEntry | undefined, rows: Item[], ep: Item): number | null {
  if (entry?.movie) return null
  if (entry?.ids) {
    const i = rows.findIndex((r) => r.id === ep.id)
    return i >= 0 ? i + 1 : null
  }
  return ep.indexAired ?? ep.indexAbsolute ?? null
}
