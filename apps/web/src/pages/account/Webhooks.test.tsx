// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const api = vi.hoisted(() => ({
  apiKeys: vi.fn(async (): Promise<{ keys: { id: string; name: string }[] }> => ({ keys: [{ id: 'k1', name: 'Sonarr' }] })),
  webhookEvents: vi.fn(async (..._a: unknown[]): Promise<{ events: unknown[] }> => ({ events: [] })),
  createApiKey: vi.fn(async (name: string) => ({ id: 'k2', name, token: 'secret-token' })),
  deleteApiKey: vi.fn(async (..._a: unknown[]) => undefined),
}))
vi.mock('../../api/client', () => ({ api, ApiError: class extends Error {} }))

import { clearAsyncCache } from '../../hooks/useAsync'
import { WebhooksPage } from './Webhooks'

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
  vi.restoreAllMocks()
})
const open = async () => {
  await act(async () => root.render(<WebhooksPage />))
  await act(async () => {})
}
const notices = () => Array.from(host.querySelectorAll('.notice')).map((n) => n.textContent ?? '')

// A list that could not be read was drawn as an empty one: no keys, nothing arrived.
describe('Webhooks: the two lists', () => {
  it('lists the keys and says when nothing has arrived', async () => {
    await open()
    expect(host.textContent).toContain('Sonarr')
    expect(notices()).toEqual(['Nothing has arrived yet.'])
  })

  it('say so when they could not be read, instead of looking empty', async () => {
    api.apiKeys.mockRejectedValueOnce(new Error('The server did not answer.'))
    api.webhookEvents.mockRejectedValueOnce(new Error('The server did not answer.'))
    await open()
    expect(notices()).toEqual(['The server did not answer.', 'The server did not answer.'])
  })
})

// Over plain http the browser has no clipboard API, and the button did nothing.
describe('Webhooks: copying a URL', () => {
  it('copies it and says so, with no clipboard API', async () => {
    Object.defineProperty(navigator, 'clipboard', { value: undefined, configurable: true })
    let copied = ''
    ;(document as { execCommand?: unknown }).execCommand = vi.fn(() => {
      const box = document.activeElement as HTMLTextAreaElement | null
      if (box?.tagName === 'TEXTAREA') copied = box.value
      return true
    })
    await open()
    const copy = host.querySelector('.copyrow .icon-btn') as HTMLButtonElement
    await act(async () => copy.click())
    await act(async () => {})
    expect(copied).toBe(`${window.location.origin}/v1/webhooks/sonarr`)
    expect(copy.textContent).toBe('Copied')
    delete (document as { execCommand?: unknown }).execCommand
  })
})
