import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { api, APP_VERSION } from '../api/client'
import type { LoginResponse } from '../api/types'
import { session } from '../api/session'
import { WitchHat } from '../components/Icons'

function signIn(r: LoginResponse) {
  session.set({ token: r.token, userId: r.user.id, userName: r.user.name, isAdmin: !!r.user.isAdmin, profileId: null, profileName: '', profileHasAvatar: false, profileAvatarKey: '' })
}

/**
 * Sign in to the server this page came from: the brand panel on the left with a wavy
 * edge, the sign-in on the right (one column on a phone). Username and password first,
 * with "Sign in with your phone" one button away: a QR code and a six-letter code that a
 * signed-in phone approves (the same link flow a TV uses). A browser cannot look for
 * servers on the network the way the apps do; the page belongs to the server it was
 * loaded from, which it names.
 */
export function Login() {
  const [phone, setPhone] = useState(false)
  const [serverName, setServerName] = useState<string | null>(null)
  const [needsSetup, setNeedsSetup] = useState(false)

  useEffect(() => {
    api.systemInfo().then((i) => setServerName(i.name ?? null)).catch(() => setServerName(null))
    api.setupStatus().then((s) => setNeedsSetup(!!s.required)).catch(() => undefined)
  }, [])

  return (
    <div className="auth">
      <aside className="auth__brand" aria-label="Roxy TV">
        <BrandArt />
        <div className="auth__logo">
          <WitchHat size={36} />
          <span>
            Roxy <b>TV</b>
          </span>
        </div>
        <div className="auth__pitch">
          <h1>Everything you watch, in one place.</h1>
          <p>Your anime, shows and movies, streamed from your own server to your TV, phone and browser.</p>
          <ul className="auth__features">
            <Feature icon={<IconSubtitles />} title="Subtitles as released" text="Styled anime typesetting, signs and songs, every font." />
            <Feature icon={<IconResume />} title="Pick up anywhere" text="Start on the TV, finish on your phone." />
            <Feature icon={<IconRequest />} title="Ask for something new" text="Request a show and watch it arrive." />
          </ul>
        </div>
        <div className="auth__server">
          <span className="auth__dot" aria-hidden="true" />
          <span>
            {serverName ? <b>{serverName}</b> : 'Roxy server'} <span className="auth__muted">· {window.location.host}</span>
          </span>
        </div>
        <svg className="auth__wave" viewBox="0 0 100 1000" preserveAspectRatio="none" aria-hidden="true">
          <path d="M100 0 H62 C18 140 88 270 50 410 C14 545 80 660 46 800 C30 870 52 940 70 1000 H100 Z" />
        </svg>
      </aside>

      <main className="auth__panel">
        <div className="auth__inner">
          <div className="auth__mobile-logo">
            <WitchHat size={32} />
            <span>
              Roxy <b>TV</b>
            </span>
          </div>
          {needsSetup ? (
            <>
              <h2 className="auth__title">Almost there</h2>
              <p className="auth__lead">This server isn't set up yet. Open the Roxy app on a phone on your home network to create the first account.</p>
            </>
          ) : phone ? (
            <PhoneSignIn onBack={() => setPhone(false)} />
          ) : (
            <PasswordSignIn serverName={serverName} onPhone={() => setPhone(true)} />
          )}
          <div className="auth__foot">Roxy TV · web {APP_VERSION}</div>
        </div>
      </main>
    </div>
  )
}

/** Flowing bands of light in Roxy's azure, echoing the wavy edge. Pure decoration. */
function BrandArt() {
  return (
    <svg className="auth__art" viewBox="0 0 800 900" preserveAspectRatio="xMidYMid slice" aria-hidden="true">
      <defs>
        <linearGradient id="auth-band-a" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" stopColor="#3ea6ff" stopOpacity="0.55" />
          <stop offset="1" stopColor="#3ea6ff" stopOpacity="0" />
        </linearGradient>
        <linearGradient id="auth-band-b" x1="1" y1="0" x2="0" y2="1">
          <stop offset="0" stopColor="#7cc4ff" stopOpacity="0.35" />
          <stop offset="1" stopColor="#1f6fd1" stopOpacity="0" />
        </linearGradient>
        <radialGradient id="auth-glow" cx="0.78" cy="0.12" r="0.6">
          <stop offset="0" stopColor="#3ea6ff" stopOpacity="0.45" />
          <stop offset="1" stopColor="#3ea6ff" stopOpacity="0" />
        </radialGradient>
        <filter id="auth-soft" x="-20%" y="-20%" width="140%" height="140%">
          <feGaussianBlur stdDeviation="18" />
        </filter>
      </defs>
      <rect width="800" height="900" fill="url(#auth-glow)" />
      <g transform="translate(0 150)">
      <g filter="url(#auth-soft)">
        <path d="M-60 640 C140 520 300 760 520 600 S820 470 900 560 L900 700 C760 640 640 800 470 760 S120 700 -60 800 Z" fill="url(#auth-band-a)" />
        <path d="M-80 760 C120 690 260 860 460 790 S760 650 900 720 L900 820 C720 790 600 930 420 880 S80 840 -80 900 Z" fill="url(#auth-band-b)" />
      </g>
      <path d="M-40 590 C160 470 320 720 540 560 S830 440 900 520" fill="none" stroke="#7cc4ff" strokeOpacity="0.35" strokeWidth="1.5" />
      <path d="M-40 650 C170 560 330 780 560 640 S840 520 900 600" fill="none" stroke="#3ea6ff" strokeOpacity="0.25" strokeWidth="1" />
      </g>
    </svg>
  )
}

