import { describe, expect, it } from 'vitest'
import { dropIndex } from './Reorder'
import { barLevels, barSpans } from './Visualizer'

describe('where a dragged row lands', () => {
  // Five rows, 50 px each.
  const boxes = [0, 1, 2, 3, 4].map((i) => ({ top: i * 50, bottom: i * 50 + 50 }))

  it('stays where it is until the pointer passes the middle of a neighbour', () => {
    expect(dropIndex(boxes, 2, 125)).toBe(2)
    expect(dropIndex(boxes, 2, 80)).toBe(2) // into the row above, not past its middle
    expect(dropIndex(boxes, 2, 170)).toBe(2) // into the row below, not past its middle
  })

  it('goes down to the last row whose middle the pointer has passed', () => {
    expect(dropIndex(boxes, 1, 130)).toBe(2)
    expect(dropIndex(boxes, 1, 180)).toBe(3)
    expect(dropIndex(boxes, 0, 9999)).toBe(4) // below the list: the end
  })

  it('goes up to the first row whose middle the pointer is above', () => {
    expect(dropIndex(boxes, 3, 120)).toBe(2)
    expect(dropIndex(boxes, 3, 60)).toBe(1)
    expect(dropIndex(boxes, 4, -50)).toBe(0) // above the list: the top
  })

  it('is the row itself in a list of one', () => {
    expect(dropIndex([{ top: 0, bottom: 50 }], 0, 400)).toBe(0)
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
