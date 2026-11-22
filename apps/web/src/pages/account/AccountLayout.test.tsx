// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

const who = vi.hoisted(() => ({ isAdmin: false }))
vi.mock('../../app/useSession', () => ({ useSession: () => ({ token: 't', userId: 'u', userName: 'drew', isAdmin: who.isAdmin, profileId: 'p', profileName: 'Drew' }) }))
vi.mock('../../components/TopBar', () => ({ TopBar: () => null }))
vi.mock('../../components/Footer', () => ({ Footer: () => null }))
// The pages themselves are not what is tested: each is its name.
vi.mock('./PhoneIndex', () => ({ PhoneAccountIndex: () => <p>phone index</p> }))
vi.mock('./Profiles', () => ({ ProfilesPage: () => <p>profiles page</p> }))
vi.mock('./Preferences', () => ({ PreferencesPage: () => <p>preferences page</p> }))
vi.mock('./Devices', () => ({ DevicesPage: () => <p>devices page</p> }))
vi.mock('./About', () => ({ AboutPage: () => <p>about page</p> }))
vi.mock('./Addresses', () => ({ AddressesPage: () => <p>addresses page</p> }))
vi.mock('./Password', () => ({ PasswordPage: () => <p>password page</p> }))
vi.mock('./Logout', () => ({ LogoutPage: () => <p>logout page</p> }))
vi.mock('./Requests', () => ({ RequestsPage: () => <p>requests page</p> }))
vi.mock('./Users', () => ({ UsersPage: () => <p>users page</p> }))
vi.mock('./Library', () => ({ LibraryPage: () => <p>library page</p> }))
vi.mock('./Arr', () => ({ ArrPage: () => <p>arr page</p> }))
vi.mock('./Transcoding', () => ({ TranscodingPage: () => <p>transcoding page</p> }))
vi.mock('./NowPlaying', () => ({ NowPlayingPage: () => <p>now playing page</p> }))
vi.mock('./Keys', () => ({ KeysPage: () => <p>keys page</p> }))
vi.mock('./Webhooks', () => ({ WebhooksPage: () => <p>webhooks page</p> }))

import { AccountSection } from './AccountLayout'

let host: HTMLDivElement
let root: Root
let at = ''
let moves = 0
function Where() {
  const loc = useLocation()
  at = loc.pathname
  moves++
  return null
}
beforeEach(() => {
  who.isAdmin = false
  at = ''
  moves = 0
  // jsdom has no matchMedia; the layout asks it whether this is a phone.
  window.matchMedia = ((q: string) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })) as unknown as typeof window.matchMedia
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
})
const open = async (path: string) => {
  await act(async () => {
    root.render(
      <MemoryRouter initialEntries={[path]}>
        <Where />
        <Routes>
          <Route path="/account/*" element={<AccountSection />} />
        </Routes>
      </MemoryRouter>,
    )
  })
  await act(async () => {})
}
const panel = () => host.querySelector('.acc-panel')?.textContent

// An address under /account/ that is no page (a typo, an old link, a Server page opened by
// someone who is not an admin) was sent to "profiles" RELATIVE to itself: /account/x went
// to /account/x/profiles, which is no page either, and so on for ever, the panel empty.
describe('An unknown address under /account/', () => {
  it('goes to Profiles, once', async () => {
    await open('/account/profile')
    expect(at).toBe('/account/profiles')
    expect(panel()).toBe('profiles page')
    expect(moves).toBeLessThan(5)
  })

  it('also from deeper down', async () => {
    await open('/account/some/old/link')
    expect(at).toBe('/account/profiles')
    expect(panel()).toBe('profiles page')
  })

  it('also a Server page for someone who is not an admin', async () => {
    await open('/account/requests')
    expect(at).toBe('/account/profiles')
    expect(panel()).toBe('profiles page')
  })

  it('leaves an admin on the Server page asked for', async () => {
    who.isAdmin = true
    await open('/account/requests')
    expect(at).toBe('/account/requests')
    expect(panel()).toBe('requests page')
  })

  it('still sends /account itself to Profiles', async () => {
    await open('/account')
    expect(at).toBe('/account/profiles')
  })
})

// The link column lists every page; the Server group only for an admin.
describe('The link column', () => {
  const links = () => Array.from(host.querySelectorAll('.acc-nav__link')).map((a) => a.getAttribute('href'))
  it('has a link for every Server page, Server addresses included', async () => {
    who.isAdmin = true
    await open('/account/profiles')
    expect(links()).toContain('/account/addresses')
    expect(links()).toContain('/account/now-playing')
  })
  it('has no Server links for someone who is not an admin', async () => {
    await open('/account/profiles')
    expect(links()).not.toContain('/account/addresses')
    expect(links()).not.toContain('/account/requests')
  })
  it('marks the page that is open', async () => {
    await open('/account/preferences')
    const on = Array.from(host.querySelectorAll('.acc-nav__link--on')).map((a) => a.textContent?.trim())
    expect(on).toEqual(['Preferences'])
  })
})
