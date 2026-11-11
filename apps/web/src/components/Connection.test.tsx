// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '../api/client'
import { connection } from '../api/connection'
import { session } from '../api/session'
import { clearAsyncCache, useAsync } from '../hooks/useAsync'
import { ConnectionStrip, LoadError } from './Connection'

// React's act() needs to be told this is a test environment.
;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const S = { token: 't', userId: 'u', userName: 'drew', isAdmin: false, profileId: 'p', profileName: 'Drew', profileHasAvatar: false, profileAvatarKey: '' }
const ok = (body: unknown) => ({ ok: true, status: 200, text: async () => JSON.stringify(body), json: async () => body })

/** A page the way the real ones are written: its data, or the load error in its place. */
function Page() {
  const { data, error, reload } = useAsync('item:x', () => api.item('x'))
  return (
    <>
      {data ? <h1>{data.title}</h1> : error ? <LoadError error={error} onRetry={() => reload()} /> : <i>loading</i>}
      <ConnectionStrip />
    </>
  )
}

let host: HTMLDivElement
let root: Root
let up = false
const flush = () => act(async () => { await vi.advanceTimersByTimeAsync(0) })

beforeEach(() => {
  vi.useFakeTimers()
  localStorage.clear()
  session.set(S)
  clearAsyncCache()
  connection.reset()
  up = false
  vi.stubGlobal('fetch', vi.fn((url: string) => (up ? Promise.resolve(ok(url === '/healthz' ? {} : { id: 'x', title: 'Frieren' })) : Promise.reject(new TypeError('Failed to fetch')))))
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
  connection.reset()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  session.set(null)
})

describe('a page opened while the server cannot be reached', () => {
  it('says so calmly, with a strip and a Try again, and no browser error text', async () => {
    act(() => root.render(<Page />))
    await flush()
    expect(host.querySelector('.offline h2')?.textContent).toBe("Can't reach your Roxy server")
    expect(host.querySelector('.offline button')?.textContent).toBe('Try again')
    expect(host.querySelector('.conn')?.textContent).toContain("Can't reach your Roxy server")
    expect(host.textContent).not.toContain('Failed to fetch')
    expect(host.textContent).not.toMatch(/ownload/)
  })

  it('loads by itself when the server answers again, and says "Back online" for a moment', async () => {
    act(() => root.render(<Page />))
    await flush()
    expect(host.querySelector('.offline')).not.toBeNull()

    up = true
    await act(async () => { await vi.advanceTimersByTimeAsync(2000) }) // the first quiet check
    await flush()
    expect(host.querySelector('h1')?.textContent).toBe('Frieren')
    expect(host.querySelector('.offline')).toBeNull()
    expect(host.querySelector('.conn--back')?.textContent).toBe('Back online')

    await act(async () => { await vi.advanceTimersByTimeAsync(2600) })
    expect(host.querySelector('.conn')).toBeNull()
  })

  it('Try again checks now and loads the page', async () => {
    act(() => root.render(<Page />))
    await flush()
    up = true
    await act(async () => {
      host.querySelector<HTMLButtonElement>('.offline button')!.click()
      await vi.advanceTimersByTimeAsync(0)
    })
    await flush()
    expect(host.querySelector('h1')?.textContent).toBe('Frieren')
  })

  it('keeps a real error the server gave as it is', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: false, status: 404, json: async () => ({ error: 'no such item' }) })))
    act(() => root.render(<Page />))
    await flush()
    expect(host.querySelector('.offline p')?.textContent).toBe('no such item')
    expect(host.querySelector('.conn')).toBeNull()
  })
})
