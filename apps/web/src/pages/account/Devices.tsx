import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import { useAsync } from '../../hooks/useAsync'
import { ActionButton, ListRow, Notice, PanelHead, errorText, formatWhen } from '../../components/Form'
import { Spinner } from '../../components/Skeleton'

/** Friendly names for the apps' client strings. */
const CLIENTS: Record<string, string> = { 'roxy-web': 'Roxy web', Roxy: 'Roxy phone', 'Roxy TV': 'Roxy TV' }
const NETWORKS: Record<string, string> = { home: 'Home network', tailscale: 'Tailscale', server: 'This server', internet: 'Internet' }

/**
 * Every device signed in with this login (Crunchyroll's Device Management): this one
 * first-class, which are online or playing right now, when each was last seen, and a
 * way to sign the others out. Several people share one login here, so it lists them all.
 */
export function DevicesPage() {
  const { data, error, loading, reload } = useAsync('devices', () => api.devices())
  const [err, setErr] = useState<string | null>(null)
  // Addresses stay hidden until asked for, one device at a time: this page can end up on
  // a stream, and a public address must never be shown there by accident.
  const [shown, setShown] = useState<Record<string, boolean>>({})
  // Online and "Playing" move: refresh while the page is open.
  useEffect(() => {
    const h = window.setInterval(() => {
      if (!document.hidden) reload(true)
    }, 30000)
    return () => window.clearInterval(h)
  }, [reload])
  const devices = data?.devices ?? []
  const signOut = async (id: string, name: string) => {
    if (!window.confirm(`Sign out ${name}? It will need to sign in again to use Roxy.`)) return
    setErr(null)
    try {
      await api.signOutDevice(id)
      reload(true)
    } catch (e) {
      setErr(errorText(e))
    }
  }
  return (
    <section className="acc-form">
      <PanelHead title="Device Management" sub="Every app and browser signed in with your login, and what they are playing." />
      {(error || err) && <Notice kind="error">{error || err}</Notice>}
      {loading && !data ? (
        <Spinner />
      ) : devices.length === 0 ? (
        <Notice>No devices are signed in.</Notice>
      ) : (
        <div className="acc-list">
          {devices.map((d) => {
            const name = d.name || CLIENTS[d.client ?? ''] || d.client || 'Unknown device'
            const status = d.nowPlaying?.title ? `Playing: ${d.nowPlaying.title}` : d.online ? 'Online now' : d.lastSeen ? `Last seen: ${formatWhen(d.lastSeen)}` : null
            return (
              <ListRow
                key={d.id}
                title={
                  <>
                    {name}
                    {d.current && <span className="tag">This device</span>}
                  </>
                }
                sub={
                  <>
                    {d.client && <div>{CLIENTS[d.client] ?? d.client}</div>}
                    {status && <div>{status}</div>}
                    {(d.network || d.ip) && (
                      <div className="dev-ip">
                        {NETWORKS[d.network ?? ''] ?? 'Network unknown'}
                        {d.ip && (
                          <>
                            {' · '}
                            <span className={shown[d.id] ? 'dev-ip__addr' : 'dev-ip__addr dev-ip__addr--hidden'}>{shown[d.id] ? d.ip : '•••.•••.•••.•••'}</span>
                            <button type="button" className="dev-ip__toggle" onClick={() => setShown((m) => ({ ...m, [d.id]: !m[d.id] }))}>
                              {shown[d.id] ? 'Hide' : 'Show'}
                            </button>
                          </>
                        )}
                      </div>
                    )}
                    {d.createdAt && <div>Signed in: {formatWhen(d.createdAt)}</div>}
                  </>
                }
                right={
                  d.current ? undefined : (
                    <ActionButton kind="outline" onClick={() => signOut(d.id, name)}>
                      Sign out
                    </ActionButton>
                  )
                }
              />
            )
          })}
        </div>
      )}
      <p className="acc-foot">Changing your password signs every other device out.</p>
    </section>
  )
}
