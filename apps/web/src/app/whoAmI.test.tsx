// @vitest-environment jsdom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const api = vi.hoisted(() => ({ me: vi.fn(async (): Promise<{ id: string; name: string; isAdmin?: boolean }> => ({ id: 'u', name: 'coffee', isAdmin: false })) }))
vi.mock('../api/client', () => ({ api, ApiError: class extends Error {} }))

import { session, type Session } from '../api/session'
import { WHO_GAP_MS, refreshLogin, useLoginRefresh } from './whoAmI'

const S: Session = { token: 't1', userId: 'u', userName: 'coffee', isAdmin: false, profileId: 'p', profileName: 'Coffee', profileHasAvatar: false, profileAvatarKey: '' }
beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  session.set({ ...S })
})
afterEach(() => {
  vi.useRealTimers()
  session.set(null)
})

// "Admin or not" was remembered from sign-in. An admin changing someone's level reached
// their open browser only when they signed out and in again.
describe('the login as the server has it now', () => {
  it('shows the Server pages to someone who was made an admin', async () => {
    api.me.mockResolvedValueOnce({ id: 'u', name: 'coffee', isAdmin: true })
    await refreshLogin()
    expect(session.get()?.isAdmin).toBe(true)
  })

  it('takes them from someone who no longer is one', async () => {
    session.set({ ...S, isAdmin: true })
    api.me.mockResolvedValueOnce({ id: 'u', name: 'coffee' })
    await refreshLogin()
    expect(session.get()?.isAdmin).toBe(false)
  })

  it('follows a changed name, and keeps the profile that is picked', async () => {
    api.me.mockResolvedValueOnce({ id: 'u', name: 'carissa', isAdmin: false })
    await refreshLogin()
    expect(session.get()).toMatchObject({ userName: 'carissa', profileId: 'p', token: 't1' })
  })

  it('writes nothing when nothing changed', async () => {
    const heard = vi.fn()
    const stop = session.subscribe(heard)
    await refreshLogin()
    stop()
    expect(heard).not.toHaveBeenCalled()
  })

  it('does not use an answer that is about another login', async () => {
    let answer = (_: { id: string; name: string; isAdmin?: boolean }) => {}
    api.me.mockImplementationOnce(() => new Promise((r) => (answer = r)))
    const asked = refreshLogin()
    session.set({ ...S, token: 't2', userId: 'u2', userName: 'drew', isAdmin: false }) // someone else signed in meanwhile
    answer({ id: 'u', name: 'coffee', isAdmin: true })
    await asked
    expect(session.get()).toMatchObject({ token: 't2', userName: 'drew', isAdmin: false })
  })

  it('asks nothing while signed out', async () => {
    session.set(null)
    await refreshLogin()
    expect(api.me).not.toHaveBeenCalled()
  })
})

describe('when it is asked', () => {
  function App({ token }: { token: string | undefined }) {
    useLoginRefresh(token)
    return null
  }
  const flush = () => act(async () => {})

  it('when the app opens, and on coming back to the tab, a minute apart at most', async () => {
    vi.useFakeTimers()
    const root = createRoot(document.createElement('div'))
    await act(async () => root.render(<App token="t1" />))
    await flush()
    expect(api.me).toHaveBeenCalledTimes(1)

    window.dispatchEvent(new Event('focus')) // back at once: not again
    await flush()
    expect(api.me).toHaveBeenCalledTimes(1)

    vi.advanceTimersByTime(WHO_GAP_MS + 1)
    window.dispatchEvent(new Event('focus'))
    await flush()
    expect(api.me).toHaveBeenCalledTimes(2)

    act(() => root.unmount())
    vi.advanceTimersByTime(WHO_GAP_MS + 1)
    window.dispatchEvent(new Event('focus'))
    await flush()
    expect(api.me).toHaveBeenCalledTimes(2) // gone from the screen: asks no more
  })

  it('not at all before sign-in', async () => {
    const root = createRoot(document.createElement('div'))
    await act(async () => root.render(<App token={undefined} />))
    await flush()
    expect(api.me).not.toHaveBeenCalled()
    act(() => root.unmount())
  })

  it('goes on quietly when the server does not answer', async () => {
    api.me.mockRejectedValueOnce(new Error('The server did not answer.'))
    const root = createRoot(document.createElement('div'))
    await act(async () => root.render(<App token="t1" />))
    await flush()
    expect(session.get()).toMatchObject({ token: 't1', isAdmin: false })
    act(() => root.unmount())
  })
})
