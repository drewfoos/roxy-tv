import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { api, ApiError, APP_VERSION } from '../api/client'
import type { SetupStatus } from '../api/types'
import { session } from '../api/session'
import { WitchHat } from '../components/Icons'
import { MediaFolders } from './account/Library'
import { IconEye, IconEyeOff, LegalDialog, TermsLine, recordTerms, signIn, type Terms } from './Login'

// The phone app's first-time setup (ui/setup/SetupWizard.kt), in the browser: Welcome,
// the admin account, TMDB for posters and Discover, the media folders with naming tips,
// then "You're all set". Before the account exists it runs from the sign-in page; after,
// the browser is signed in and a flag here brings it back (even after a reload) until Finish.

const STEP_WELCOME = 0
const STEP_ACCOUNT = 1
const STEP_DETAILS = 2
const STEP_MEDIA = 3
const STEP_DONE = 4

const PROGRESS_KEY = 'roxy.setupInProgress'

/** The user id whose setup is still running in this browser, if any. */
export function setupInProgressFor(): string | null {
  try {
    return localStorage.getItem(PROGRESS_KEY)
  } catch {
    return null
  }
}

function setSetupInProgress(userId: string | null) {
  try {
    if (userId) localStorage.setItem(PROGRESS_KEY, userId)
    else localStorage.removeItem(PROGRESS_KEY)
  } catch {
    /* private mode: a reload lands on Home instead */
  }
}

/** An API error as a sentence, like the phone's apiError. */
function apiError(e: unknown): string {
  if (e instanceof ApiError) return e.message.charAt(0).toUpperCase() + e.message.slice(1)
  if (e instanceof TypeError) return "Can't reach the server. Check it's switched on and that you're on the same network."
  return e instanceof Error ? e.message : 'Something went wrong.'
}

/**
 * The wizard, full page. `status` comes from the sign-in page (no account yet);
 * signed in (setup already under way) it starts at Details. `onFinish` runs after Finish.
 */
export function SetupWizard({ status, onCancel, onFinish }: { status?: SetupStatus; onCancel?: () => void; onFinish?: () => void }) {
  const signedIn = !!session.get()
  const [step, setStep] = useState(signedIn ? STEP_DETAILS : STEP_WELCOME)
  const [doc, setDoc] = useState<'terms' | 'privacy' | null>(null)
  const go = (s: number) => {
    setStep(s)
    window.scrollTo(0, 0)
  }
  const back = step === STEP_WELCOME ? undefined : () => go(step - 1)
  const finish = () => {
    setSetupInProgress(null)
    onFinish?.()
  }

  return (
    <div className="signin">
      <div className="signin__wall signin__wall--empty" aria-hidden="true" />
      <main className="signin__center setup">
        <div className="signin__card setup__card">
          {step !== STEP_WELCOME && <StepHeader step={step} onBack={step === STEP_ACCOUNT && signedIn ? undefined : back} />}
          {step === STEP_WELCOME && <Welcome status={status} onStart={() => go(STEP_ACCOUNT)} onCancel={onCancel} />}
          {step === STEP_ACCOUNT && <AccountStep onDone={() => go(STEP_DETAILS)} onShow={setDoc} />}
          {step === STEP_DETAILS && <DetailsStep onNext={() => go(STEP_MEDIA)} />}
          {step === STEP_MEDIA && <MediaStep onNext={() => go(STEP_DONE)} />}
          {step === STEP_DONE && <DoneStep onFinish={finish} />}
        </div>
        <div className="signin__foot">
          <span>Roxy TV · web {APP_VERSION}</span>
        </div>
      </main>
      {doc && <LegalDialog doc={doc} onClose={() => setDoc(null)} />}
    </div>
  )
}

/** Back arrow, then four short bars for Account, Details, Media, Done. */
function StepHeader({ step, onBack }: { step: number; onBack?: () => void }) {
  return (
    <div className="setup__head">
      {onBack ? (
        <button type="button" className="setup__back" onClick={onBack} aria-label="Back">
          <IconBack />
        </button>
      ) : (
        <span className="setup__back" />
      )}
      <div className="setup__bars" aria-hidden="true">
        {[STEP_ACCOUNT, STEP_DETAILS, STEP_MEDIA, STEP_DONE].map((i) => (
          <span key={i} className={i <= step ? 'setup__bar setup__bar--on' : 'setup__bar'} />
        ))}
      </div>
      <span className="setup__count">{step} of 4</span>
    </div>
  )
}

