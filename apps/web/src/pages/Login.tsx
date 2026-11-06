import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent, type ReactNode } from 'react'
import { api, ApiError, APP_VERSION } from '../api/client'
import type { LoginResponse, SetupStatus } from '../api/types'
import { session } from '../api/session'
import { WitchHat } from '../components/Icons'
import { LegalText } from '../components/LegalText'
import { SetupWizard } from './Setup'

type View = 'password' | 'phone' | 'server'

export function signIn(r: LoginResponse) {
  session.set({ token: r.token, userId: r.user.id, userName: r.user.name, isAdmin: !!r.user.isAdmin, profileId: null, profileName: '', profileHasAvatar: false, profileAvatarKey: '' })
}

// ---- Terms of Use: agreed once per terms version; recorded on the login after sign-in ----

const TERMS_KEY = 'roxy.termsAccepted'

/** The terms version this browser agreed to, if any. */
function termsAcceptedHere(): string | null {
  try {
    return localStorage.getItem(TERMS_KEY)
  } catch {
    return null
  }
}

/** Record the agreement on the server (the same record the apps keep) and in this browser. */
export async function recordTerms(token: string, known: string | null) {
  try {
    const version = known ?? (await api.legal('terms')).version
    if (!version) return
    await fetch('/v1/legal/accept', { method: 'POST', headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' }, body: JSON.stringify({ version }) })
    localStorage.setItem(TERMS_KEY, version)
  } catch {
    /* not fatal: the apps ask again */
  }
}

/** What the sign-in forms need to know about the terms. */
export interface Terms {
  version: string | null
  /** This browser hasn't agreed to this version yet: the checkbox shows and must be ticked. */
  needed: boolean
  agreed: boolean
  setAgreed: (v: boolean) => void
  show: (doc: 'terms' | 'privacy') => void
}

const MUST_AGREE = 'Please agree to the Terms of Use and Privacy Policy to continue.'

/** A sign-in error in words a person can act on. */
function friendly(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 401) return "That username and password don't match. Check both and try again."
    if (err.status === 429) return err.message // the server says how long to wait
    if (err.status >= 500) return 'The server had a problem signing you in. Try again in a moment.'
    return err.message
  }
  if (err instanceof TypeError) return "Can't reach the server. Check it's switched on and that you're on the same network."
  return err instanceof Error ? err.message : 'Sign-in failed'
}

// ---- servers this browser has used (for "Change server") ----

interface KnownServer {
  origin: string
  name: string
}
const RECENT_KEY = 'roxy.recentServers'

function recentServers(): KnownServer[] {
  try {
    const list = JSON.parse(localStorage.getItem(RECENT_KEY) ?? '[]') as KnownServer[]
    return Array.isArray(list) ? list.filter((s) => typeof s.origin === 'string') : []
  } catch {
    return []
  }
}

function rememberServer(s: KnownServer) {
  try {
    const list = [s, ...recentServers().filter((x) => x.origin !== s.origin)].slice(0, 6)
    localStorage.setItem(RECENT_KEY, JSON.stringify(list))
  } catch {
    /* private mode */
  }
}

/** Candidate origins for what someone typed: "192.168.1.5", "roxy.local:8097", "http://…". */
function candidates(input: string): string[] {
  let t = input.trim().replace(/\/+$/, '')
  if (!t) return []
  const hasScheme = /^https?:\/\//i.test(t)
  if (!hasScheme) t = 'http://' + t
  let u: URL
  try {
    u = new URL(t)
  } catch {
    return []
  }
  if (u.port || hasScheme) return [u.origin]
  // No port given: Roxy's own port first, then the one a Docker setup often maps it to.
  return [`${u.protocol}//${u.hostname}:8096`, `${u.protocol}//${u.hostname}:8097`, u.origin]
}

/** Is there a Roxy server at origin? Its name, or null. */
async function probe(origin: string): Promise<string | null> {
  const ctl = new AbortController()
  const t = setTimeout(() => ctl.abort(), 4000)
  try {
    const r = await fetch(origin + '/v1/system/info', { signal: ctl.signal })
    if (!r.ok) return null
    const info = (await r.json()) as { product?: string; name?: string }
    return info.product === 'roxy' ? info.name || 'Roxy' : null
  } catch {
    return null
  } finally {
    clearTimeout(t)
  }
}

