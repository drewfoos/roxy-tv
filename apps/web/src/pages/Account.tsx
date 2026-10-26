import { useNavigate } from 'react-router'
import { api, px, APP_VERSION } from '../api/client'
import { useAsync } from '../hooks/useAsync'
import { useSession } from '../app/useSession'
import { Avatar, TopBar, signOut } from '../components/TopBar'
import { Footer } from '../components/Footer'
import { IconLogout, IconPerson } from '../components/Icons'

/** Account: the profile card, switch profile, sign out, and what server this is. */
export function Account() {
  const s = useSession()
  const nav = useNavigate()
  const { data: info } = useAsync('system-info', () => api.systemInfo())
  if (!s) return null
  return (
    <div className="page page--tab">
      <TopBar />
      <div className="tabhead">
        <h1>Account</h1>
      </div>
      <div className="account">
        <div className="card-list">
          <div className="card-list__row">
            <Avatar name={s.profileName || s.userName} url={s.profileId && s.profileHasAvatar ? api.profileAvatarUrl(s.profileId, px(64) * 2, s.profileAvatarKey) : null} size={64} />
            <div className="card-list__text">
              <b>{s.profileName || s.userName}</b>
              <span>
                {s.userName} · {info?.name ?? window.location.host}
                {s.isAdmin ? ' · admin' : ''}
              </span>
            </div>
          </div>
        </div>
        <div className="card-list">
          <button className="card-list__row" onClick={() => nav('/profiles')}>
            <IconPerson />
            <div className="card-list__text">
              <b>Switch profile</b>
              <span>Back to "Who's watching?"</span>
            </div>
          </button>
          <button className="card-list__row" onClick={signOut}>
            <IconLogout />
            <div className="card-list__text">
              <b>Sign out</b>
              <span>This browser only</span>
            </div>
          </button>
        </div>
        <div className="section-label">ABOUT</div>
        <div className="card-list">
          <div className="card-list__row">
            <div className="card-list__text">
              <b>Roxy Web {APP_VERSION}</b>
              <span>
                Server {info?.name ?? ''} {info?.version ? `· ${info.version}` : ''}
              </span>
            </div>
          </div>
        </div>
      </div>
      <Footer />
    </div>
  )
}
