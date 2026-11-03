import { useState, type ReactNode } from 'react'
import { IconCheck, IconClose, IconExpand } from './Icons'

/*
 * The settings controls, drawn like Crunchyroll's account pages: a small grey label over
 * a bold value with a hairline under it, round switches with a tick or a cross, uppercase
 * brand buttons, and section headings inside a centred 420 px column.
 */

/** The panel's heading: a centred title and a grey line under it. */
export function PanelHead({ title, sub }: { title: string; sub?: ReactNode }) {
  return (
    <div className="panel-head">
      <h1>{title}</h1>
      {sub && <p>{sub}</p>}
    </div>
  )
}

export function SectionHead({ children }: { children: ReactNode }) {
  return <h2 className="form-section">{children}</h2>
}

/** A labelled value with a hairline: the shell for selects and text inputs. */
export function Field({ label, children, hint }: { label: string; children: ReactNode; hint?: ReactNode }) {
  return (
    <label className="ffield">
      <span className="ffield__label">{label}</span>
      {children}
      {hint && <span className="ffield__hint">{hint}</span>}
    </label>
  )
}

export function Select<T extends string | number>({ value, options, onChange, disabled }: { value: T; options: { id: T; label: string; disabled?: boolean }[]; onChange: (v: T) => void; disabled?: boolean }) {
  const numeric = typeof value === 'number'
  return (
    <span className="fselect">
      <select value={String(value)} disabled={disabled} onChange={(e) => onChange((numeric ? Number(e.target.value) : e.target.value) as T)}>
        {options.map((o) => (
          <option key={String(o.id)} value={String(o.id)} disabled={o.disabled}>
            {o.label}
          </option>
        ))}
      </select>
      <IconExpand size={20} />
    </span>
  )
}

export function TextInput({ value, onChange, type = 'text', placeholder, autoComplete, maxLength, disabled }: { value: string; onChange: (v: string) => void; type?: string; placeholder?: string; autoComplete?: string; maxLength?: number; disabled?: boolean }) {
  return <input className="finput" type={type} value={value} placeholder={placeholder} autoComplete={autoComplete} maxLength={maxLength} disabled={disabled} onChange={(e) => onChange(e.target.value)} />
}

/**
 * A key or password field: masked by default, with a Show/Hide toggle so a pasted key can
 * be checked without it sitting on screen (Drew streams; a key on stream is a leaked key).
 */
export function SecretInput({ value, onChange, placeholder, autoComplete = 'off', inputClassName = 'finput', label = 'key' }: { value: string; onChange: (v: string) => void; placeholder?: string; autoComplete?: string; inputClassName?: string; label?: string }) {
  const [shown, setShown] = useState(false)
  return (
    <span className="fsecret">
      <input className={inputClassName || undefined} type={shown ? 'text' : 'password'} value={value} placeholder={placeholder} autoComplete={autoComplete} autoCapitalize="none" spellCheck={false} onChange={(e) => onChange(e.target.value)} />
      <button type="button" className="fsecret__toggle" aria-pressed={shown} aria-label={(shown ? 'Hide ' : 'Show ') + label} onClick={() => setShown((s) => !s)}>
        {shown ? 'Hide' : 'Show'}
      </button>
    </span>
  )
}

/** A secret shown once (a new API key): masked until Reveal, with Copy either way. */
export function SecretValue({ label, value }: { label: string; value: string }) {
  const [shown, setShown] = useState(false)
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1400)
    } catch {
      setShown(true) // no clipboard (plain http on a LAN address): reveal so it can be selected
    }
  }
  return (
    <div className="copyrow">
      <span className="ffield__label">{label}</span>
      <div className="copyrow__line">
        <code>{shown ? value : '•'.repeat(Math.min(value.length, 32))}</code>
        <button type="button" className="fsecret__toggle" aria-pressed={shown} onClick={() => setShown((s) => !s)}>
          {shown ? 'Hide' : 'Reveal'}
        </button>
        <button type="button" className="fsecret__toggle" onClick={() => void copy()} aria-label={`Copy ${label}`}>
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
    </div>
  )
}

