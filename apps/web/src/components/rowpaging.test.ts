import { describe, expect, it } from 'vitest'
import { pageTarget, showWhole } from './Cards'

// A row of 20 cards 200 wide with 20 between them, 64 px gutters, an 1100 px wide track
// (4 whole cards and a peek): card i starts at 64 + 220 i in the track.
const pad = 64
const cards = Array.from({ length: 20 }, (_, i) => ({ left: pad + 220 * i, right: pad + 220 * i + 200 }))
const view = 1100
const max = cards[19].right + pad - view
const start = (i: number) => cards[i].left - pad
const shown = (from: number) => cards.map((c, i) => (c.left >= from + pad - 1 && c.right <= from + view - pad + 1 ? i : -1)).filter((i) => i >= 0)

describe('pageTarget', () => {
  it('next page starts at the first card not fully shown', () => {
    expect(shown(0)).toEqual([0, 1, 2, 3])
    expect(pageTarget(cards, 0, view, pad, pad, max, 1)).toBe(start(4))
  })
  it('previous page ends with the card before the first shown', () => {
    const back = pageTarget(cards, start(8), view, pad, pad, max, -1)
    expect(shown(back).at(-1)).toBe(7)
  })
  it('pages through and back with no card skipped or shown twice', () => {
    let at = 0
    const seen: number[] = []
    for (let i = 0; i < 10 && at < max; i++) {
      seen.push(...shown(at))
      at = pageTarget(cards, at, view, pad, pad, max, 1)
    }
    seen.push(...shown(at))
    expect(new Set(seen).size).toBe(20)
    while (at > 0) {
      const first = shown(at)[0]
      const back = pageTarget(cards, at, view, pad, pad, max, -1)
      if (back > 0) expect(shown(back).at(-1)).toBe(first - 1)
      at = back
    }
  })
  it('stops at the ends', () => {
    expect(pageTarget(cards, max, view, pad, pad, max, 1)).toBe(max)
    expect(pageTarget(cards, start(2), view, pad, pad, max, -1)).toBe(0)
  })
  it('pages on from where an earlier click is heading', () => {
    // A second quick click, measured from the first's target, moves one more page.
    const first = pageTarget(cards, 0, view, pad, pad, max, 1)
    expect(pageTarget(cards, first, view, pad, pad, max, 1)).toBe(start(8))
  })
})

describe('showWhole', () => {
  it('leaves a card that is already whole', () => {
    expect(showWhole(cards, 2, 0, view, pad, pad, max)).toBe(0)
  })
  it('brings a card cut off on the right in at the right edge, moving the least', () => {
    const at = showWhole(cards, 4, 0, view, pad, pad, max)
    expect(shown(at)).toContain(4)
    expect(shown(at).at(-1)).toBe(4)
  })
  it('brings a card cut off on the left in at the left edge', () => {
    expect(showWhole(cards, 3, start(4) + 10, view, pad, pad, max)).toBe(start(3))
  })
})
