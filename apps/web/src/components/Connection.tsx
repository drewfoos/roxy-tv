import { useEffect, useState, useSyncExternalStore, type ReactNode } from 'react'
import { connection, connectionWords, type Link } from '../api/connection'
import { IconCheck, IconReplay } from './Icons'

export function useConnection(): Link {
  return useSyncExternalStore(connection.subscribe, connection.get, connection.get)
}

const CloudOff = ({ size = 24 }: { size?: number }) => (
  <svg width={size} height={size} viewBox="0 0 24 24" fill="currentColor" aria-hidden>
    <path d="M19.35 10.04A7.49 7.49 0 0 0 12 4c-1.48 0-2.85.43-4.01 1.17l1.46 1.46A5.5 5.5 0 0 1 17.5 11.5v.5H19a3 3 0 0 1 1.79 5.4l1.41 1.41A4.98 4.98 0 0 0 19.35 10.04zM3 5.27l2.75 2.74A5.99 5.99 0 0 0 6 20h11.73l2 2L21 20.73 4.27 4 3 5.27zM7.73 10l8 8H6a4 4 0 0 1 0-8h1.73z" />
  </svg>
)

/**
 * The quiet line at the bottom of the screen while the server cannot be reached, and a
 * green "Back online" for a moment when it answers again. It never takes clicks: the
 * page under it stays usable (a video already buffered keeps playing).
 */
export function ConnectionStrip() {
  const link = useConnection()
  const [back, setBack] = useState(false)
  useEffect(
    () =>
      connection.onBack(() => {
        setBack(true)
      }),
    [],
  )
  useEffect(() => {
    if (!back) return
    const t = setTimeout(() => setBack(false), 2500)
    return () => clearTimeout(t)
  }, [back])
  if (link === 'ok' && !back) return null
  return (
    <div className={'conn' + (link === 'ok' ? ' conn--back' : '')} role="status">
      {link === 'ok' ? <IconCheck size={18} /> : <CloudOff size={18} />}
      <span>{link === 'ok' ? 'Back online' : connectionWords(link).strip}</span>
      {link === 'away' && <span className="conn__more">Reconnecting…</span>}
    </div>
  )
}

/**
 * A page that could not load. While the server is unreachable every page says the same
 * calm thing (and loads by itself once it is back); otherwise the page's own heading
 * with what the server said.
 */
export function LoadError({ error, onRetry, title, children }: { error: string | null; onRetry?: () => void; title?: string; children?: ReactNode }) {
  const link = useConnection()
  const [checking, setChecking] = useState(false)
  if (link !== 'ok') {
    const words = connectionWords(link)
    const retry = () => {
      if (checking) return
      setChecking(true)
      void connection
        .check()
        .then((up) => {
          // Back: useAsync reloads what failed. Asked for too, for loads it does not own.
          if (up) onRetry?.()
        })
        .finally(() => setChecking(false))
    }
    return (
      <div className="offline">
        <div className="offline__icon">
          <CloudOff size={56} />
        </div>
        <h2>{words.title}</h2>
        <p>{words.body}</p>
        <button className="btn btn--white offline__btn" onClick={retry} disabled={checking}>
          <IconReplay size={18} />
          {checking ? 'Checking…' : 'Try again'}
        </button>
      </div>
    )
  }
  return (
    <div className="offline">
      {title && <h2>{title}</h2>}
      <p className={title ? undefined : 'error'}>{error ?? 'Something went wrong'}</p>
      {children}
      {onRetry && (
        <button className="btn btn--grey offline__btn" onClick={onRetry}>
          Try again
        </button>
      )}
    </div>
  )
}
