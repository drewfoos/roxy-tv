// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { usePlayerKeys, type PlayerKeyActions } from './usePlayerKeys'

// React's act() needs to be told this is a test environment.
;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

let root: Root
let host: HTMLDivElement
let actions: { [K in keyof PlayerKeyActions]: ReturnType<typeof vi.fn> }

function Harness({ a }: { a: PlayerKeyActions }) {
  usePlayerKeys(a)
  return <input data-testid="field" />
}

beforeEach(() => {
  actions = { togglePlay: vi.fn(), seekBy: vi.fn(), volumeBy: vi.fn(), toggleFullscreen: vi.fn(), toggleMute: vi.fn(), toggleSubs: vi.fn(), next: vi.fn(), closeMenu: vi.fn(), poke: vi.fn() }
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
  act(() => root.render(<Harness a={actions as unknown as PlayerKeyActions} />))
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
})

const press = (key: string, init: KeyboardEventInit = {}, target: EventTarget = window) => {
  const e = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...init })
  act(() => {
    target.dispatchEvent(e)
  })
  return e
}

describe('usePlayerKeys', () => {
  it('maps the player keys', () => {
    press(' ')
    press('k')
    expect(actions.togglePlay).toHaveBeenCalledTimes(2)
    press('ArrowLeft')
    press('l')
    expect(actions.seekBy.mock.calls).toEqual([[-10], [10]])
    press('ArrowUp')
    press('ArrowDown')
    expect(actions.volumeBy.mock.calls).toEqual([[0.1], [-0.1]])
    press('f')
    press('m')
    press('c')
    press('n')
    press('Escape')
    for (const a of ['toggleFullscreen', 'toggleMute', 'toggleSubs', 'next', 'closeMenu'] as const) expect(actions[a]).toHaveBeenCalledTimes(1)
    expect(actions.poke).toHaveBeenCalledTimes(11) // every handled key shows the controls
  })

  it('stops the page scrolling for keys it uses', () => {
    expect(press(' ').defaultPrevented).toBe(true)
    expect(press('ArrowDown').defaultPrevented).toBe(true)
  })

  it('leaves browser and system shortcuts alone', () => {
    press('l', { ctrlKey: true }) // address bar
    press('f', { metaKey: true }) // find
    press('ArrowLeft', { altKey: true }) // back
    expect(actions.seekBy).not.toHaveBeenCalled()
    expect(actions.toggleFullscreen).not.toHaveBeenCalled()
    expect(actions.poke).not.toHaveBeenCalled()
  })

  it('ignores keys typed in a field, and keys it does not use', () => {
    const field = host.querySelector('input')!
    press('k', {}, field)
    press(' ', {}, field)
    press('x')
    expect(actions.togglePlay).not.toHaveBeenCalled()
    expect(actions.poke).not.toHaveBeenCalled()
  })
})
