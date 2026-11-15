// @vitest-environment jsdom
import { act, Suspense } from 'react'
import { createRoot } from 'react-dom/client'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { lazyPage, loadPage, preloadPages } from './lazyPage'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

// A tab left open across a server update asks for a page script of the old build, which
// is gone (404). The page is loaded again once, which brings the new build; never twice
// in a row, and never while the server is away.

const gone = () => Promise.reject(new TypeError('Failed to fetch dynamically imported module'))
const there = async () => true
const away = async () => false
const settled = async <T,>(p: Promise<T>) => {
  let state = 'waiting'
  p.then(
    () => (state = 'loaded'),
    () => (state = 'failed'),
  )
  for (let i = 0; i < 5; i++) await Promise.resolve()
  return state
}

beforeEach(() => sessionStorage.clear())

describe('a page script that cannot be loaded', () => {
  it('loads the page again once, and draws nothing meanwhile', async () => {
    const reload = vi.fn()
    expect(await settled(loadPage(gone, reload, there))).toBe('waiting')
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('does not load the page again a second time in a row', async () => {
    const reload = vi.fn()
    await settled(loadPage(gone, reload, there))
    // The page came back and its script is still not there.
    expect(await settled(loadPage(gone, reload, there))).toBe('failed')
    expect(reload).toHaveBeenCalledTimes(1)
    await expect(loadPage(gone, reload, there)).rejects.toThrow(/could not be loaded/)
  })

  it('does again after a page loaded in between (the next update)', async () => {
    const reload = vi.fn()
    await settled(loadPage(gone, reload, there))
    await expect(loadPage(async () => 'page', reload, there)).resolves.toBe('page')
    await settled(loadPage(gone, reload, there))
    expect(reload).toHaveBeenCalledTimes(2)
  })

  it('leaves the page as it is while the server is away', async () => {
    const reload = vi.fn()
    expect(await settled(loadPage(gone, reload, away))).toBe('failed')
    expect(reload).not.toHaveBeenCalled()
    // And loads it again once the server is back with another build.
    expect(await settled(loadPage(gone, reload, there))).toBe('waiting')
    expect(reload).toHaveBeenCalledTimes(1)
  })
})

describe('a page loaded when first opened', () => {
  it('shows the placeholder, then the page', async () => {
    let arrive: (m: { Page: () => React.JSX.Element }) => void = () => undefined
    const Page = lazyPage(
      () => new Promise<{ Page: () => React.JSX.Element }>((res) => (arrive = res)),
      (m) => m.Page,
    )
    const host = document.createElement('div')
    const root = createRoot(host)
    await act(async () => {
      root.render(
        <Suspense fallback={<div className="spinner" />}>
          <Page />
        </Suspense>,
      )
    })
    expect(host.querySelector('.spinner')).not.toBeNull()
    await act(async () => arrive({ Page: () => <h1>Here</h1> }))
    expect(host.textContent).toBe('Here')
    act(() => root.unmount())
  })

  it('is fetched ahead of time without reloading a page in use when that fails', async () => {
    const reload = vi.fn()
    vi.stubGlobal('location', { ...window.location, reload })
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true })))
    vi.stubGlobal('requestIdleCallback', (f: () => void) => f())
    const load = vi.fn(gone)
    lazyPage(load, () => () => null)
    preloadPages()
    for (let i = 0; i < 10; i++) await Promise.resolve()
    expect(load).toHaveBeenCalled()
    expect(reload).not.toHaveBeenCalled()
    expect(sessionStorage.length).toBe(0)
    vi.unstubAllGlobals()
  })
})
