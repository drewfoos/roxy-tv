// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// A new server asks for its setup code (in its log and its data folder): being on the
// server's network is not proof of being its owner. The wizard asks for it and sends it.
const calls = { admin: [] as unknown[][] }
vi.mock('../api/client', async (orig) => {
  const real = await orig<typeof import('../api/client')>()
  return {
    ...real,
    api: {
      ...real.api,
      legal: () => Promise.resolve({ version: '1', title: '', body: '' }),
      setupAdmin: (...a: unknown[]) => (calls.admin.push(a), new Promise(() => {})),
    },
  }
})

import { SetupWizard } from './Setup'

let host: HTMLDivElement
let root: Root
beforeEach(() => {
  ;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  localStorage.clear()
  calls.admin = []
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
})

async function open(status: Record<string, unknown>) {
  await act(async () => {
    root.render(
      <MemoryRouter>
        <SetupWizard status={status} />
      </MemoryRouter>,
    )
  })
  // Past the welcome page, to the account.
  const next = Array.from(host.querySelectorAll('button')).find((b) => /get started|start|next|continue/i.test(b.textContent ?? ''))
  if (next && !host.querySelector('input[autocomplete="username"]')) await act(async () => next.click())
  await act(async () => {})
}

function type(el: HTMLInputElement, value: string) {
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!
  act(() => {
    set.call(el, value)
    el.dispatchEvent(new Event('input', { bubbles: true }))
  })
}

const field = (label: RegExp) => Array.from(host.querySelectorAll('label')).find((l) => label.test(l.textContent ?? ''))?.querySelector('input') as HTMLInputElement | undefined

async function fill(code: string | null) {
  if (code !== null) type(field(/setup code/i)!, code)
  type(field(/your name/i)!, 'owner')
  type(field(/^password(?! again)/i)!, 'a long enough password 1')
  type(field(/password again/i)!, 'a long enough password 1')
  const agree = host.querySelector('input[type="checkbox"]') as HTMLInputElement | null
  if (agree && !agree.checked) await act(async () => agree.click())
}

const submit = () => act(async () => host.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })))

describe('the setup wizard and the setup code', () => {
  it('asks for the code, says where it is, and sends it', async () => {
    await open({ required: true, allowed: true, code: true })
    expect(field(/setup code/i)).toBeTruthy()
    expect(host.textContent).toContain('setup-code.txt')
    await fill('k7qm-3xpd ')
    await submit()
    expect(calls.admin).toEqual([['owner', 'a long enough password 1', 'k7qm-3xpd']])
  })

  it('does not send the account without a code', async () => {
    await open({ required: true, allowed: true, code: true })
    await fill(null)
    await submit()
    expect(calls.admin).toEqual([])
    expect(host.querySelector('[role="alert"]')?.textContent).toMatch(/setup code/i)
  })

  it('shows no code field for a server that asks for none', async () => {
    await open({ required: true, allowed: true })
    expect(field(/setup code/i)).toBeUndefined()
    await fill(null)
    await submit()
    expect(calls.admin).toEqual([['owner', 'a long enough password 1', '']])
  })
})
