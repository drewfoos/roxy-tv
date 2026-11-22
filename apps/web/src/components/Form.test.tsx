// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { QUALITY_PREFS, type QualityPref } from '../api/prefs'
import { ActionButton, CONFIRM_ARM_MS, ConfirmButton, Select, copyText } from './Form'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

let host: HTMLDivElement
let root: Root
beforeEach(() => {
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
  vi.restoreAllMocks()
})
const flush = () => act(async () => {})
const button = () => host.querySelector('button') as HTMLButtonElement

// A list with both kinds of id (Video Quality: 'original' and heights). The select took
// "is the value in force a number?" for "are the ids numbers?".
describe('Select', () => {
  const choose = (value: string) =>
    act(() => {
      const el = host.querySelector('select') as HTMLSelectElement
      el.value = value
      el.dispatchEvent(new Event('change', { bubbles: true }))
    })

  it("hands on the option's own id, a number picked from a text", () => {
    const picked: QualityPref[] = []
    act(() => root.render(<Select value={'original' as QualityPref} options={QUALITY_PREFS} onChange={(v) => picked.push(v)} />))
    choose('720')
    expect(picked).toEqual([720])
    expect(typeof picked[0]).toBe('number')
  })

  it('and a text picked from a number', () => {
    const picked: QualityPref[] = []
    act(() => root.render(<Select value={720 as QualityPref} options={QUALITY_PREFS} onChange={(v) => picked.push(v)} />))
    choose('original')
    expect(picked).toEqual(['original'])
  })

  it('shows the option that is in force', () => {
    act(() => root.render(<Select value={1080 as QualityPref} options={QUALITY_PREFS} onChange={() => {}} />))
    expect((host.querySelector('select') as HTMLSelectElement).value).toBe('1080')
  })
})

// "Tap again to delete": the armed button is where the first click was, so a double-click
// deleted a profile, an account or a library folder in one go.
describe('ConfirmButton', () => {
  let clock = 1_000_000
  const now = () => clock
  const press = async () => {
    await act(async () => button().click())
    await flush()
  }

  it('arms on the first press and acts on the second', async () => {
    const onConfirm = vi.fn(async () => {})
    const onArmed = vi.fn()
    act(() => root.render(<ConfirmButton label="Delete profile" confirm="Tap again to delete" onConfirm={onConfirm} onArmed={onArmed} now={now} />))
    expect(button().textContent).toBe('Delete profile')
    await press()
    expect(button().textContent).toBe('Tap again to delete')
    expect(button().className).toContain('btn--danger')
    expect(onArmed).toHaveBeenCalledTimes(1)
    expect(onConfirm).not.toHaveBeenCalled()
    clock += CONFIRM_ARM_MS + 1
    await press()
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('takes a double-click for one press', async () => {
    const onConfirm = vi.fn(async () => {})
    act(() => root.render(<ConfirmButton label="Delete" confirm="Tap again to delete" onConfirm={onConfirm} now={now} />))
    await press()
    clock += 120 // the second click of a double-click
    await press()
    clock += CONFIRM_ARM_MS - 200
    await press()
    expect(onConfirm).not.toHaveBeenCalled()
    expect(button().textContent).toBe('Tap again to delete')
  })

  it('does one removal however often it is pressed while that runs', async () => {
    let finish = () => {}
    const onConfirm = vi.fn(() => new Promise<void>((r) => (finish = r)))
    act(() => root.render(<ConfirmButton label="Delete" confirm="Tap again to delete" onConfirm={onConfirm} now={now} />))
    await press()
    clock += CONFIRM_ARM_MS + 1
    await press()
    expect(button().disabled).toBe(true)
    expect(button().textContent).toBe('Working…')
    await press()
    await press()
    expect(onConfirm).toHaveBeenCalledTimes(1)
    await act(async () => finish())
    expect(button().disabled).toBe(false)
  })
})

// A form with several fields and no submit button does nothing on Enter. The button of
// such a form is its submit button: Enter in a field presses it (the browser's own rule),
// which runs its action with its busy guard.
describe('ActionButton in a form', () => {
  it('is the submit button, and one press is one action', async () => {
    const onClick = vi.fn(async () => {})
    const onSubmit = vi.fn((e: { preventDefault: () => void }) => e.preventDefault())
    act(() =>
      root.render(
        <form onSubmit={onSubmit}>
          <input name="a" />
          <input name="b" />
          <ActionButton submit onClick={onClick}>
            Change password
          </ActionButton>
        </form>,
      ),
    )
    expect(button().type).toBe('submit')
    await act(async () => button().click())
    await flush()
    expect(onClick).toHaveBeenCalledTimes(1)
    expect(onSubmit).toHaveBeenCalledTimes(1) // the form's own submit is only stopped from leaving the page
  })

  it('stays a plain button elsewhere', () => {
    act(() => root.render(<ActionButton onClick={() => {}}>Save</ActionButton>))
    expect(button().type).toBe('button')
  })

  it('does nothing when pressed while disabled', async () => {
    const onClick = vi.fn(async () => {})
    act(() =>
      root.render(
        <ActionButton submit disabled onClick={onClick}>
          Change password
        </ActionButton>,
      ),
    )
    await act(async () => button().click())
    expect(onClick).not.toHaveBeenCalled()
  })
})

// The clipboard API exists only on https and localhost; Roxy is mostly opened at a plain
// http address at home, where every Copy button did nothing and said nothing.
describe('copyText', () => {
  const setClipboard = (value: unknown) => Object.defineProperty(navigator, 'clipboard', { value, configurable: true })
  afterEach(() => {
    setClipboard(undefined)
    delete (document as { execCommand?: unknown }).execCommand
  })

  it('uses the clipboard where there is one', async () => {
    const writeText = vi.fn(async () => {})
    setClipboard({ writeText })
    expect(await copyText('http://server/v1/webhooks/sonarr')).toBe(true)
    expect(writeText).toHaveBeenCalledWith('http://server/v1/webhooks/sonarr')
  })

  it('copies the old way where there is none (plain http)', async () => {
    setClipboard(undefined)
    let copied = ''
    // What the copy command does: takes the selection of the element that has the focus.
    ;(document as { execCommand?: unknown }).execCommand = vi.fn((cmd: string) => {
      const box = document.activeElement as HTMLTextAreaElement | null
      if (cmd === 'copy' && box?.tagName === 'TEXTAREA') copied = box.value.slice(box.selectionStart ?? 0, box.selectionEnd ?? 0)
      return true
    })
    act(() => root.render(<button>Copy</button>))
    button().focus()
    expect(await copyText('the address')).toBe(true)
    expect(copied).toBe('the address')
    expect(document.querySelector('textarea')).toBeNull() // nothing left on the page
    expect(document.activeElement).toBe(button()) // and the focus is back where it was
  })

  it('also when the clipboard refuses', async () => {
    setClipboard({ writeText: vi.fn(async () => Promise.reject(new Error('NotAllowedError'))) })
    ;(document as { execCommand?: unknown }).execCommand = vi.fn(() => true)
    expect(await copyText('x')).toBe(true)
  })

  it('says so when nothing works', async () => {
    setClipboard(undefined)
    ;(document as { execCommand?: unknown }).execCommand = vi.fn(() => false)
    expect(await copyText('x')).toBe(false)
    delete (document as { execCommand?: unknown }).execCommand
    expect(await copyText('x')).toBe(false)
  })
})