function Feature({ icon, title, text }: { icon: ReactNode; title: string; text: string }) {
  return (
    <li className="auth__feature">
      <span className="auth__feature-icon">{icon}</span>
      <span>
        <b>{title}</b>
        <span className="auth__muted">{text}</span>
      </span>
    </li>
  )
}

function PasswordSignIn({ serverName, onPhone }: { serverName: string | null; onPhone: () => void }) {
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      signIn(await api.login(name.trim(), password))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Sign-in failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <h2 className="auth__title">Welcome back</h2>
      <p className="auth__lead">{serverName ? `Sign in to ${serverName}.` : 'Sign in with your Roxy account.'}</p>
      <form className="auth__form" onSubmit={submit}>
        <label className="auth__field">
          <span>Username</span>
          <input value={name} onChange={(e) => setName(e.target.value)} autoComplete="username" autoFocus required />
        </label>
        <label className="auth__field">
          <span>Password</span>
          <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />
        </label>
        {error && <div className="auth__error">{error}</div>}
        <button className="auth__submit" type="submit" disabled={busy}>
          {busy ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
      <div className="auth__or">
        <span>or</span>
      </div>
      <button type="button" className="auth__alt" onClick={onPhone}>
        <IconPhone /> Sign in with your phone
      </button>
    </>
  )
}

/** The QR / code sign-in: ask for a link code, show it, and wait for a phone to approve it. */
function PhoneSignIn({ onBack }: { onBack: () => void }) {
  const [link, setLink] = useState<{ code: string; secret: string; url: string } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [attempt, setAttempt] = useState(0)

  useEffect(() => {
    let stop = false
    setLink(null)
    setError(null)
    ;(async () => {
      try {
        const l = await api.linkStart()
        if (stop) return
        setLink(l)
        const until = Date.now() + l.expiresIn * 1000
        // Long-poll until the phone approves or the code runs out (then a fresh code).
        while (!stop && Date.now() < until) {
          try {
            const r = await api.linkWait(l.code, l.secret)
            if (stop) return
            if (r.status === 'done') {
              signIn(r)
              return
            }
          } catch (e) {
            if (stop) return
            // The code is gone (expired, server restarted): start over with a new one.
            if (e instanceof Error && /not found|expired|404/i.test(e.message)) break
            await new Promise((res) => setTimeout(res, 3000))
          }
        }
        if (!stop) setAttempt((n) => n + 1)
      } catch (e) {
        if (!stop) setError(e instanceof Error ? e.message : 'Could not get a sign-in code')
      }
    })()
    return () => {
      stop = true
    }
  }, [attempt])

  const shown = link ? link.code.slice(0, 3) + '-' + link.code.slice(3) : ''
  return (
    <>
      <button type="button" className="auth__back" onClick={onBack}>
        <IconChevron left /> Use username and password
      </button>
      <h2 className="auth__title">Sign in with your phone</h2>
      <p className="auth__lead">Approve this browser from a phone that's already signed in.</p>
      {error ? (
        <>
          <div className="auth__error">{error}</div>
          <button className="auth__submit" type="button" onClick={() => setAttempt((n) => n + 1)}>
            Try again
          </button>
        </>
      ) : (
        <div className="auth__link">
          <div className="auth__qr">{link ? <img src={api.linkQrUrl(link.code, link.secret)} alt={`QR code for sign-in code ${shown}`} width={176} height={176} /> : <div className="auth__qr-wait" />}</div>
          <div className="auth__code" aria-label="Sign-in code">
            {shown || '······'}
          </div>
          <div className="auth__waiting">
            <span className="auth__spinner" aria-hidden="true" />
            {link ? 'Waiting for your phone…' : 'Getting a code…'}
          </div>
          <ol className="auth__steps">
            <li>Scan the QR code with your phone's camera.</li>
            <li>
              Or in the Roxy app open <b>Account › Sign in a TV or browser</b> and enter the code.
            </li>
          </ol>
        </div>
      )}
    </>
  )
}

// ---- small line icons (24 px grid, currentColor) ----

const svg = { width: 22, height: 22, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.8, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const }

function IconPhone() {
  return (
    <svg {...svg} width={20} height={20} aria-hidden="true">
      <rect x="6" y="2.5" width="12" height="19" rx="2.5" />
      <path d="M10.5 18.5h3" />
    </svg>
  )
}

function IconSubtitles() {
  return (
    <svg {...svg}>
      <rect x="2.5" y="5" width="19" height="14" rx="2.5" />
      <path d="M6.5 15h4M13 15h4.5M6.5 11.5h7M15.5 11.5h2" />
    </svg>
  )
}

function IconResume() {
  return (
    <svg {...svg}>
      <path d="M3.5 12a8.5 8.5 0 1 0 2.5-6" />
      <path d="M3.5 4v4h4" />
      <path d="m10.5 9 4.5 3-4.5 3z" />
    </svg>
  )
}

function IconRequest() {
  return (
    <svg {...svg}>
      <path d="M12 3.5v11M7.5 10l4.5 4.5 4.5-4.5" />
      <path d="M4 16.5V19a1.5 1.5 0 0 0 1.5 1.5h13A1.5 1.5 0 0 0 20 19v-2.5" />
    </svg>
  )
}

function IconChevron({ left }: { left?: boolean }) {
  return (
    <svg {...svg} width={18} height={18} style={left ? { transform: 'rotate(180deg)' } : undefined} aria-hidden="true">
      <path d="m9 5 7 7-7 7" />
    </svg>
  )
}
