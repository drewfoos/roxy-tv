import { useNavigate } from 'react-router'
import { api, px } from '../api/client'
import { session } from '../api/session'
import type { Profile } from '../api/types'
import { useAsync, clearAsyncCache } from '../hooks/useAsync'
import { Avatar, signOut } from '../components/TopBar'
import { Spinner } from '../components/Skeleton'
import { useSession } from '../app/useSession'
import { WitchHat } from '../components/Icons'

/** "Who's watching?": the profiles on this login. */
export function Profiles() {
  const s = useSession()
  const nav = useNavigate()
  const { data, error, loading } = useAsync('profiles', () => api.profiles())

  const choose = (p: Profile) => {
    clearAsyncCache()
    session.update({ profileId: p.id, profileName: p.name, profileHasAvatar: !!p.hasAvatar, profileAvatarKey: p.avatarKey ?? '' })
    nav('/', { replace: true })
  }

  if (loading && !data) return <Spinner />
  return (
    <div className="profiles">
      <span className="profiles__hat">
        <WitchHat size={44} />
      </span>
      <h1>Who's watching?</h1>
      {error && <div className="error">{error}</div>}
      <div className="profiles__list">
        {(data?.profiles ?? []).map((p) => (
          <button key={p.id} className="profiles__one" onClick={() => choose(p)}>
            <Avatar name={p.name} url={p.hasAvatar ? api.profileAvatarUrl(p.id, px(120) * 2, p.avatarKey ?? '') : null} size={120} selected={p.id === s?.profileId} />
            <span>{p.name}</span>
          </button>
        ))}
      </div>
      <div className="profiles__actions">
        <button className="profiles__manage" onClick={() => nav('/account/profiles')}>
          Manage profiles
        </button>
        <button className="link" onClick={signOut}>
          Sign out of {s?.userName}
        </button>
      </div>
    </div>
  )
}
