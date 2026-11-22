// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const DOTS = '••••••••'
const saved = () => ({
  sonarr: { url: 'http://sonarr:8989', apiKey: DOTS, rootFolder: '/data/Anime', qualityProfileId: 4, tags: [], animeTags: [], seasonFolders: true },
  radarr: { url: '', apiKey: '', rootFolder: '', qualityProfileId: null, tags: [], animeTags: [], seasonFolders: true },
})
const api = vi.hoisted(() => ({
  arrSettings: vi.fn(async (): Promise<unknown> => ({})),
  testArr: vi.fn(async (..._args: unknown[]) => ({ version: '4', rootFolders: [], qualityProfiles: [], tags: [] })),
  saveArrSettings: vi.fn(async (body: unknown) => body),
}))
vi.mock('../../api/client', () => ({ api, ApiError: class extends Error {} }))

import { clearAsyncCache } from '../../hooks/useAsync'
import { ArrPage, SAVED_KEY } from './Arr'

let host: HTMLDivElement
let root: Root
beforeEach(() => {
  vi.clearAllMocks()
  clearAsyncCache()
  api.arrSettings.mockImplementation(async () => saved())
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
  await act(async () => root.render(<ArrPage />))
  await flush()
  await flush()
}
const keyBox = () => host.querySelector('.fsecret input') as HTMLInputElement
const type = (value: string) =>
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(keyBox(), value)
    keyBox().dispatchEvent(new Event('input', { bubbles: true }))
  })
const click = async (text: string) => {
  const b = Array.from(host.querySelectorAll('button')).find((x) => x.textContent?.trim() === text) as HTMLButtonElement
  await act(async () => b.click())
  await flush()
}
const sentKey = () => (api.saveArrSettings.mock.calls[0][0] as { sonarr: { apiKey: string } }).sonarr.apiKey

// A saved key comes from the server as eight dots, which mean "keep the saved key" only
// when they come back exactly so. They sat in the box as text: a new key pasted in after
// them went out as dots + key, which failed the test, and Save stored that.
describe('Sonarr & Radarr: the API key box', () => {
  it('is what the server sends for a saved key', () => {
    expect(SAVED_KEY).toBe(DOTS)
  })

  it('is empty for a saved key, and says one is saved', async () => {
    await open()
    expect(keyBox().value).toBe('')
    expect(keyBox().placeholder).toMatch(/^Saved/)
  })

  it('keeps the saved key when nothing is typed', async () => {
    await open()
    await click('Save')
    expect(sentKey()).toBe(DOTS)
    await click('Test connection')
    expect(api.testArr).toHaveBeenCalledWith('sonarr', 'http://sonarr:8989', DOTS)
  })

  it('sends a pasted key as it is', async () => {
    await open()
    type('0123456789abcdef0123456789abcdef')
    expect(keyBox().value).toBe('0123456789abcdef0123456789abcdef')
    await click('Save')
    expect(sentKey()).toBe('0123456789abcdef0123456789abcdef')
  })

  it('goes back to the saved key when what was typed is cleared', async () => {
    await open()
    type('wrong-key')
    type('')
    expect(keyBox().value).toBe('')
    await click('Save')
    expect(sentKey()).toBe(DOTS)
  })

  it('has nothing to keep where no key is saved', async () => {
    await open()
    await click('Radarr (movies)')
    expect(keyBox().value).toBe('')
    expect(keyBox().placeholder).toBe('')
    type('abc')
    type('')
    await click('Save')
    expect((api.saveArrSettings.mock.calls[0][0] as { radarr: { apiKey: string } }).radarr.apiKey).toBe('')
  })
})
