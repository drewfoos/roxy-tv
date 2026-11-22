// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const api = vi.hoisted(() => ({
  devices: vi.fn(async (): Promise<{ devices: unknown[] }> => ({ devices: [] })),
  signOutDevice: vi.fn(async (..._args: unknown[]) => undefined),
}))
vi.mock('../../api/client', () => ({ api, ApiError: class extends Error {} }))

import { clearAsyncCache } from '../../hooks/useAsync'
import { DevicesPage } from './Devices'

let host: HTMLDivElement
let root: Root
beforeEach(() => {
  vi.clearAllMocks()
  clearAsyncCache()
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
})
const open = async () => {
  await act(async () => root.render(<DevicesPage />))
  await act(async () => {})
}
const notices = () => Array.from(host.querySelectorAll('.notice')).map((n) => n.textContent)

// "No devices are signed in." beside a load error: the list was never read (and this very
// browser is one of them).
describe('Device Management', () => {
  it('says no device is signed in only when the server said so', async () => {
    await open()
    expect(notices()).toEqual(['No devices are signed in.'])
  })

  it('says only what went wrong when the list could not be read', async () => {
    api.devices.mockRejectedValueOnce(new Error('The server did not answer.'))
    await open()
    expect(notices()).toEqual(['The server did not answer.'])
  })
})
