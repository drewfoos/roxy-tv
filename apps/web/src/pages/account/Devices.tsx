import { api } from '../../api/client'
import { session } from '../../api/session'
import { useAsync } from '../../hooks/useAsync'
import { ListRow, Notice, PanelHead, formatWhen } from '../../components/Form'
import { Spinner } from '../../components/Skeleton'

/** The players signed in with this login (Crunchyroll's Device Management). */
export function DevicesPage() {
  const { data, error, loading } = useAsync('players', () => api.players())
  const mine = session.deviceId()
  const players = data?.players ?? []
  return (
    <section className="acc-form">
      <PanelHead title="Device Management" sub="The apps and browsers signed in with your login, and what they are playing." />
      {error && <Notice kind="error">{error}</Notice>}
      {loading && !data ? (
        <Spinner />
      ) : players.length === 0 ? (
        <Notice>No other device has checked in yet. Phones, TVs and browsers appear here once they play something.</Notice>
      ) : (
        <div className="acc-list">
          {players.map((p) => {
            const here = p.deviceId === mine
            return (
              <ListRow
                key={p.deviceId}
                title={
                  <>
                    {p.name || p.client || p.deviceId}
                    {here && <span className="tag">This browser</span>}
                  </>
                }
                sub={
                  <>
                    {p.client && <div>{p.client}</div>}
                    {p.nowPlaying?.title ? <div>Playing: {p.nowPlaying.title}</div> : null}
                    {p.lastSeen && <div>Last seen: {formatWhen(p.lastSeen)}</div>}
                  </>
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
