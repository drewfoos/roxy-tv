import { describe, expect, it } from 'vitest'
import { dragOffset, dropIndex, rowShifts, settleOffset } from './Reorder'
import { barLevels, barSpans } from './Visualizer'

describe('where a carried row lands', () => {
  // Five rows, 50 px each. The row is said by how far it has been carried from its place.
  const boxes = [0, 1, 2, 3, 4].map((i) => ({ top: i * 50, bottom: i * 50 + 50 }))

  it('stays where it is until it covers more than half of a neighbour', () => {
    expect(dropIndex(boxes, 2, 0)).toBe(2)
    expect(dropIndex(boxes, 2, -25)).toBe(2) // half over the row above: not yet
    expect(dropIndex(boxes, 2, 25)).toBe(2) // half over the row below: not yet
    expect(dropIndex(boxes, 2, -26)).toBe(1)
    expect(dropIndex(boxes, 2, 26)).toBe(3)
  })

  it('goes down to the last row it covers more than half of', () => {
    expect(dropIndex(boxes, 1, 30)).toBe(2)
    expect(dropIndex(boxes, 1, 80)).toBe(3)
    expect(dropIndex(boxes, 0, 200)).toBe(4) // carried to the last place: the end
  })

  it('goes up to the first row it covers more than half of', () => {
    expect(dropIndex(boxes, 3, -30)).toBe(2)
    expect(dropIndex(boxes, 3, -90)).toBe(1)
    expect(dropIndex(boxes, 4, -200)).toBe(0) // carried to the first place: the top
  })

  it('is the row itself in a list of one', () => {
    expect(dropIndex([{ top: 0, bottom: 50 }], 0, 0)).toBe(0)
  })
})

describe('the bars of the visualizer', () => {
  // 2048 bins over 0 to 24 kHz: 11.72 Hz a bin.
  const HZ = 24000 / 2048
  const spans = barSpans(56, 2048, 48000)

  it('covers 32 Hz to 16 kHz without a gap, each bar starting where the last one ended', () => {
    expect(spans.length).toBe(56)
    expect(spans[0][0] * HZ).toBeCloseTo(32, 5)
    expect(spans[55][1] * HZ).toBeCloseTo(16000, 5)
    for (let i = 1; i < spans.length; i++) expect(spans[i][0]).toBeCloseTo(spans[i - 1][1], 9)
  })

  it('spaces them by pitch: every bar the same ratio above the one before', () => {
    const ratio = spans[0][1] / spans[0][0]
    for (const [from, to] of spans) expect(to / from).toBeCloseTo(ratio, 9)
    // Half the bars are below about 720 Hz, where most of the music is.
    expect(spans[28][0] * HZ).toBeCloseTo(Math.sqrt(32 * 16000), 3)
  })

  it('stops at half the sample rate when that is lower than 16 kHz', () => {
    const low = barSpans(20, 512, 22050)
    expect(low[19][1]).toBeCloseTo(512, 9)
  })

  it('takes a wide bar\'s level from the loudest of its bins, and from none of its neighbours\'', () => {
    const data = new Uint8Array(2048)
    const [from, to] = spans[50]
    expect(to - from).toBeGreaterThan(20)
    data[Math.floor(from) + 3] = 51
    data[Math.ceil(to) - 2] = 255
    const out = new Float32Array(56)
    barLevels(data, spans, out)
    expect(out[50]).toBe(1)
    expect(out[48]).toBe(0)
    expect(out[52]).toBe(0)
  })

  it('reads a bass bar at its own pitch, between the two bins around it', () => {
    // The first bars are a third of a bin wide. A tone in bin 3 (35 Hz): the bar whose
    // middle is nearest shows it most, the ones further away less, each its own amount.
    expect(spans[0][1] - spans[0][0]).toBeLessThan(1)
    const data = new Uint8Array(2048)
    data[3] = 255
    const out = new Float32Array(56)
    barLevels(data, spans, out)
    const mid = (i: number) => (spans[i][0] + spans[i][1]) / 2
    for (let i = 0; i < 12; i++) expect(out[i]).toBeCloseTo(Math.max(0, 1 - Math.abs(mid(i) - 3)), 5)
    const nearest = out.indexOf(Math.max(...out))
    expect(Math.abs(mid(nearest) - 3)).toBeLessThan(0.25)
    expect(new Set(Array.from(out.slice(0, 6))).size).toBeGreaterThan(3) // not one block
  })

  it('does not read past the last bin', () => {
    const tight = barSpans(4, 8, 32000, 1000, 16000)
    const data = new Uint8Array(8).fill(128)
    const out = new Float32Array(4)
    barLevels(data, tight, out)
    for (const v of out) expect(v).toBeCloseTo(128 / 255, 5)
  })
})

describe('a row carried by its handle', () => {
  const boxes = [0, 1, 2, 3].map((i) => ({ top: i * 50, bottom: i * 50 + 50 }))

  it('goes with the pointer, and no further than the list goes', () => {
    expect(dragOffset(boxes, 1, 30)).toBe(30)
    expect(dragOffset(boxes, 1, -20)).toBe(-20)
    expect(dragOffset(boxes, 1, 5000)).toBe(100) // the last row's place
    expect(dragOffset(boxes, 1, -5000)).toBe(-50) // the first row's
    expect(dragOffset(boxes, 0, -1)).toBe(0)
    expect(dragOffset([{ top: 10, bottom: 60 }], 0, 80)).toBe(0) // alone: nowhere to go
  })

  it('has the rows it passes step aside by its own height, and no others', () => {
    // Carried down from the first place to the third: the two it passed step up.
    expect(rowShifts(4, 0, 2, 110, 50)).toEqual([110, -50, -50, 0])
    // Carried up from the last place to the second: the two it passed step down.
    expect(rowShifts(4, 3, 1, -95, 54)).toEqual([0, 54, 54, -95])
    // Still over its own place: nothing else moves.
    expect(rowShifts(4, 2, 2, 12, 50)).toEqual([0, 0, 12, 0])
  })

  it('settles exactly where the list will have it', () => {
    expect(settleOffset(boxes, 0, 2)).toBe(100)
    expect(settleOffset(boxes, 3, 1)).toBe(-100)
    expect(settleOffset(boxes, 2, 2)).toBe(0)
    // Rows of different heights: a tall row carried down ends with its foot where the last one's was.
    const mixed = [{ top: 0, bottom: 80 }, { top: 80, bottom: 120 }, { top: 120, bottom: 160 }]
    expect(settleOffset(mixed, 0, 2)).toBe(80)
    expect(settleOffset(mixed, 2, 0)).toBe(-120)
  })
})
