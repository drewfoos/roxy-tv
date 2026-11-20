import type { CastGroup, CastMember, ItemDetails } from '../api/types'

/** What a cast row shows: the casts to switch between (none when there is one), the one showing and its people. */
export interface ShownCast {
  groups: CastGroup[]
  language: string
  members: CastMember[]
}

/**
 * The server says which casts a title has and which to show first (the dub's for someone
 * who watches the dub and has it); `picked` is the viewer's switch on this page. Without
 * casts from the server it is the TMDB cast, as before.
 */
export function shownCast(details: Pick<ItemDetails, 'tmdb' | 'casts' | 'castLanguage'> | null | undefined, picked?: string | null): ShownCast {
  const groups = (details?.casts ?? []).filter((g) => g.members?.length > 0)
  if (groups.length < 2) return { groups: [], language: '', members: details?.tmdb?.cast ?? [] }
  const g = groups.find((x) => x.language === picked) ?? groups.find((x) => x.language === details?.castLanguage) ?? groups[0]
  return { groups, language: g.language, members: g.members }
}