function Welcome({ status, onStart, onCancel }: { status?: SetupStatus; onStart: () => void; onCancel?: () => void }) {
  const name = status?.serverName || 'Roxy'
  const allowed = status?.allowed !== false
  return (
    <div className="setup__hero">
      <div className="setup__hat">
        <WitchHat size={112} />
      </div>
      <div className="setup__brand">Roxy TV</div>
      <p className="setup__tagline">Your shows and movies, beautifully.</p>
      <span className="setup__pill">
        <span className="setup__dot" />
        New server found: {name}
      </span>
      {allowed ? (
        <>
          <p className="setup__intro">Let's get it ready. It takes about two minutes: an account for you, where posters come from, and where your media lives.</p>
          <button type="button" className="signin__primary setup__wide" onClick={onStart}>
            Get started <IconArrow />
          </button>
        </>
      ) : (
        <div className="signin__error" role="alert">
          Setup only works from the server's home network. Open this page on a computer or phone connected to the same network as the server, using its local address (like 192.168.1.20:8096).
        </div>
      )}
      {onCancel && (
        <button type="button" className="setup__cancel" onClick={onCancel}>
          Not your server? Go back
        </button>
      )}
    </div>
  )
}

function StepTitle({ title, children }: { title: string; children: ReactNode }) {
  return (
    <>
      <h1 className="signin__title">{title}</h1>
      <p className="signin__lead">{children}</p>
    </>
  )
}

function AccountStep({ onDone, onShow }: { onDone: () => void; onShow: (doc: 'terms' | 'privacy') => void }) {
  const s = session.get()
  const [name, setName] = useState('')
  const [pw, setPw] = useState('')
  const [pw2, setPw2] = useState('')
  const [show, setShow] = useState(false)
  const [agreed, setAgreed] = useState(false)
  const [version, setVersion] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  useEffect(() => {
    api.legal('terms').then((d) => setVersion(d.version)).catch(() => setVersion(null))
  }, [])

  // Back here after creating it: the server's setup door is shut (it can't be made twice),
  // so show what was made instead of a form that would be refused.
  if (s) {
    return (
      <>
        <StepTitle title="Your account">Created and signed in. You can change the password later in Account › Change password.</StepTitle>
        <div className="setup__box setup__row">
          <IconCheckCircle />
          <span className="setup__who">
            <b>{s.userName}</b>
            <span>Admin</span>
          </span>
        </div>
        <button type="button" className="signin__primary" onClick={onDone}>
          Continue <IconArrow />
        </button>
      </>
    )
  }

  // A new server: agreeing is always asked here, like a first sign-in.
  const terms: Terms = { version, needed: true, agreed, setAgreed, show: onShow }
  const ready = name.trim() !== '' && pw.length >= 8 && pw === pw2 && agreed && !busy
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (busy) return
    if (!name.trim()) return setErr('Enter a name to sign in with.')
    if (pw.length < 8) return setErr('The password needs at least 8 characters.')
    if (pw !== pw2) return setErr("The two passwords don't match.")
    if (!agreed) return setErr('Please agree to the Terms of Use and Privacy Policy to continue.')
    setBusy(true)
    setErr(null)
    try {
      const r = await api.setupAdmin(name.trim(), pw)
      await recordTerms(r.token, version)
      // The flag first, so the signed-in app opens back here instead of on Home.
      setSetupInProgress(r.user.id)
      signIn(r)
      onDone()
    } catch (e) {
      setErr(apiError(e))
      setBusy(false)
    }
  }

  return (
    <>
      <StepTitle title="Create your account">You're the admin: you'll manage the server and can add your family later, each with their own account and profiles.</StepTitle>
      <form className="signin__form" onSubmit={submit} noValidate>
        <label className="signin__field">
          <span>Your name (what you'll sign in with)</span>
          <input value={name} onChange={(e) => (setName(e.target.value), setErr(null))} autoComplete="username" autoCapitalize="none" spellCheck={false} autoFocus />
        </label>
        <label className="signin__field">
          <span>Password</span>
          <span className="signin__password">
            <input type={show ? 'text' : 'password'} value={pw} onChange={(e) => (setPw(e.target.value), setErr(null))} autoComplete="new-password" />
            <button type="button" className="signin__eye" onClick={() => setShow((v) => !v)} aria-label={show ? 'Hide password' : 'Show password'} aria-pressed={show}>
              {show ? <IconEyeOff /> : <IconEye />}
            </button>
          </span>
        </label>
        <label className="signin__field">
          <span>Password again</span>
          <input type={show ? 'text' : 'password'} value={pw2} onChange={(e) => (setPw2(e.target.value), setErr(null))} autoComplete="new-password" />
        </label>
        <PasswordHint pw={pw} pw2={pw2} />
        <TermsLine terms={terms} />
        {err && (
          <div className="signin__error" role="alert">
            {err}
          </div>
        )}
        <button className="signin__primary" type="submit" disabled={!ready}>
          {busy ? <span className="signin__spinner" aria-hidden="true" /> : <IconTick />}
          {busy ? 'Creating…' : 'Create account'}
        </button>
      </form>
    </>
  )
}

