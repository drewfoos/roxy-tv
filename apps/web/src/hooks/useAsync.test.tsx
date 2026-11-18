// @vitest-environment jsdom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clearAsyncCache, useAsync, usePoll } from './useAsync'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const tick = async () => {
  for (let i = 0; i < 10; i++) await Promise.resolve()
}

beforeEach(() => clearAsyncCache())
afterEach(() => vi.useRealTimers())

describe('reload()', () => {
  it('overtaken by a newer one, ends when the newer one has answered', async () => {
    const answers: ((v: string) => void)[] = []
    let reload: (silent?: boolean) => Promise<void> = async () => undefined
    let shown: string | null = null
    function Page() {
      const s = useAsync('k', () => new Promise<string>((res) => answers.push(res)))
      reload = s.reload
      shown = s.data
      return null
    }
    const root = createRoot(document.createElement('div'))
    await act(async () => root.render(<Page />))
    await act(async () => { answers[0]('first'); await tick() })

    let done = false
    let first: Promise<void> = Promise.resolve()
    act(() => {
      first = reload(true).then(() => { done = true })
      void reload(true) // a poll lands on top of it
    })
    await act(async () => { answers[1]('old'); await tick() })
    // The first reload's answer is dropped: the one that will be shown is still out.
    expect(done).toBe(false)
    await act(async () => { answers[2]('new'); await tick() })
    await first
    expect(done).toBe(true)
    expect(shown).toBe('new')
    act(() => root.unmount())
  })
})

describe('usePoll', () => {
  it('skips a tick while the one before is unanswered', async () => {
    vi.useFakeTimers()
    const answers: (() => void)[] = []
    const reload = vi.fn(() => new Promise<void>((res) => answers.push(res)))
    function Page() {
      usePoll(reload, 5000)
      return null
    }
    const root = createRoot(document.createElement('div'))
    await act(async () => root.render(<Page />))
    await act(async () => { await vi.advanceTimersByTimeAsync(16000) })
    expect(reload).toHaveBeenCalledTimes(1)
    await act(async () => { answers[0](); await vi.advanceTimersByTimeAsync(5000) })
    expect(reload).toHaveBeenCalledTimes(2)
    act(() => root.unmount())
  })
})
