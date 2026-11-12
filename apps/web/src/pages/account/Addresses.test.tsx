// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AddressSettings } from '../../api/admin'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const api = vi.hoisted(() => ({
  addresses: vi.fn<() => Promise<AddressSettings>>(),
  saveAddresses: vi.fn<(list: string[]) => Promise<AddressSettings>>(),
}))
vi.mock('../../api/client', () => ({ api, ApiError: class extends Error {} }))

import { clearAsyncCache } from '../../hooks/useAsync'
import { AddressesPage, addressLines } from './Addresses'

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
        <AddressesPage />
      </MemoryRouter>,
    )
  })
  await flush()
}
const box = () => host.querySelector('textarea') as HTMLTextAreaElement | null
const type = (value: string) =>
  act(() => {
    const el = box()!
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!.call(el, value)
    el.dispatchEvent(new Event('input', { bubbles: true }))
  })
/** The page's one button (it reads "Saved" for a moment after a save that worked). */
const saveButton = () => (host.querySelector('.acc-actions button') ?? undefined) as HTMLButtonElement | undefined
const save = async () => {
  await act(async () => saveButton()!.click())
  await flush()
}
const notices = (kind: string) => Array.from(host.querySelectorAll('.notice--' + kind)).map((n) => n.textContent ?? '')

describe('addressLines', () => {
  it('takes one address per line and drops blanks', () => {
    expect(addressLines(' http://100.64.0.7:8097 \r\n\n  \nhttps://roxy.example.com\n')).toEqual(['http://100.64.0.7:8097', 'https://roxy.example.com'])
    expect(addressLines('')).toEqual([])
  })
})

describe('Server addresses', () => {
  it('shows what is saved and the home address', async () => {
    api.addresses.mockResolvedValue({ advertised: 'http://192.168.1.50:8097', addresses: ['http://100.64.0.7:8097', 'https://roxy.example.com'], fromEnvironment: false })
    await open()
    expect(box()!.value).toBe('http://100.64.0.7:8097\nhttps://roxy.example.com')
    expect(host.textContent).toContain('http://192.168.1.50:8097')
  })

  it('saves the lines typed, shows them as the server wrote them, and says what each check found', async () => {
    api.addresses.mockResolvedValue({ advertised: '', addresses: [], fromEnvironment: false })
    api.saveAddresses.mockResolvedValue({
      advertised: '',
      addresses: ['http://100.64.0.7:8097', 'https://roxy.example.com'],
      fromEnvironment: false,
      checks: [
        { address: 'http://100.64.0.7:8097', state: 'ok' },
        { address: 'https://roxy.example.com', state: 'unreachable' },
      ],
    })
    await open()
    await type(' http://100.64.0.7:8097/ \n\nHTTPS://roxy.example.com')
    await save()
    expect(api.saveAddresses).toHaveBeenCalledTimes(1)
    expect(api.saveAddresses.mock.calls[0][0]).toEqual(['http://100.64.0.7:8097/', 'HTTPS://roxy.example.com'])
    expect(box()!.value).toBe('http://100.64.0.7:8097\nhttps://roxy.example.com')
    expect(notices('ok')).toEqual(['http://100.64.0.7:8097 reaches this server.'])
    expect(notices('info')).toHaveLength(1)
    expect(notices('info')[0]).toContain('https://roxy.example.com is saved')
    expect(notices('error')).toEqual([])
  })

  it('saves an empty list when the box is emptied', async () => {
    api.addresses.mockResolvedValue({ advertised: '', addresses: ['http://100.64.0.7:8097'], fromEnvironment: false })
    api.saveAddresses.mockResolvedValue({ advertised: '', addresses: [], fromEnvironment: false, checks: [] })
    await open()
    await type('  \n')
    await save()
    expect(api.saveAddresses.mock.calls[0][0]).toEqual([])
    expect(box()!.value).toBe('')
  })

  it("shows the server's refusal and keeps what was typed", async () => {
    api.addresses.mockResolvedValue({ advertised: '', addresses: [], fromEnvironment: false })
    api.saveAddresses.mockRejectedValue(new Error('http://100.64.0.9:8097 is a different Roxy server, not this one. Nothing was saved.'))
    await open()
    await type('http://100.64.0.9:8097')
    await save()
    expect(notices('error')).toEqual(['http://100.64.0.9:8097 is a different Roxy server, not this one. Nothing was saved.'])
    expect(notices('ok')).toEqual([])
    expect(box()!.value).toBe('http://100.64.0.9:8097')
    expect(saveButton()!.textContent).toBe('Save addresses') // not "Saved"
  })

  it('a second save clears the last result first', async () => {
    api.addresses.mockResolvedValue({ advertised: '', addresses: [], fromEnvironment: false })
    api.saveAddresses.mockResolvedValueOnce({ advertised: '', addresses: ['http://100.64.0.7:8097'], fromEnvironment: false, checks: [{ address: 'http://100.64.0.7:8097', state: 'ok' }] })
    api.saveAddresses.mockRejectedValueOnce(new Error('At most 8 addresses.'))
    await open()
    await type('http://100.64.0.7:8097')
    await save()
    expect(notices('ok')).toHaveLength(1)
    expect(saveButton()!.textContent).toBe('Saved')
    await save()
    expect(notices('ok')).toEqual([])
    expect(notices('error')).toEqual(['At most 8 addresses.'])
  })

  it('only lists them when ROXY_OTHER_URLS sets them', async () => {
    api.addresses.mockResolvedValue({ advertised: 'http://192.168.1.50:8097', addresses: ['http://100.64.0.7:8097'], fromEnvironment: true })
    await open()
    expect(box()).toBeNull()
    expect(saveButton()).toBeUndefined()
    expect(host.textContent).toContain('http://100.64.0.7:8097')
    expect(host.textContent).toContain('ROXY_OTHER_URLS')
  })

  it('says so when the list cannot be loaded, and offers no form to save over it', async () => {
    api.addresses.mockRejectedValue(new Error('Server error'))
    await open()
    expect(box()).toBeNull()
    expect(saveButton()).toBeUndefined()
    expect(host.textContent).toContain('Server error')
  })
})
