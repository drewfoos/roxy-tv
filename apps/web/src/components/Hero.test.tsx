// @vitest-environment jsdom
import { act } from 'react'
import { createRoot } from 'react-dom/client'
import { MemoryRouter } from 'react-router'
import { expect, it } from 'vitest'
import type { Item } from '../api/types'
import { Hero } from './Hero'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const series = (id: string) => ({ id, type: 'series', title: id }) as Item

// Every slide is in the page at once and only one is shown: Tab must not walk through the
// links and buttons of the slides that are not (they are see-through, not gone).
it('keeps the slides that are not shown out of reach of the keyboard', async () => {
  const host = document.createElement('div')
  const root = createRoot(host)
  await act(async () =>
    root.render(
      <MemoryRouter>
        <Hero heroes={['a', 'b', 'c'].map((id) => ({ title: series(id), play: series(id) }))} />
      </MemoryRouter>,
    ),
  )
  const slides = [...host.querySelectorAll('.hero__slide')]
  expect(slides.map((s) => s.hasAttribute('inert'))).toEqual([false, true, true])
  act(() => root.unmount())
})
