// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const api = vi.hoisted(() => ({ changePassword: vi.fn(async (..._args: unknown[]) => undefined) }))
vi.mock('../../api/client', () => ({ api, ApiError: class extends Error {} }))
vi.mock('../../app/useSession', () => ({ useSession: () => ({ userName: 'drew' }) }))

import { PasswordPage } from './Password'

let host: HTMLDivElement
let root: Root
beforeEach(() => {
  vi.clearAllMocks()
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
  act(() => root.render(<PasswordPage />))
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
})
const flush = () => act(async () => {})
const boxes = () => Array.from(host.querySelectorAll('input')) as HTMLInputElement[]
const fill = (values: string[]) =>
  act(() => {
    boxes().forEach((el, i) => {
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(el, values[i])
      el.dispatchEvent(new Event('input', { bubbles: true }))
    })
  })
const theButton = () => host.querySelector('form button.btn') as HTMLButtonElement
/** Enter in a field: the browser presses the form's submit button, if it has one that is not disabled. */
const pressEnter = async () => {
  const submit = host.querySelector('form button[type="submit"]:not([disabled])') as HTMLButtonElement | null
  if (submit) await act(async () => submit.click())
  await flush()
}

// The form had three fields and no submit button (its button was a plain one), and a form
// like that does nothing on Enter.
describe('Change Password', () => {
  it('has a submit button, so Enter in a field changes the password', async () => {
    expect(theButton().type).toBe('submit')
    fill(['old-password', 'new-password-1', 'new-password-1'])
    await pressEnter()
    expect(api.changePassword).toHaveBeenCalledTimes(1)
    expect(api.changePassword).toHaveBeenCalledWith('old-password', 'new-password-1')
    expect(host.textContent).toContain('Password changed.')
    expect(boxes().map((b) => b.value)).toEqual(['', '', '']) // cleared after it worked
  })

  it('changes it once for one press of the button', async () => {
    fill(['old-password', 'new-password-1', 'new-password-1'])
    await act(async () => theButton().click())
    await flush()
    expect(api.changePassword).toHaveBeenCalledTimes(1)
  })

  it('does nothing on Enter while a field is empty', async () => {
    fill(['old-password', 'new-password-1', ''])
    expect(theButton().disabled).toBe(true)
    await pressEnter()
    expect(api.changePassword).not.toHaveBeenCalled()
  })

  it('still says what is wrong with the new password, and sends nothing', async () => {
    fill(['old-password', 'short', 'short'])
    await pressEnter()
    expect(api.changePassword).not.toHaveBeenCalled()
    expect(host.querySelector('.notice--error')?.textContent).toBe('The new password needs at least 8 characters.')
  })
})
