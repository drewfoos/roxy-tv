// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const api = vi.hoisted(() => ({
  tmdb: vi.fn(async (): Promise<{ set: boolean; source?: string }> => ({ set: true })),
  fanart: vi.fn(async (): Promise<{ enabled: boolean }> => ({ enabled: false })),
  saveTmdb: vi.fn(async (..._a: unknown[]) => undefined),
  saveFanart: vi.fn(async (..._a: unknown[]) => undefined),
}))
vi.mock('../../api/client', () => ({ api, ApiError: class extends Error {} }))

import { clearAsyncCache } from '../../hooks/useAsync'
import { KeysPage } from './Keys'

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
  await act(async () => root.render(<KeysPage />))
  await act(async () => {})
}
const notices = () => Array.from(host.querySelectorAll('.notice')).map((n) => n.textContent ?? '')

// When either state could not be read its line stayed "…" for good, with no word of why.
describe('TMDB & fanart.tv', () => {
  it('says what is saved', async () => {
    await open()
    expect(notices()[0]).toMatch(/^A TMDB key is saved/)
    expect(notices().some((n) => n === '…')).toBe(false)
  })

  it('says so when it could not read whether a key is saved', async () => {
    api.tmdb.mockRejectedValueOnce(new Error('The server did not answer.'))
    api.fanart.mockRejectedValueOnce(new Error('The server did not answer.'))
    await open()
    expect(notices().filter((n) => n === 'The server did not answer.')).toHaveLength(2)
    expect(notices().some((n) => n === '…')).toBe(false)
    expect(notices().some((n) => n.startsWith('Roxy could not read whether a TMDB key is saved'))).toBe(true)
  })
})
