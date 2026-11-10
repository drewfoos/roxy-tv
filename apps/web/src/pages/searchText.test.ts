import { describe, expect, it } from 'vitest'
import { matchesSearch, searchFold } from './searchText'

describe('library search text', () => {
  it('finds an accented title from plain letters', () => {
    expect(matchesSearch('pokemon', 'Pok\u00e9mon')).toBe(true)
    expect(matchesSearch('POKEMON the', 'Pok\u00e9mon the Movie')).toBe(true)
  })

  it('finds a plain title from an accented query', () => {
    expect(matchesSearch('Pok\u00e9mon', 'Pokemon Concierge')).toBe(true)
  })

  it('treats composed and decomposed spellings alike', () => {
    expect(searchFold('Pok\u00e9mon')).toBe(searchFold('Poke\u0301mon'))
  })

  it('looks in genres and ignores case', () => {
    expect(matchesSearch('sci-fi', 'Dune', ['Sci-Fi & Fantasy'])).toBe(true)
  })

  it('does not match other titles, and a blank query matches nothing', () => {
    expect(matchesSearch('pokemon', 'Digimon Adventure')).toBe(false)
    expect(matchesSearch('  ', 'Digimon Adventure')).toBe(false)
  })
})