/**
 * Sign in to the server this page came from. One card over a dimmed wall of the server's
 * own posters: username and password first, "Sign in with your phone" one button away
 * (a QR code, a short address and a six-letter code, approved from a signed-in phone),
 * and the server it signs in to named on the card with a way to change it. Built to
 * leave nobody stuck: every error says what to do, and each way in shows a way out.
 */
export function Login() {
  const [view, setView] = useState<View>('password')
  const [serverName, setServerName] = useState<string | null>(null)
  const [setup, setSetup] = useState<SetupStatus | null>(null)
  const [termsVersion, setTermsVersion] = useState<string | null>(null)
  const [agreed, setAgreed] = useState(false)
  const [doc, setDoc] = useState<'terms' | 'privacy' | null>(null)

  useEffect(() => {
    api
      .systemInfo()
      .then((i) => {
        const name = i.name ?? 'Roxy'
        setServerName(name)
        rememberServer({ origin: window.location.origin, name })
      })
      .catch(() => setServerName(null))
    api.setupStatus().then(setSetup).catch(() => undefined)
    api.legal('terms').then((d) => setTermsVersion(d.version)).catch(() => setTermsVersion(null))
  }, [])

  // Decided at first paint from this browser, so the form never jumps when the server
  // answers: agreed before -> small print; never -> the checkbox. The server's version
  // only changes that when the terms themselves changed.
  const acceptedHere = termsAcceptedHere()
  const needed = acceptedHere == null || (termsVersion != null && termsVersion !== acceptedHere)
  const terms: Terms = { version: termsVersion, needed, agreed, setAgreed, show: setDoc }

  // A brand-new server: the setup wizard instead of a sign-in nobody can use yet.
  if (setup?.required && view !== 'server') return <SetupWizard status={setup} onCancel={() => setView('server')} />

  return (
    <div className="signin">
      <PosterWall />
      <main className="signin__center">
        <div className="signin__logo">
          <WitchHat size={40} />
          <span>
            Roxy <b>TV</b>
          </span>
        </div>
        <div className="signin__card">
          {view === 'server' ? (
            <ChangeServer onBack={() => setView('password')} />
          ) : (
            view === 'phone' ? (
              <PhoneSignIn terms={terms} onBack={() => setView('password')} />
            ) : (
              <PasswordSignIn terms={terms} onPhone={() => setView('phone')} />
            )
          )}
        </div>
        {view === 'password' && <Help />}
        <div className="signin__foot">
          {view !== 'server' && (
            <span>
              Signing in to <b>{serverName ?? 'Roxy'}</b> · {window.location.host} ·{' '}
              <button type="button" className="signin__foot-link" onClick={() => setView('server')}>
                Different server?
              </button>
            </span>
          )}
          <span>Roxy TV · web {APP_VERSION}</span>
        </div>
      </main>
      {doc && <LegalDialog doc={doc} onClose={() => setDoc(null)} />}
    </div>
  )
}

function PasswordSignIn({ terms, onPhone }: { terms: Terms; onPhone: () => void }) {
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [show, setShow] = useState(false)
  const [caps, setCaps] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const passRef = useRef<HTMLInputElement>(null)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (busy) return
    if (!name.trim() || !password) {
      setError(!name.trim() ? 'Enter your username.' : 'Enter your password.')
      return
    }
    if (terms.needed && !terms.agreed) {
      setError(MUST_AGREE)
      return
    }
    setBusy(true)
    setError(null)
    try {
      const r = await api.login(name.trim(), password)
      if (terms.needed) await recordTerms(r.token, terms.version)
      signIn(r)
    } catch (err) {
      setError(friendly(err))
      setPassword('')
      passRef.current?.focus()
    } finally {
      setBusy(false)
    }
  }
  const capsCheck = (e: KeyboardEvent<HTMLInputElement>) => setCaps(e.getModifierState?.('CapsLock') ?? false)

  return (
    <>
      <h1 className="signin__title">Sign in</h1>
      <form className="signin__form" onSubmit={submit} noValidate>
        <label className="signin__field">
          <span>Username</span>
          <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="username" autoCapitalize="none" spellCheck={false} autoFocus />
        </label>
        <label className="signin__field">
          <span>Password</span>
          <span className="signin__password">
            <input ref={passRef} type={show ? 'text' : 'password'} value={password} onChange={(e) => setPassword(e.target.value)} onKeyUp={capsCheck} onKeyDown={capsCheck} autoComplete="current-password" />
            <button type="button" className="signin__eye" onClick={() => setShow((v) => !v)} aria-label={show ? 'Hide password' : 'Show password'} aria-pressed={show}>
              {show ? <IconEyeOff /> : <IconEye />}
            </button>
          </span>
        </label>
        {caps && <div className="signin__hint">Caps Lock is on.</div>}
        <TermsLine terms={terms} />
        {error && (
          <div className="signin__error" role="alert">
            {error}
          </div>
        )}
        <button className="signin__primary" type="submit" disabled={busy}>
          {busy ? <span className="signin__spinner" aria-hidden="true" /> : null}
          {busy ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
      <div className="signin__or">
        <span>or</span>
      </div>
      <button
        type="button"
        className="signin__secondary"
        onClick={() => {
          if (terms.needed && !terms.agreed) setError(MUST_AGREE)
          else onPhone()
        }}
      >
        <IconPhone /> Sign in with your phone
      </button>
    </>
  )
}

