// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const api = vi.hoisted(() => ({
  libraryManage: vi.fn(async () => ({ scanning: false, queue: 0, titles: [{ id: 't1', title: 'A show', year: 2019, type: 'series', matched: false, locked: false }], emptyLibraries: [] })),
  folders: vi.fn(async () => ({ libraries: [] })),
  item: vi.fn(async () => ({ id: 't1', type: 'series', title: 'A show', year: 2019, overview: 'About it.', genres: ['Drama'], studio: 'Studio', premiered: '2019-04-06' })),
  putMetadata: vi.fn(async (..._args: unknown[]) => undefined),
}))
vi.mock('../../api/client', () => ({ api, ApiError: class extends Error {} }))

import { LibraryPage } from './Library'

let host: HTMLDivElement
let root: Root
beforeEach(() => {
  vi.clearAllMocks()
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
})

const flush = () => act(async () => {})
const button = (text: string) => Array.from(host.querySelectorAll('button')).find((b) => b.textContent?.trim() === text) as HTMLButtonElement
const click = async (text: string) => {
  await act(async () => button(text).click())
  await flush()
}
/** The input under a field's label. */
const field = (label: string) => {
  const all = Array.from(host.querySelectorAll('.acc-sub label, .acc-sub .ffield'))
  const box = all.find((el) => el.textContent?.startsWith(label))
  return box!.querySelector('input') as HTMLInputElement
}
const type = (label: string, value: string) =>
  act(() => {
    const el = field(label)
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, value)
    el.dispatchEvent(new Event('input', { bubbles: true }))
  })
const errors = () => Array.from(host.querySelectorAll('.notice--error')).map((n) => n.textContent)

/** The Library page with a title's Edit details form open and filled from the server. */
async function openForm() {
  await act(async () => {
    root.render(
      <MemoryRouter>
        <LibraryPage />
      </MemoryRouter>,
    )
  })
  await flush()
  await click('Edit details')
  expect(field('Year').value).toBe('2019')
  expect(field('Premiered').value).toBe('2019-04-06')
}

// Number("20x6") is NaN, which JSON sends as null: a typo in the year erased the year.
// The date went to the server as typed, whatever it was.
describe('Edit details', () => {
  it('saves a year and a date', async () => {
    await openForm()
    type('Year', ' 2020 ')
    type('Premiered', '2020-01-11')
    await click('Save')
    expect(errors()).toEqual([])
    expect(api.putMetadata).toHaveBeenCalledTimes(1)
    expect(api.putMetadata.mock.calls[0][1]).toMatchObject({ year: 2020, premiered: '2020-01-11' })
  })

  it('still clears a year or a date left empty', async () => {
    await openForm()
    type('Year', '')
    type('Premiered', '')
    await click('Save')
    expect(api.putMetadata.mock.calls[0][1]).toMatchObject({ year: null, premiered: '' })
  })

  it.each(['20x6', '19', '20190', '2019.5', '-2019'])('refuses the year %j, says so, and sends nothing', async (year) => {
    await openForm()
    type('Year', year)
    await click('Save')
    expect(api.putMetadata).not.toHaveBeenCalled()
    expect(errors()).toEqual(['The year must be four digits, like 2019.'])
    expect(field('Year').value).toBe(year) // the form stays open with what was typed
  })

  it.each(['April 2019', '2019-4-6', '06/04/2019', '2019'])('refuses the date %j, says so, and sends nothing', async (date) => {
    await openForm()
    type('Premiered', date)
    await click('Save')
    expect(api.putMetadata).not.toHaveBeenCalled()
    expect(errors()).toEqual(['The date must look like 2019-04-06 (year-month-day).'])
  })
})
