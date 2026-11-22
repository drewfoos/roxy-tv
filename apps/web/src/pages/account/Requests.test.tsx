// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const waiting = (id: string, title: string) => ({ id, type: 'tv', tmdbId: 1, title, year: 2020, status: 'approval', userName: 'coffee', createdAt: '2026-11-20T10:00:00Z' })
const api = vi.hoisted(() => ({
  requests: vi.fn(async (): Promise<{ requests: unknown[] }> => ({ requests: [] })),
  declineRequest: vi.fn(async (..._args: unknown[]) => undefined),
  approveRequest: vi.fn(async (..._args: unknown[]) => undefined),
}))
vi.mock('../../api/client', () => ({ api, ApiError: class extends Error {} }))

import { clearAsyncCache } from '../../hooks/useAsync'
import { RequestsPage } from './Requests'

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

const flush = () => act(async () => {})
const open = async () => {
  await act(async () => {
    root.render(
      <MemoryRouter>
        <RequestsPage />
      </MemoryRouter>,
    )
  })
  await flush()
}
const card = (title: string) => Array.from(host.querySelectorAll('.req')).find((r) => r.textContent?.includes(title)) as HTMLElement
const press = async (within: HTMLElement, text: string) => {
  const b = Array.from(within.querySelectorAll('button')).find((x) => x.textContent?.trim() === text) as HTMLButtonElement
  await act(async () => b.click())
  await flush()
}
const reasonBox = () => host.querySelector('.req__decline input') as HTMLInputElement | null
const type = (value: string) =>
  act(() => {
    const el = reasonBox()!
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value)
    el.dispatchEvent(new Event('input', { bubbles: true }))
  })
const notices = () => Array.from(host.querySelectorAll('.notice')).map((n) => n.textContent)

// The page has one reason box. A reason typed for one request and cancelled stayed in it,
// and was sent with the next request that was declined.
describe('Declining a request', () => {
  beforeEach(() => api.requests.mockResolvedValue({ requests: [waiting('A', 'First show'), waiting('B', 'Second show')] }))

  it('sends the reason typed for it', async () => {
    await open()
    await press(card('First show'), 'Decline')
    type('we already have this one')
    await press(card('First show'), 'Decline')
    expect(api.declineRequest).toHaveBeenCalledWith('A', 'we already have this one')
  })

  it('does not send a reason typed for another request and cancelled', async () => {
    await open()
    await press(card('First show'), 'Decline')
    type('we already have this one')
    await press(card('First show'), 'Cancel')
    await press(card('Second show'), 'Decline')
    expect(reasonBox()!.value).toBe('')
    await press(card('Second show'), 'Decline')
    expect(api.declineRequest).toHaveBeenCalledTimes(1)
    expect(api.declineRequest).toHaveBeenCalledWith('B', undefined)
  })

  it('nor one typed for another request that was left open', async () => {
    await open()
    await press(card('First show'), 'Decline')
    type('not this season')
    await press(card('Second show'), 'Decline') // straight to the other one
    expect(reasonBox()!.value).toBe('')
    await press(card('Second show'), 'Decline')
    expect(api.declineRequest).toHaveBeenCalledWith('B', undefined)
  })
})

// "Nobody has requested anything yet." beside "the server did not answer": the list was
// never read, so nothing is known to be empty.
describe('The list', () => {
  it('says nobody has asked only when the server said so', async () => {
    api.requests.mockResolvedValue({ requests: [] })
    await open()
    expect(notices()).toContain('Nobody has requested anything yet.')
  })

  it('says only what went wrong when it could not be read', async () => {
    api.requests.mockRejectedValue(new Error('The server did not answer.'))
    await open()
    expect(notices()).toEqual(['The server did not answer.'])
  })
})