/** The agreement: a checkbox until this browser has agreed to this version, then one line. */
export function TermsLine({ terms }: { terms: Terms }) {
  const links = (
    <>
      the{' '}
      <button type="button" className="signin__inline-link" onClick={() => terms.show('terms')}>
        Terms of Use
      </button>{' '}
      and{' '}
      <span className="signin__nowrap">
        <button type="button" className="signin__inline-link" onClick={() => terms.show('privacy')}>
          Privacy Policy
        </button>
        .
      </span>
    </>
  )
  if (!terms.needed) return <p className="signin__fine">By signing in you agree to {links}</p>
  return (
    <label className="signin__check">
      <input type="checkbox" checked={terms.agreed} onChange={(e) => terms.setAgreed(e.target.checked)} />
      <span className="signin__check-box" aria-hidden="true">
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
          <path d="m5 12 5 5 9-10" />
        </svg>
      </span>
      <span>I agree to {links}</span>
    </label>
  )
}

/** The Terms of Use or Privacy Policy over the sign-in page (those pages need a session). */
export function LegalDialog({ doc, onClose }: { doc: 'terms' | 'privacy'; onClose: () => void }) {
  const [data, setData] = useState<{ title: string; body: string } | null>(null)
  const [failed, setFailed] = useState(false)
  useEffect(() => {
    api
      .legal(doc)
      .then((d) => setData({ title: d.title, body: d.body }))
      .catch(() => setFailed(true))
  }, [doc])
  useEffect(() => {
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])
  return (
    <div className="signin__dialog-scrim" onClick={onClose}>
      <div className="signin__dialog" role="dialog" aria-modal="true" aria-label={data?.title ?? 'Terms'} onClick={(e) => e.stopPropagation()}>
        <div className="signin__dialog-head">
          <h2>{data?.title ?? (doc === 'privacy' ? 'Privacy Policy' : 'Terms of Use')}</h2>
          <button type="button" className="signin__dialog-close" onClick={onClose} aria-label="Close">
            ×
          </button>
        </div>
        <div className="signin__dialog-body">
          {failed ? <p className="signin__error">This document didn't load. Try again in a moment.</p> : data ? <LegalText body={data.body} /> : <p className="signin__muted">Loading…</p>}
        </div>
        <div className="signin__dialog-foot">
          <button type="button" className="signin__primary" onClick={onClose}>
            Done
          </button>
        </div>
      </div>
    </div>
  )
}

/** Below the card: what to do when the usual ways fail. */
function Help() {
  const [open, setOpen] = useState(false)
  return (
    <div className="signin__help">
      <button type="button" className="signin__help-toggle" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
        Trouble signing in?
      </button>
      {open && (
        <ul>
          <li>
            <b>Forgot your password?</b> Whoever runs this Roxy server can set a new one in Account › Users.
          </li>
          <li>
            <b>Signed in on your phone?</b> Use <i>Sign in with your phone</i>: no password needed.
          </li>
          <li>
            <b>Wrong server?</b> Use <i>Different server?</i> below and type the right address.
          </li>
          <li>
            <b>Page won't load at all?</b> The server may be off, or you may be outside your home network.
          </li>
        </ul>
      )}
    </div>
  )
}

/** The QR / code sign-in: ask for a link code, show it three ways, wait for a phone to approve it. */
function PhoneSignIn({ terms, onBack }: { terms: Terms; onBack: () => void }) {
  const [link, setLink] = useState<{ code: string; secret: string; expiresAt: number } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [attempt, setAttempt] = useState(0)
  const [now, setNow] = useState(Date.now())

  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(t)
  }, [])

  useEffect(() => {
    let stop = false
    setLink(null)
    setError(null)
    ;(async () => {
      try {
        const l = await api.linkStart()
        if (stop) return
        const expiresAt = Date.now() + l.expiresIn * 1000
        setLink({ code: l.code, secret: l.secret, expiresAt })
        // Long-poll until the phone approves or the code runs out (then a fresh code).
        while (!stop && Date.now() < expiresAt) {
          try {
            const r = await api.linkWait(l.code, l.secret)
            if (stop) return
            if (r.status === 'done') {
              if (terms.needed) await recordTerms(r.token, terms.version)
              signIn(r)
              return
            }
          } catch (e) {
            if (stop) return
            if (e instanceof ApiError && e.status === 404) break // gone (expired, server restarted)
            await new Promise((res) => setTimeout(res, 3000))
          }
        }
        if (!stop) setAttempt((n) => n + 1)
      } catch (e) {
        if (!stop) setError(friendly(e))
      }
    })()
    return () => {
      stop = true
    }
  }, [attempt])

  const shown = link ? link.code.slice(0, 3) + '-' + link.code.slice(3) : ''
  const left = link ? Math.max(0, Math.round((link.expiresAt - now) / 1000)) : 0
  const linkAddress = `${window.location.host}/link`
  return (
    <>
      <BackButton onClick={onBack}>Use username and password</BackButton>
      <h1 className="signin__title">Sign in with your phone</h1>
      <p className="signin__lead">Use a phone that's already signed in to Roxy.</p>
      {error ? (
        <>
          <div className="signin__error" role="alert">
            {error}
          </div>
          <button className="signin__primary" type="button" onClick={() => setAttempt((n) => n + 1)}>
            Try again
          </button>
        </>
      ) : (
        <div className="signin__phone">
          <div className="signin__qr">
            {link ? <img src={api.linkQrUrl(link.code, link.secret)} alt={`QR code for sign-in code ${shown}`} width={168} height={168} /> : <div className="signin__qr-wait" />}
          </div>
          <ol className="signin__steps">
            <li>
              <b>Point your phone's camera at the code</b> and open the link.
            </li>
            <li>
              <b>Can't scan?</b> On your phone, go to <span className="signin__mono">{linkAddress}</span> and type
              <span className="signin__code">{shown || '······'}</span>
            </li>
            <li>
              <b>In the Roxy app:</b> Account › Sign in a TV or browser.
            </li>
          </ol>
          <div className="signin__waiting">
            <span className="signin__spinner signin__spinner--accent" aria-hidden="true" />
            {link ? `Waiting for your phone · new code in ${Math.floor(left / 60)}:${String(left % 60).padStart(2, '0')}` : 'Getting a code…'}
            {link && (
              <button type="button" className="signin__link" onClick={() => setAttempt((n) => n + 1)}>
                New code
              </button>
            )}
          </div>
        </div>
      )}
    </>
  )
}

