/**
 * Text as the library search compares it: lower case with the accents off, so "pokemon"
 * finds "Pokémon". Full-width letters fold to plain ones too (NFKD).
 */
export function searchFold(s: string): string {
  return s.normalize('NFKD').replace(/\p{M}+/gu, '').toLowerCase()
}

/** True when a title or one of its genres contains the query. A blank query matches nothing. */
export function matchesSearch(query: string, title: string, genres: string[] = []): boolean {
  const q = searchFold(query.trim())
  if (!q) return false
  return searchFold(title).includes(q) || genres.some((g) => searchFold(g).includes(q))
}
