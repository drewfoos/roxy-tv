import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../api/client'
import type { LoginResponse } from '../api/types'
import { session } from '../api/session'
import { WitchHat } from '../components/Icons'

type Method = 'choose' | 'phone' | 'password'
const LAST_METHOD = 'roxy.loginMethod'

function rememberMethod(m: Method) {
  try {
    localStorage.setItem(LAST_METHOD, m)
  } catch {
    /* private mode */
  }
}

function lastMethod(): Method | null {
  try {
    const m = localStorage.getItem(LAST_METHOD)
    return m === 'phone' || m === 'password' ? m : null
  } catch {
    return null
  }
}

function signIn(r: LoginResponse) {
  session.set({ token: r.token, userId: r.user.id, userName: r.user.name, isAdmin: !!r.user.isAdmin, profileId: null, profileName: '', profileHasAvatar: false, profileAvatarKey: '' })
}

/**
 * Sign in to the server this page came from. First the way in: with a phone that is
 * already signed in (a QR code and a six-letter code, the same link flow a TV uses), or
 * with a username and password. A browser cannot look for servers on the network the
 * way the apps do; the page belongs to the server it was loaded from, which it names.
 */
export function Login() {
  const [method, setMethod] = useState<Method>('choose')
  const [serverName, setServerName] = useState<string | null>(null)
  const [needsSetup, setNeedsSetup] = useState(false)
  const previous = lastMethod()

  useEffect(() => {
    api.systemInfo().then((i) => setServerName(i.name ?? null)).catch(() => setServerName(null))
    api.setupStatus().then((s) => setNeedsSetup(!!s.required)).catch(() => undefined)
  }, [])

  const pick = (m: Method) => {
    if (m !== 'choose') rememberMethod(m)
    setMethod(m)
  }

  return (
    <div className="login">
      <div className="login__box">
        <div className="login__brand">
          <WitchHat size={40} />
          <span>Roxy</span>
        </div>
        <div>
          <div className="login__server">{serverName ? `Sign in to ${serverName}` : 'Sign in'}</div>
          <div className="muted login__address">{window.location.host}</div>
        </div>
        {needsSetup ? (
          <div className="muted">This server is not set up yet. Open the Roxy app on a phone on your home network to create the first account.</div>
        ) : method === 'choose' ? (
          <div className="login__options">
            <button type="button" className={'login__option' + (previous === 'phone' ? ' login__option--last' : '')} onClick={() => pick('phone')} autoFocus={previous !== 'password'}>
              <span className="login__option-title">Use your phone</span>
              <span className="muted">Scan a QR code with the Roxy app or your camera. No typing.</span>
              {previous === 'phone' && <span className="login__option-badge">Last used</span>}
            </button>
            <button type="button" className={'login__option' + (previous === 'password' ? ' login__option--last' : '')} onClick={() => pick('password')} autoFocus={previous === 'password'}>
              <span className="login__option-title">Username and password</span>
              <span className="muted">Type the account you sign in with on the apps.</span>
              {previous === 'password' && <span className="login__option-badge">Last used</span>}
            </button>
          </div>
        ) : method === 'phone' ? (
          <PhoneSignIn onBack={() => setMethod('choose')} />
        ) : (
          <PasswordSignIn onBack={() => setMethod('choose')} />
        )}
      </div>
    </div>
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
    <div className="login__phone">
      {error ? (
        <>
          <div className="error">{error}</div>
          <button className="btn btn--pill" type="button" onClick={() => setAttempt((n) => n + 1)}>
            Try again
          </button>
        </>
      ) : link ? (
        <>
          <img className="login__qr" src={api.linkQrUrl(link.code, link.secret)} alt={`QR code for sign-in code ${shown}`} width={220} height={220} />
          <div className="login__code" aria-label="Sign-in code">
            {shown}
          </div>
          <ol className="login__steps muted">
            <li>Scan the QR code with your phone's camera, or open the Roxy app and go to Account › Sign in a TV or browser.</li>
            <li>Check the code matches, then approve.</li>
          </ol>
          <div className="muted login__waiting">Waiting for your phone…</div>
        </>
      ) : (
        <div className="muted">Getting a sign-in code…</div>
      )}
      <button className="btn btn--pill login__back" type="button" onClick={onBack}>
        Other ways to sign in
      </button>
    </div>
  )
}

function PasswordSignIn({ onBack }: { onBack: () => void }) {
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
    <form className="login__form" onSubmit={submit}>
      <div className="field">
        <label htmlFor="user">Username</label>
        <input id="user" value={name} onChange={(e) => setName(e.target.value)} autoComplete="username" autoFocus required />
      </div>
      <div className="field">
        <label htmlFor="pass">Password</label>
        <input id="pass" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />
      </div>
      {error && <div className="error">{error}</div>}
      <button className="btn btn--brand btn--pill" type="submit" disabled={busy}>
        {busy ? 'Signing in…' : 'Sign in'}
      </button>
      <button className="btn btn--pill login__back" type="button" onClick={onBack}>
        Other ways to sign in
      </button>
    </form>
  )
}
