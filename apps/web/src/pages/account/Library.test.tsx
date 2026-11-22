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
  identifySearch: vi.fn(async (_id: string, provider: string, _q: string) => ({
    candidates: provider === 'tmdb' ? [{ id: 123, title: 'A show (TMDB)', year: 2019 }] : [{ id: 987, title: 'A show (AniList)', year: 2019 }],
  })),
  identifyApply: vi.fn(async (..._args: unknown[]) => undefined),
  browse: vi.fn(async (path?: string) =>
    path
      ? { path, parent: '', dirs: [], roots: undefined as { name: string; path: string }[] | undefined }
      : { path: '', parent: null as string | null, dirs: [], roots: [{ name: 'Anime', path: '/mnt/media/Anime' }, { name: 'Films', path: '/mnt/media/Films' }] },
  ),
  addFolder: vi.fn(async (..._args: unknown[]) => undefined),
}))
vi.mock('../../api/client', () => ({ api, ApiError: class extends Error {} }))

import { clearAsyncCache } from '../../hooks/useAsync'
import { LibraryPage } from './Library'

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

const openPage = async () => {
  await act(async () => {
    root.render(
      <MemoryRouter>
        <LibraryPage />
      </MemoryRouter>,
    )
  })
  await flush()
}
const notices = () => Array.from(host.querySelectorAll('.notice')).map((n) => n.textContent)

// The list of titles that need attention was "empty" before it was read, and after a read
// that failed: the page said, in green, that every title was matched.
describe('Needs attention', () => {
  it('does not say every title is matched before the list is read', async () => {
    let answer = (_: Awaited<ReturnType<typeof api.libraryManage>>) => {}
    api.libraryManage.mockImplementationOnce(() => new Promise((r) => (answer = r)))
    await openPage()
    expect(notices()).not.toContain('Every title is matched.')
    await act(async () => answer({ scanning: false, queue: 0, titles: [], emptyLibraries: [] }))
    await flush()
    expect(notices()).toContain('Every title is matched.')
  })

  it('nor when the list could not be read', async () => {
    api.libraryManage.mockRejectedValueOnce(new Error('The server did not answer.'))
    await openPage()
    expect(errors()).toContain('The server did not answer.')
    expect(notices()).not.toContain('Every title is matched.')
  })
})

// "Keep my edits" was off when the form opened (the phone's is on): details typed by hand
// were saved unprotected, the title stayed under Needs attention, and the next refresh
// could replace them.
describe('Edit details: Keep my edits', () => {
  it('starts on, and is sent on', async () => {
    await openForm()
    const keep = host.querySelector('.acc-sub [role="switch"]') as HTMLButtonElement
    expect(keep.getAttribute('aria-checked')).toBe('true')
    await click('Save')
    expect(api.putMetadata.mock.calls[0][1]).toMatchObject({ locked: true })
  })

  it('can still be turned off', async () => {
    await openForm()
    await act(async () => (host.querySelector('.acc-sub [role="switch"]') as HTMLButtonElement).click())
    await click('Save')
    expect(api.putMetadata.mock.calls[0][1]).toMatchObject({ locked: false })
  })
})

