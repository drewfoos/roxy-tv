import { useEffect, useState, type FormEvent } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router'
import { api } from '../api/client'
import { useSession } from '../app/useSession'
import { WitchHat } from '../components/Icons'

/**
 * /link/:code — the page a TV's QR code opens. Signed in here, you see which device is
 * asking and approve it; the TV, waiting on the code, is then signed in as you.
 *
 * A code in a URL proves nothing: anyone can start a sign-in and send the link to you
 * ("click to watch!"), and approving it signs THEIR device in as you. So the page does
 * not show the code from the link: you type the one the TV in front of you shows, and only
 * a match enables Approve. Coming from /link (where you just typed it) skips the retype.
 */
const normCode = (c: string) => c.toUpperCase().replace(/[^A-Z0-9]/g, '')

export function LinkTv() {
  const { code = '' } = useParams()
  const typedOnEnter = (useLocation().state as { typed?: string } | null)?.typed ?? ''
  const s = useSession()
  const [info, setInfo] = useState<{ client: string; device: string; approved: boolean; network?: 'home' | 'tailscale' | 'internet' } | null>(null)
  const [typed, setTyped] = useState(typedOnEnter)
  const matches = normCode(typed) !== '' && normCode(typed) === normCode(code)
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    let live = true
    api
      .linkInfo(code)
      .then((i) => live && setInfo(i))
      .catch((e: unknown) => live && setError(e instanceof Error ? e.message : 'This code is not valid'))
    return () => {
      live = false
    }
  }, [code])

  const approve = async () => {
    if (busy || !matches) return
    setBusy(true)
    setError(null)
    try {
      await api.linkApprove(code)
      setDone(true)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not sign the device in')
    } finally {
      setBusy(false)
    }
  }

  const pretty = normCode(code).replace(/(.{3})(.{3})/, '$1 $2')
  // The same flow signs in a TV or a web browser; say which one is asking.
  const kind = info?.client === 'roxy-web' ? 'browser' : 'TV'
  return (
    <div className="login">
      <div className="login__box linktv">
        <div className="login__brand">
          <WitchHat size={40} />
          <span>Roxy</span>
        </div>
        {done ? (
          <>
            <h2 className="linktv__title">Done</h2>
            <p className="muted">
              {info?.device ?? `The ${kind}`} is signing in as <b>{s?.userName}</b>. Pick a profile on the {kind}.
            </p>
            <Link className="btn btn--grey btn--pill" to="/">
              Back to Roxy
            </Link>
          </>
        ) : error ? (
          <>
            <h2 className="linktv__title">Sign in a {kind}</h2>
            <p className="error">{error}</p>
            <Link className="btn btn--grey btn--pill" to="/">
              Back to Roxy
            </Link>
          </>
        ) : (
          <>
            <h2 className="linktv__title">Sign in a {kind}</h2>
            {matches && <div className="linktv__code">{pretty}</div>}
            <p className="muted">
              {info ? (
                <>
                  <b>{info.device}</b> ({info.client}) wants to sign in as <b>{s?.userName}</b>.
                </>
              ) : (
                'Checking the code…'
              )}
            </p>
            {info?.network === 'internet' && (
              <div className="notice notice--error linktv__warn" role="alert">
                <b>This {kind} is not on your home network.</b> It is asking from the internet. If someone sent you this link or
                asked you to approve it, do not: approving signs their device in to your account.
              </div>
            )}
            {info?.network === 'tailscale' && <p className="muted">The {kind} is connecting over Tailscale.</p>}
            {info?.network === 'home' && <p className="muted">The {kind} is on your home network.</p>}
            <p className="linktv__only">
              <b>Only approve a {kind} you are in front of.</b>
            </p>
            {!info?.approved && (
              <div className="field">
                <label htmlFor="confirm-code">Type the code the {kind} shows</label>
                <input
                  id="confirm-code"
                  value={typed}
                  onChange={(e) => setTyped(e.target.value)}
                  autoCapitalize="characters"
                  autoComplete="off"
                  spellCheck={false}
                  maxLength={7}
                  aria-invalid={normCode(typed).length >= normCode(code).length && !matches}
                />
                {normCode(typed).length >= normCode(code).length && !matches && <span className="error">That is not the code for this sign-in.</span>}
              </div>
            )}
            <button className="btn btn--brand btn--pill" type="button" onClick={approve} disabled={busy || !info || info.approved || !matches}>
              {busy ? 'Signing in…' : info?.approved ? 'Already approved' : `Sign in this ${kind}`}
            </button>
            <Link className="muted linktv__cancel" to="/">
              Not now
            </Link>
          </>
        )}
      </div>
    </div>
  )
}

/** /link — type the code the TV shows (for a phone that cannot scan). */
export function LinkEnter() {
  const [code, setCode] = useState('')
  const nav = useNavigate()
  const submit = (e: FormEvent) => {
    e.preventDefault()
    const c = code.toUpperCase().replace(/[^A-Z0-9]/g, '')
    // The code was just typed from the TV: the approve page need not ask again.
    if (c.length >= 6) nav('/link/' + c, { state: { typed: c } })
  }
  return (
    <div className="login">
      <form className="login__box linktv" onSubmit={submit}>
        <div className="login__brand">
          <WitchHat size={40} />
          <span>Roxy</span>
        </div>
        <h2 className="linktv__title">Sign in a TV or browser</h2>
        <p className="muted">Enter the code shown on its sign-in screen.</p>
        <div className="field">
          <label htmlFor="code">Code</label>
          <input id="code" value={code} onChange={(e) => setCode(e.target.value)} autoCapitalize="characters" autoComplete="off" spellCheck={false} maxLength={7} autoFocus required />
        </div>
        <button className="btn btn--brand btn--pill" type="submit">
          Continue
        </button>
      </form>
    </div>
  )
}
