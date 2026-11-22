// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

vi.mock('../../api/client', () => ({ api: {}, ApiError: class extends Error {} }))

import { livePositionMs } from './nowPlaying'
import { useSinceArrival } from './NowPlaying'

let host: HTMLDivElement
let root: Root
const seen: number[] = []
beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-11-22T12:00:00Z'))
  seen.length = 0
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
  vi.useRealTimers()
})

/** A card's clock: the position the server gave, counted on by the time since it arrived. */
function Clock({ answer }: { answer: { positionMs: number } }) {
  const since = useSinceArrival(answer)
  const shown = livePositionMs({ state: 'playing', positionMs: answer.positionMs, durationMs: 1_440_000, ageMs: 0 }, since)
  seen.push(shown)
  return <span>{shown}</span>
}
const shown = () => Number(host.textContent)

// The page asks again every 5 s. The draw that showed a new answer counted on from the
// PREVIOUS answer's arrival, so each card read 5 s too far until the next one-second tick.
describe('Now Playing: time since the answer arrived', () => {
  it('starts again from nothing with each new answer', () => {
    const first = { positionMs: 600_000 }
    act(() => root.render(<Clock answer={first} />))
    expect(shown()).toBe(600_000)

    // Four seconds on, the same answer drawn again (the one-second tick).
    vi.advanceTimersByTime(4000)
    act(() => root.render(<Clock answer={first} />))
    expect(shown()).toBe(604_000)

    // The next answer, a second later: the player really is at 10:05.
    vi.advanceTimersByTime(1000)
    seen.length = 0
    act(() => root.render(<Clock answer={{ positionMs: 605_000 }} />))
    expect(shown()).toBe(605_000)
    expect(seen.every((v) => v === 605_000), `drawn: ${seen.join(', ')}`).toBe(true) // never 610_000, not even for one draw

    vi.advanceTimersByTime(1000)
    act(() => root.render(<Clock answer={{ positionMs: 605_000 }} />)) // an equal answer, but a new one
    expect(shown()).toBe(605_000)
  })

  it('counts on while the same answer stays', () => {
    const answer = { positionMs: 0 }
    act(() => root.render(<Clock answer={answer} />))
    for (let s = 1; s <= 3; s++) {
      vi.advanceTimersByTime(1000)
      act(() => root.render(<Clock answer={answer} />))
      expect(shown()).toBe(s * 1000)
    }
  })
})