// A result's id belongs to the provider it came from. The provider chips changed only what
// the match was sent as: a TMDB result picked, AniList pressed, and the title was saved
// with TMDB's id as an AniList id (another show's details and art, or none).
describe('Identify', () => {
  const row = (text: string) => Array.from(host.querySelectorAll('.acc-sub .lrow')).find((r) => r.textContent?.includes(text)) as HTMLElement
  const openIdentify = async () => {
    await openPage()
    await click('Identify')
  }

  it('sends a match as the provider its result came from', async () => {
    await openIdentify()
    await click('Search')
    await act(async () => row('A show (TMDB)').click())
    await click('Yes, use this match')
    expect(api.identifyApply).toHaveBeenCalledWith('t1', 'tmdb', 123)
  })

  it('drops the results and the pick when the other provider is chosen', async () => {
    await openIdentify()
    await click('Search')
    await act(async () => row('A show (TMDB)').click())
    expect(button('Yes, use this match')).toBeTruthy()
    await click('AniList')
    expect(row('A show (TMDB)')).toBeUndefined()
    expect(button('Yes, use this match')).toBeUndefined()
    expect(api.identifyApply).not.toHaveBeenCalled()
    // Searched again there, the match is AniList's own.
    await click('Search')
    await act(async () => row('A show (AniList)').click())
    await click('Yes, use this match')
    expect(api.identifyApply).toHaveBeenCalledTimes(1)
    expect(api.identifyApply).toHaveBeenCalledWith('t1', 'anilist', 987)
  })

  it('drops the pick when a new search is made', async () => {
    await openIdentify()
    await click('Search')
    await act(async () => row('A show (TMDB)').click())
    await click('Search')
    expect(button('Yes, use this match')).toBeUndefined()
  })

  // A search still on its way when the other provider is chosen: its results arrived under
  // the other provider's chip, to be picked and sent as that provider's.
  it('drops the answer of a search that was overtaken by the other provider', async () => {
    let answer = (_: { candidates: { id: number; title: string; year: number }[] }) => {}
    api.identifySearch.mockImplementationOnce(() => new Promise((r) => (answer = r)))
    await openIdentify()
    await act(async () => button('Search').click()) // TMDB, slow
    await click('AniList')
    await act(async () => answer({ candidates: [{ id: 123, title: 'A show (TMDB)', year: 2019 }] }))
    await flush()
    expect(row('A show (TMDB)')).toBeUndefined()
    expect(host.textContent).not.toContain('Nothing found')
  })

  it('keeps what is there when the chip of the provider in use is pressed', async () => {
    await openIdentify()
    await click('Search')
    await act(async () => row('A show (TMDB)').click())
    await click('TMDB')
    expect(button('Yes, use this match')).toBeTruthy()
  })
})

describe('Add folder', () => {
  const folderRow = (text: string) => Array.from(host.querySelectorAll('.acc-sub .lrow')).find((r) => r.textContent?.includes(text)) as HTMLElement
  const openAdd = async () => {
    await openPage()
    await click('Add folder')
  }

  // Cancel was only offered once a folder had been opened; on the first list (and when
  // that list could not be read) there was no way back.
  it('can be cancelled from its first list', async () => {
    await openAdd()
    expect(folderRow('Anime')).toBeTruthy()
    await click('Cancel')
    expect(host.querySelector('.acc-sub')).toBeNull()
    expect(button('Add folder')).toBeTruthy()
  })

  it('can be cancelled, or tried again, when the folders could not be read', async () => {
    api.browse.mockRejectedValueOnce(new Error('The server did not answer.'))
    await openAdd()
    expect(errors()).toContain('The server did not answer.')
    await click('Try again')
    expect(folderRow('Anime')).toBeTruthy()
    await click('Cancel')
    expect(button('Add folder')).toBeTruthy()
  })

  // Use a folder, go back, use another: the name stayed the first folder's while the type
  // and the anime switch were guessed again for the second.
  it('names the folder after the one in use, unless a name was typed', async () => {
    await openAdd()
    await act(async () => folderRow('Anime').click())
    await flush()
    await click('Use this folder')
    expect(field('Name').value).toBe('Anime')
    await click('Back')
    await act(async () => button('Up').click())
    await flush()
    await act(async () => folderRow('Films').click())
    await flush()
    await click('Use this folder')
    expect(field('Name').value).toBe('Films')
    // A name of one's own is kept.
    type('Name', 'Weekend films')
    await click('Back')
    await act(async () => button('Up').click())
    await flush()
    await act(async () => folderRow('Anime').click())
    await flush()
    await click('Use this folder')
    expect(field('Name').value).toBe('Weekend films')
  })
})
