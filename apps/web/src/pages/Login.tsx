import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../api/client'
import { session } from '../api/session'
import { WitchHat } from '../components/Icons'

/** Sign in to the server this page came from. */
export function Login() {
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [serverName, setServerName] = useState<string | null>(null)
  const [needsSetup, setNeedsSetup] = useState(false)

  useEffect(() => {
    api.systemInfo().then((i) => setServerName(i.name ?? null)).catch(() => setServerName(null))
    api.setupStatus().then((s) => setNeedsSetup(!!s.required)).catch(() => undefined)
  }, [])

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      const r = await api.login(name.trim(), password)
      session.set({ token: r.token, userId: r.user.id, userName: r.user.name, isAdmin: !!r.user.isAdmin, profileId: null, profileName: '', profileHasAvatar: false, profileAvatarKey: '' })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Sign-in failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login">
      <form className="login__box" onSubmit={submit}>
        <div className="login__brand">
          <WitchHat size={40} />
          <span>Roxy</span>
        </div>
        <div className="muted">{serverName ? `Sign in to ${serverName}` : 'Sign in'}</div>
        {needsSetup && <div className="muted">This server is not set up yet. Open the Roxy app on a phone on your home network to create the first account.</div>}
        <div className="field">
          <label htmlFor="user">Username</label>
          <input id="user" value={name} onChange={(e) => setName(e.target.value)} autoComplete="username" autoFocus required />
        </div>
        <div className="field">
          <label htmlFor="pass">Password</label>
          <input id="pass" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />
        </div>
        {error && <div className="error">{error}</div>}
        <button className="btn btn--brand btn--pill" type="submit" disabled={busy || needsSetup}>
          {busy ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
    </div>
  )
}
