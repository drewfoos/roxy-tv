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

// Every reload() a caller holds must end, whatever order the answers come in: a button
// that waits on it would otherwise stay busy for good.
describe('overlapping reloads', () => {
  type Answer = { ok: (v: string) => void; fail: (e: Error) => void }
  function mount(fn?: () => Promise<string>) {
    const answers: Answer[] = []
    const page = { reload: (async () => undefined) as (silent?: boolean) => Promise<void>, data: null as string | null, error: null as string | null, loading: false }
    const ask = fn ?? (() => new Promise<string>((ok, fail) => answers.push({ ok, fail })))
    function Page() {
      const s = useAsync('k', ask)
      page.reload = s.reload
      page.data = s.data
      page.error = s.error
      page.loading = s.loading
      return null
    }
    const root = createRoot(document.createElement('div'))
    return { answers, page, root, Page }
  }
  /** Three reloads on top of each other; returns which of them have ended. */
  function three(page: { reload: (silent?: boolean) => Promise<void> }) {
    const ended = [false, false, false]
    act(() => {
      for (let i = 0; i < 3; i++) void page.reload(true).then(() => { ended[i] = true })
    })
    return ended
  }

  it('three, answered oldest first: all end with the newest, which is shown', async () => {
    const { answers, page, root, Page } = mount()
    await act(async () => root.render(<Page />))
    await act(async () => { answers[0].ok('first'); await tick() })
    const ended = three(page)
    await act(async () => { answers[1].ok('a'); await tick() })
    await act(async () => { answers[2].ok('b'); await tick() })
    expect(ended).toEqual([false, false, false])
    expect(page.data).toBe('first')
    await act(async () => { answers[3].ok('c'); await tick() })
    expect(ended).toEqual([true, true, true])
    expect(page.data).toBe('c')
    act(() => root.unmount())
  })

  it('three, answered newest first: each older one ends as its own answer comes', async () => {
    const { answers, page, root, Page } = mount()
    await act(async () => root.render(<Page />))
    await act(async () => { answers[0].ok('first'); await tick() })
    const ended = three(page)
    await act(async () => { answers[3].ok('c'); await tick() })
    expect(ended).toEqual([false, false, true])
    await act(async () => { answers[1].ok('a'); await tick() })
    await act(async () => { answers[2].ok('b'); await tick() })
    expect(ended).toEqual([true, true, true])
    expect(page.data).toBe('c')
    act(() => root.unmount())
  })

  it('older ones fail, the newest answers: all end, no error is shown', async () => {
    const { answers, page, root, Page } = mount()
    await act(async () => root.render(<Page />))
    await act(async () => { answers[0].ok('first'); await tick() })
    const ended = three(page)
    await act(async () => { answers[1].fail(new Error('gone')); await tick() })
    await act(async () => { answers[2].fail(new Error('gone too')); await tick() })
    expect(ended).toEqual([false, false, false])
    expect(page.error).toBeNull()
    await act(async () => { answers[3].ok('c'); await tick() })
    expect(ended).toEqual([true, true, true])
    expect(page.data).toBe('c')
    expect(page.error).toBeNull()
    act(() => root.unmount())
  })

  it('the newest fails after older ones answered: all end, the error is shown over the old data', async () => {
    const { answers, page, root, Page } = mount()
    await act(async () => root.render(<Page />))
    await act(async () => { answers[0].ok('first'); await tick() })
    const ended = three(page)
    await act(async () => { answers[1].ok('a'); answers[2].ok('b'); await tick() })
    await act(async () => { answers[3].fail(new Error('no answer')); await tick() })
    expect(ended).toEqual([true, true, true])
    expect(page.error).toBe('no answer')
    expect(page.data).toBe('first')
    act(() => root.unmount())
  })

  it('an error, then a reload that answers: both end and the error is gone', async () => {
    const { answers, page, root, Page } = mount()
    await act(async () => root.render(<Page />))
    await act(async () => { answers[0].fail(new Error('down')); await tick() })
    expect(page.error).toBe('down')
    const ended = [false, false]
    act(() => {
      void page.reload(true).then(() => { ended[0] = true })
      void page.reload(true).then(() => { ended[1] = true })
    })
    await act(async () => { answers[1].fail(new Error('still down')); await tick() })
    expect(ended).toEqual([false, false])
    expect(page.error).toBe('down')
    await act(async () => { answers[2].ok('back'); await tick() })
    expect(ended).toEqual([true, true])
    expect(page.data).toBe('back')
    expect(page.error).toBeNull()
    act(() => root.unmount())
  })

  it('the page goes away with three out: all end when their answers come', async () => {
    const { answers, page, root, Page } = mount()
    await act(async () => root.render(<Page />))
    await act(async () => { answers[0].ok('first'); await tick() })
    const ended = three(page)
    act(() => root.unmount())
    await act(async () => { answers[2].ok('b'); await tick() })
    expect(ended).toEqual([false, false, false]) // waits for the newest, as on screen
    await act(async () => { answers[3].fail(new Error('late')); await tick() })
    await act(async () => { answers[1].ok('a'); await tick() })
    expect(ended).toEqual([true, true, true])
  })

  it('a load that throws before it starts counts as a failed one', async () => {
    let boom = false
    const answers: Answer[] = []
    const { page, root, Page } = mount(() => {
      if (boom) throw new Error('could not start')
      return new Promise<string>((ok, fail) => answers.push({ ok, fail }))
    })
    await act(async () => root.render(<Page />))
    await act(async () => { answers[0].ok('first'); await tick() })
    const ended = [false, false]
    let thrown: unknown = null
    act(() => {
      void page.reload(true).then(() => { ended[0] = true })
      boom = true
      try {
        void page.reload(true).then(() => { ended[1] = true })
      } catch (e) {
        thrown = e
      }
    })
    await act(async () => { answers[1].ok('a'); await tick() })
    expect(thrown).toBeNull()
    expect(ended).toEqual([true, true])
    expect(page.error).toBe('could not start')
    expect(page.loading).toBe(false)
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
