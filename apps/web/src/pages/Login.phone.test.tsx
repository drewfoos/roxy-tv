// @vitest-environment jsdom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { afterEach, describe, expect, it, vi } from 'vitest'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

// The sign-in code's wait is held open by the server for 25 s at a time. Leaving the
// page left the one in flight open: every visit to "sign in with a phone" added one.

const api = vi.hoisted(() => ({ linkStart: vi.fn(), linkWait: vi.fn(), linkQrUrl: vi.fn(() => '') }))
vi.mock('../api/client', () => ({ api, ApiError: class extends Error {}, NetworkError: class extends Error {} }))

import { PhoneSignIn } from './Login'

afterEach(() => vi.clearAllMocks())

describe('signing in with a phone', () => {
  it('ends its wait for the phone when the page is left', async () => {
    api.linkStart.mockResolvedValue({ code: 'ABCDEF', secret: 's', url: '', expiresIn: 600 })
    api.linkWait.mockImplementation(() => new Promise(() => undefined))
    const host = document.createElement('div')
    const root = createRoot(host)
    await act(async () => { root.render(<PhoneSignIn terms={{ needed: false, version: null } as never} onBack={() => undefined} />) })
    await act(async () => { await Promise.resolve() })
    expect(api.linkWait).toHaveBeenCalled()
    // StrictMode aside, the last wait is the one in flight.
    const left = api.linkWait.mock.calls.at(-1)![2] as AbortSignal
    expect(left.aborted).toBe(false)
    act(() => root.unmount())
    expect(left.aborted).toBe(true)
  })
})