/** Sign in somewhere else: check an address is a Roxy server, then go there. */
function ChangeServer({ onBack }: { onBack: () => void }) {
  const [address, setAddress] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const recent = recentServers().filter((s) => s.origin !== window.location.origin)

  const go = (origin: string, name: string) => {
    rememberServer({ origin, name })
    window.location.href = origin + '/'
  }

  const connect = async (e: FormEvent) => {
    e.preventDefault()
    const list = candidates(address)
    if (list.length === 0) {
      setError('Type the address of a Roxy server, like 192.168.1.50:8096.')
      return
    }
    setBusy(true)
    setError(null)
    for (const origin of list) {
      if (origin === window.location.origin) {
        setBusy(false)
        onBack() // that's this one
        return
      }
      const name = await probe(origin)
      if (name) {
        go(origin, name)
        return
      }
    }
    setBusy(false)
    if (window.location.protocol === 'https:' && list.every((o) => o.startsWith('http:'))) {
      setError(`This page is on https, so the browser won't connect to an http address from here. Open ${list[0]} directly instead.`)
    } else {
      setError(`No Roxy server answered at ${address.trim()}. Check the address and port, that the server is on, and that you're on the same network.`)
    }
  }

  return (
    <>
      <BackButton onClick={onBack}>Back to sign in</BackButton>
      <h1 className="signin__title">Change server</h1>
      <p className="signin__lead">Enter the address of the Roxy server you want. It's in the Roxy app under Account › Server, or ask whoever runs it.</p>
      <form className="signin__form" onSubmit={connect} noValidate>
        <label className="signin__field">
          <span>Server address</span>
          <input value={address} onChange={(e) => setAddress(e.target.value)} placeholder="192.168.1.50:8096" autoComplete="url" autoCapitalize="none" spellCheck={false} inputMode="url" autoFocus />
        </label>
        {error && (
          <div className="signin__error" role="alert">
            {error}
          </div>
        )}
        <button className="signin__primary" type="submit" disabled={busy || !address.trim()}>
          {busy ? <span className="signin__spinner" aria-hidden="true" /> : null}
          {busy ? 'Looking for the server…' : 'Connect'}
        </button>
      </form>
      {recent.length > 0 && (
        <div className="signin__recent">
          <div className="signin__recent-title">Recent servers</div>
          {recent.map((s) => (
            <button key={s.origin} type="button" className="signin__recent-item" onClick={() => go(s.origin, s.name)}>
              <IconServer />
              <span className="signin__server-text">
                <span className="signin__server-name">{s.name}</span>
                <span className="signin__muted">{s.origin.replace(/^https?:\/\//, '')}</span>
              </span>
              <IconChevron />
            </button>
          ))}
        </div>
      )}
    </>
  )
}

function BackButton({ onClick, children }: { onClick: () => void; children: ReactNode }) {
  return (
    <button type="button" className="signin__back" onClick={onClick}>
      <IconChevron left /> {children}
    </button>
  )
}

/**
 * The background: slowly drifting, tilted columns of this server's posters, dimmed
 * (Netflix and Crunchyroll sign in over their own catalogue). The server hands out
 * posters by position only (no titles). With too few, just Roxy's azure glow.
 */
function PosterWall() {
  const [count, setCount] = useState(0)
  useEffect(() => {
    fetch('/v1/showcase')
      .then((r) => (r.ok ? r.json() : { count: 0 }))
      .then((d: { count?: number }) => setCount(d.count ?? 0))
      .catch(() => setCount(0))
  }, [])
  if (count < 4) return <div className="signin__wall signin__wall--empty" aria-hidden="true" />
  const columns = 7
  const all = Array.from({ length: count }, (_, i) => i)
  // A big library splits its posters between the columns; a small one runs every poster
  // down every column, each column starting further along so neighbours don't line up.
  const cols =
    count >= 42
      ? Array.from({ length: columns }, (_, c) => all.filter((i) => i % columns === c))
      : Array.from({ length: columns }, (_, c) => all.map((_, i) => all[(i + c * 3) % count]))
  return (
    <div className="signin__wall" aria-hidden="true">
      <div className="signin__wall-grid">
        {cols.map((col, c) => (
          <div key={c} className="signin__wall-col" style={{ animationDuration: `${90 + c * 9}s`, animationDirection: c % 2 ? 'reverse' : 'normal' }}>
            {/* Twice over, so the drift loops without a seam. */}
            {[...col, ...col].map((i, k) => (
              <img key={k} src={`/v1/showcase/${i}`} alt="" loading="lazy" decoding="async" />
            ))}
          </div>
        ))}
      </div>
      <div className="signin__wall-shade" />
    </div>
  )
}

// ---- small line icons (24 px grid, currentColor) ----

const svg = { width: 20, height: 20, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.8, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const, 'aria-hidden': true }

function IconPhone() {
  return (
    <svg {...svg}>
      <rect x="6" y="2.5" width="12" height="19" rx="2.5" />
      <path d="M10.5 18.5h3" />
    </svg>
  )
}

function IconServer() {
  return (
    <svg {...svg}>
      <rect x="3" y="4" width="18" height="7" rx="2" />
      <rect x="3" y="13" width="18" height="7" rx="2" />
      <path d="M7 7.5h.01M7 16.5h.01" />
    </svg>
  )
}

export function IconEye() {
  return (
    <svg {...svg}>
      <path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12Z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  )
}

export function IconEyeOff() {
  return (
    <svg {...svg}>
      <path d="M3 3l18 18" />
      <path d="M10.6 5.6A9.7 9.7 0 0 1 12 5.5c6 0 9.5 6.5 9.5 6.5a17 17 0 0 1-3.2 4M6.4 7.4C3.9 9.2 2.5 12 2.5 12S6 18.5 12 18.5c1.6 0 3-.4 4.2-1" />
      <path d="M9.9 9.9a3 3 0 0 0 4.2 4.2" />
    </svg>
  )
}

function IconChevron({ left }: { left?: boolean }) {
  return (
    <svg {...svg} width={18} height={18} style={left ? { transform: 'rotate(180deg)' } : undefined}>
      <path d="m9 5 7 7-7 7" />
    </svg>
  )
}