function PasswordHint({ pw, pw2 }: { pw: string; pw2: string }) {
  let text = 'Good. Longer is even better.'
  let tone = 'setup__hint--accent'
  if (pw === '') {
    text = 'At least 8 characters. A short sentence is easy to remember and hard to guess.'
    tone = ''
  } else if (pw.length < 8) {
    const n = 8 - pw.length
    text = `${n} more character${n === 1 ? '' : 's'}`
    tone = ''
  } else if (pw2 !== '' && pw !== pw2) {
    text = "The two passwords don't match yet"
    tone = 'setup__hint--error'
  } else if (pw.length >= 14) {
    text = 'Strong password'
    tone = 'setup__hint--ok'
  }
  return <div className={'setup__hint ' + tone}>{text}</div>
}

function DetailsStep({ onNext }: { onNext: () => void }) {
  const [has, setHas] = useState<boolean | null>(null)
  const [key, setKey] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  useEffect(() => {
    api
      .tmdb()
      .then((t) => setHas(!!t.set))
      .catch(() => setHas(false))
  }, [])
  const can = key !== '' || has === true
  const submit = async (e: FormEvent) => {
    e.preventDefault()
    if (busy) return
    if (key === '') {
      if (has) onNext()
      return
    }
    setBusy(true)
    setErr(null)
    try {
      await api.saveTmdb(key)
      onNext()
    } catch (e) {
      setErr(apiError(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <>
      <StepTitle title="Posters and details">Roxy names your files and fetches posters, descriptions, cast and the Discover page from TMDB. It's free, but every server needs its own key.</StepTitle>
      <div className="setup__box">
        {has === true ? (
          <>
            <div className="setup__row">
              <IconCheckCircle />
              <span>This server already has a TMDB key.</span>
            </div>
            <p className="setup__small">You can paste a different one below, or just continue.</p>
          </>
        ) : (
          <>
            <b>Get a free key</b>
            <ol className="setup__numbered">
              <li>Make a free account at themoviedb.org</li>
              <li>Open Settings › API and create a key (choose "Personal")</li>
              <li>Copy the API Key (or the longer Read Access Token) and paste it here</li>
            </ol>
            <a className="setup__link" href="https://www.themoviedb.org/settings/api" target="_blank" rel="noreferrer">
              Open TMDB <IconOpen />
            </a>
          </>
        )}
      </div>
      <form className="signin__form" onSubmit={submit} noValidate>
        <label className="signin__field">
          <span>TMDB API key</span>
          <input value={key} onChange={(e) => (setKey(e.target.value.trim()), setErr(null))} autoComplete="off" autoCapitalize="none" spellCheck={false} />
        </label>
        {err && (
          <div className="signin__error" role="alert">
            {err}
          </div>
        )}
        <p className="setup__small">Anime details come from AniList, which needs no key at all.</p>
        <button className="signin__primary" type="submit" disabled={!can || busy}>
          {busy ? <span className="signin__spinner" aria-hidden="true" /> : null}
          {busy ? 'Checking…' : key !== '' ? 'Check and continue' : has ? 'Continue' : 'Paste a key to continue'}
          {!busy && can && <IconArrow />}
        </button>
      </form>
      {/* No skip: without a key the library has no posters, titles or Discover, which is no first impression. */}
      {has === false && <p className="setup__small setup__centre">A key is needed to continue. It takes about two minutes and never costs anything.</p>}
    </>
  )
}

function MediaStep({ onNext }: { onNext: () => void }) {
  return (
    <>
      <StepTitle title="Your media">Point Roxy at the folders that hold your shows and movies. It only ever reads them; nothing is moved or deleted.</StepTitle>
      <div className="setup__folders">
        <MediaFolders />
      </div>
      <div className="setup__box">
        <div className="setup__row">
          <IconBulb />
          <b>Name things like this</b>
        </div>
        <p className="setup__small">Roxy finds the right show, season and episode from the names:</p>
        <Tree
          lines={[
            [0, 'Shows/'],
            [1, 'Frieren (2023)/'],
            [2, 'Season 01/'],
            [3, 'Frieren (2023) - S01E01.mkv'],
            [3, 'Frieren (2023) - S01E02.mkv'],
            [0, 'Movies/'],
            [1, 'Spirited Away (2001)/'],
            [2, 'Spirited Away (2001).mkv'],
          ]}
        />
        <ul className="setup__bullets">
          <li>One folder per show or movie, with the year in brackets</li>
          <li>Episodes as S01E01; anime numbered 1, 2, 3… works too</li>
          <li>Shows and movies can share a folder, or keep their own</li>
          <li>Sonarr and Radarr already name everything this way</li>
          <li>Custom shows (fan edits like One Pace) can bring a tvshow.nfo</li>
        </ul>
      </div>
      <button type="button" className="signin__primary" onClick={onNext}>
        Continue <IconArrow />
      </button>
      <p className="setup__small setup__centre">You can add or change folders any time in Account › Library.</p>
    </>
  )
}

function DoneStep({ onFinish }: { onFinish: () => void }) {
  const [tmdb, setTmdb] = useState<boolean | null>(null)
  const [folders, setFolders] = useState<number | null>(null)
  useEffect(() => {
    api
      .tmdb()
      .then((t) => setTmdb(!!t.set))
      .catch(() => setTmdb(false))
    api
      .folders()
      .then((f) => setFolders(f.libraries?.length ?? 0))
      .catch(() => setFolders(0))
  }, [])
  return (
    <>
      <div className="setup__hero setup__hero--done">
        <WitchHat size={84} />
        <h1 className="signin__title">You're all set</h1>
        <p className="signin__lead">Roxy is scanning your folders now. Shows appear on Home as they're found.</p>
      </div>
      <div className="setup__box">
        <Check ok>Your admin account</Check>
        <Check ok={tmdb === true}>{tmdb === true ? 'Posters and details from TMDB' : 'TMDB key: add one in Settings for posters and Discover'}</Check>
        <Check ok={(folders ?? 0) > 0}>{folders == null ? 'Media folders' : folders === 0 ? 'No media folders yet: add them in Account › Library' : folders === 1 ? '1 media folder' : `${folders} media folders`}</Check>
      </div>
      <div className="setup__box">
        <b>Next, when you like</b>
        <ul className="setup__bullets">
          <li>Connect Sonarr and Radarr to request shows from Discover (Account › Sonarr &amp; Radarr)</li>
          <li>Add your family in Account › Users</li>
        </ul>
      </div>
      <button type="button" className="signin__primary" onClick={onFinish}>
        <IconPlay /> Start watching
      </button>
    </>
  )
}

function Check({ ok, children }: { ok?: boolean; children: ReactNode }) {
  return (
    <div className={ok ? 'setup__row setup__check' : 'setup__row setup__check setup__check--todo'}>
      {ok ? <IconCheckCircle /> : <IconCircle />}
      <span>{children}</span>
    </div>
  )
}

/** A folder tree in a monospaced box; files in the brand colour. */
function Tree({ lines }: { lines: [number, string][] }) {
  return (
    <div className="setup__tree">
      {lines.map(([depth, name], i) => (
        <div key={i} className={name.endsWith('/') ? undefined : 'setup__file'}>
          {'  '.repeat(depth) + (depth > 0 ? '└ ' : '') + name}
        </div>
      ))}
    </div>
  )
}

// ---- small line icons (24 px grid, currentColor), like the sign-in page's ----

const svg = { width: 20, height: 20, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.8, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const, 'aria-hidden': true }

function IconBack() {
  return (
    <svg {...svg} width={22} height={22}>
      <path d="M19 12H5M11 6l-6 6 6 6" />
    </svg>
  )
}

function IconArrow() {
  return (
    <svg {...svg} width={18} height={18}>
      <path d="M5 12h14M13 6l6 6-6 6" />
    </svg>
  )
}

function IconTick() {
  return (
    <svg {...svg} width={18} height={18} strokeWidth={2.4}>
      <path d="m5 12 5 5 9-10" />
    </svg>
  )
}

function IconPlay() {
  return (
    <svg width={18} height={18} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d="M8 5.5v13l11-6.5z" />
    </svg>
  )
}

function IconCheckCircle() {
  return (
    <svg width={20} height={20} viewBox="0 0 24 24" aria-hidden="true" className="setup__ok">
      <circle cx="12" cy="12" r="10" fill="currentColor" />
      <path d="m7.5 12.5 3 3 6-6.5" fill="none" stroke="#06080d" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

function IconCircle() {
  return (
    <svg {...svg}>
      <circle cx="12" cy="12" r="9" />
    </svg>
  )
}

function IconBulb() {
  return (
    <svg {...svg} className="setup__bulb">
      <path d="M9 18h6M10 21h4M12 3a6 6 0 0 0-3.5 10.9c.6.5 1 1.2 1 2.1h5c0-.9.4-1.6 1-2.1A6 6 0 0 0 12 3Z" />
    </svg>
  )
}

function IconOpen() {
  return (
    <svg {...svg} width={16} height={16}>
      <path d="M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5" />
    </svg>
  )
}