export function TextArea({ value, onChange, rows = 4, placeholder }: { value: string; onChange: (v: string) => void; rows?: number; placeholder?: string }) {
  return <textarea className="finput finput--area" rows={rows} value={value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} />
}

/** A bold title with the switch at the right and a grey description under it. */
export function ToggleRow({ title, hint, on, onChange, disabled }: { title: string; hint?: ReactNode; on: boolean; onChange: (v: boolean) => void; disabled?: boolean }) {
  return (
    <div className="trow">
      <div className="trow__top">
        <span className="trow__title">{title}</span>
        <button type="button" role="switch" aria-checked={on} aria-label={title} className={'switch' + (on ? ' switch--on' : '')} disabled={disabled} onClick={() => onChange(!on)}>
          <i>{on ? <IconCheck size={12} /> : <IconClose size={12} />}</i>
        </button>
      </div>
      {hint && <div className="trow__hint">{hint}</div>}
    </div>
  )
}

/** One choice per row with a radio dot, for the short lists the phone shows as radios. */
export function RadioList<T extends string | number>({ value, options, onChange }: { value: T; options: { id: T; label: string; hint?: string; disabled?: boolean }[]; onChange: (v: T) => void }) {
  return (
    <div className="radios" role="radiogroup">
      {options.map((o) => (
        <button key={String(o.id)} type="button" role="radio" aria-checked={o.id === value} className={'radio' + (o.id === value ? ' radio--on' : '') + (o.disabled ? ' radio--off' : '')} disabled={o.disabled} onClick={() => onChange(o.id)}>
          <i />
          <span>
            <b>{o.label}</b>
            {o.hint && <small>{o.hint}</small>}
          </span>
        </button>
      ))}
    </div>
  )
}

export function Notice({ kind = 'info', children }: { kind?: 'info' | 'error' | 'ok'; children: ReactNode }) {
  return <div className={'notice notice--' + kind}>{children}</div>
}

/** A primary button that shows its own busy and done states. */
export function ActionButton({ children, onClick, disabled, kind = 'brand', done }: { children: ReactNode; onClick: () => Promise<unknown> | void; disabled?: boolean; kind?: 'brand' | 'grey' | 'outline' | 'danger'; done?: string }) {
  const [busy, setBusy] = useState(false)
  const [ok, setOk] = useState(false)
  const run = async () => {
    setBusy(true)
    setOk(false)
    try {
      await onClick()
      if (done) {
        setOk(true)
        setTimeout(() => setOk(false), 1600)
      }
    } finally {
      setBusy(false)
    }
  }
  return (
    <button type="button" className={'btn btn--' + kind} disabled={disabled || busy} onClick={run}>
      {busy ? 'Working…' : ok && done ? done : children}
    </button>
  )
}

/** Label-value rows for read-only facts (version, device, dates). */
export function Facts({ rows }: { rows: [string, ReactNode][] }) {
  return (
    <dl className="facts">
      {rows.map(([k, v]) => (
        <div key={k}>
          <dt>{k}</dt>
          <dd>{v}</dd>
        </div>
      ))}
    </dl>
  )
}

/** A list row: bold title, grey line, an action at the right. */
export function ListRow({ title, sub, right, onClick }: { title: ReactNode; sub?: ReactNode; right?: ReactNode; onClick?: () => void }) {
  const body = (
    <>
      <div className="lrow__text">
        <b>{title}</b>
        {sub && <span>{sub}</span>}
      </div>
      {right && <div className="lrow__right">{right}</div>}
    </>
  )
  return onClick ? (
    <button type="button" className="lrow lrow--button" onClick={onClick}>
      {body}
    </button>
  ) : (
    <div className="lrow">{body}</div>
  )
}

export function errorText(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

export function formatWhen(iso?: string | null): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString(undefined, { month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit' })
}

export function formatBytes(n?: number): string {
  if (!n || n <= 0) return ''
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 10 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}
