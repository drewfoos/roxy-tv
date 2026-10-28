import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { api } from '../api/client'
import { useSession } from '../app/useSession'
import { WitchHat } from '../components/Icons'

/**
 * /link/:code — the page a TV's QR code opens. Signed in here, you see which device is
 * asking and approve it; the TV, waiting on the code, is then signed in as you.
 */
export function LinkTv() {
  const { code = '' } = useParams()
  const s = useSession()
  const [info, setInfo] = useState<{ client: string; device: string; approved: boolean } | null>(null)
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
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      await api.linkApprove(code)
      setDone(true)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not sign the TV in')
    } finally {
      setBusy(false)
    }
  }

  const pretty = code.toUpperCase().replace(/(.{3})(.{3})/, '$1 $2')
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
              {info?.device ?? 'The TV'} is signing in as <b>{s?.userName}</b>. Pick a profile on the TV.
            </p>
            <Link className="btn btn--grey btn--pill" to="/">
              Back to Roxy
            </Link>
          </>
        ) : error ? (
          <>
            <h2 className="linktv__title">Sign in a TV</h2>
            <p className="error">{error}</p>
            <Link className="btn btn--grey btn--pill" to="/">
              Back to Roxy
            </Link>
          </>
        ) : (
          <>
            <h2 className="linktv__title">Sign in a TV</h2>
            <div className="linktv__code">{pretty}</div>
            <p className="muted">
              {info ? (
                <>
                  <b>{info.device}</b> ({info.client}) wants to sign in as <b>{s?.userName}</b>. Only approve this if the code
                  matches the one on your screen.
                </>
              ) : (
                'Checking the code…'
              )}
            </p>
            <button className="btn btn--brand btn--pill" type="button" onClick={approve} disabled={busy || !info || info.approved}>
              {busy ? 'Signing in…' : info?.approved ? 'Already approved' : 'Sign in this TV'}
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
    if (c.length >= 6) nav('/link/' + c)
  }
  return (
    <div className="login">
      <form className="login__box linktv" onSubmit={submit}>
        <div className="login__brand">
          <WitchHat size={40} />
          <span>Roxy</span>
        </div>
        <h2 className="linktv__title">Sign in a TV</h2>
        <p className="muted">Enter the code on the TV's sign-in screen.</p>
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
