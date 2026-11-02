import { useState } from 'react'
import { useNavigate } from 'react-router'
import { api, px } from '../api/client'
import { session } from '../api/session'
import type { Profile } from '../api/types'
import { useAsync, clearAsyncCache } from '../hooks/useAsync'
import { Avatar, signOut } from '../components/TopBar'
import { useSession } from '../app/useSession'
import { IconBack, IconEdit, WitchHat } from '../components/Icons'
import { ProfileEditor } from './account/Profiles'

const MAX_PROFILES = 6
// The profile picked last on this browser, kept across sign-outs (the apps' lastProfileId).
const LAST_KEY = 'roxy.lastProfileId'

function lastPicked(): string | null {
  try {
    return localStorage.getItem(LAST_KEY)
  } catch {
    return null
  }
}

/**
 * "Who's watching?", the same screen as the apps' ProfilePicker.kt: brand-blue avatars,
 * the cat on the last profile used, an "Add profile" tile, and "Manage profiles" that
 * edits right here (pencil on each avatar, tap to edit).
 */
export function Profiles() {
  const s = useSession()
  const nav = useNavigate()
  const { data, error, loading, reload } = useAsync('profiles', () => api.profiles())
  const [managing, setManaging] = useState(false)
  const [editing, setEditing] = useState<Profile | 'new' | null>(null)
  const profiles = data?.profiles ?? []
  const lastId = s?.profileId ?? lastPicked()
  // 112 like the apps, 96 once there are five or more so two rows still fit.
  const size = profiles.length >= 5 ? 96 : 112

  const choose = (p: Profile) => {
    if (managing) return setEditing(p)
    try {
      localStorage.setItem(LAST_KEY, p.id)
    } catch {
      // private window: the cat just won't remember
    }
    clearAsyncCache()
    session.update({ profileId: p.id, profileName: p.name, profileHasAvatar: !!p.hasAvatar, profileAvatarKey: p.avatarKey ?? '' })
    nav('/', { replace: true })
  }

  if (editing) {
    return (
      <div className="profiles profiles--editor">
        <section className="acc-form">
          <ProfileEditor
            profile={editing === 'new' ? null : editing}
            onDone={(changed) => {
              // Save or Cancel: back to the picker, out of Manage profiles (as on the phone and TV).
              setEditing(null)
              setManaging(false)
              if (changed) {
                clearAsyncCache()
                reload()
              }
            }}
          />
        </section>
      </div>
    )
  }

  return (
    <div className="profiles" style={{ ['--tile' as string]: size + 'px' }}>
      {s?.profileId && !managing && (
        <button className="profiles__back" aria-label="Back" onClick={() => nav('/', { replace: true })}>
          <IconBack size={24} />
        </button>
      )}
      <span className="profiles__hat">
        <WitchHat size={44} />
      </span>
      <h1>Who's watching?</h1>
      {loading && !data ? (
        <div className="profiles__list" aria-hidden>
          <div className="bone profiles__bone" />
          <div className="bone profiles__bone" />
        </div>
      ) : error && !data ? (
        <div className="profiles__error">
          <div className="error">{error}</div>
          <button className="btn btn--brand" onClick={() => reload()}>
            Try again
          </button>
        </div>
      ) : (
        <div className="profiles__list">
          {profiles.map((p) => {
            const last = p.id === lastId
            return (
              <button key={p.id} className={'profiles__one' + (last && !managing ? ' profiles__one--last' : '')} onClick={() => choose(p)}>
                <span className="profiles__ring">
                  <Avatar name={p.name} url={p.hasAvatar ? api.profileAvatarUrl(p.id, px(size) * 2, p.avatarKey ?? '') : null} size={size} />
                  {managing && (
                    <span className="profiles__edit">
                      <IconEdit size={36} />
                    </span>
                  )}
                  {last && !managing && <CatOnRing />}
                </span>
                <span className="profiles__name">{p.name}</span>
              </button>
            )
          })}
          {profiles.length < MAX_PROFILES && (
            <button className="profiles__one profiles__add" onClick={() => setEditing('new')}>
              <span className="profiles__ring">
                <span className="profiles__add-circle">
                  <svg width="44" height="44" viewBox="0 0 24 24" fill="currentColor" aria-hidden>
                    <path d="M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z" />
                  </svg>
                </span>
              </span>
              <span className="profiles__name">Add profile</span>
            </button>
          )}
        </div>
      )}
      <div className="profiles__actions">
        <button className={'profiles__manage' + (managing ? ' profiles__manage--on' : '')} onClick={() => setManaging(!managing)}>
          {managing ? 'Done' : 'Manage profiles'}
        </button>
        <button className="profiles__logout" onClick={signOut}>
          Log out
        </button>
      </div>
    </div>
  )
}

/** The little cat peeking over the last profile's ring (CatOnRing in ProfilePicker.kt). */
function CatOnRing() {
  const fur = 'var(--accent-light)'
  const ink = 'var(--accent-dark)'
  const sw = 0.03 * 100
  return (
    <svg className="profiles__cat" width="44" height="35.2" viewBox="0 0 100 80" aria-hidden>
      <ellipse cx="24" cy="64.8" rx="12" ry="8.8" fill={fur} stroke={ink} strokeWidth={sw} />
      <ellipse cx="76" cy="64.8" rx="12" ry="8.8" fill={fur} stroke={ink} strokeWidth={sw} />
      <path d="M18 36 L16 4 L42 22.4 Z M82 36 L84 4 L58 22.4 Z" fill={fur} stroke={ink} strokeWidth={sw} strokeLinejoin="round" />
      <ellipse cx="50" cy="40.8" rx="38" ry="26.4" fill={fur} stroke={ink} strokeWidth={sw} />
      <path d="M30 41.6 Q37 33.6 44 41.6 M56 41.6 Q63 33.6 70 41.6" fill="none" stroke={ink} strokeWidth={3.5} strokeLinecap="round" />
      <path d="M46 48 L54 48 L50 52.8 Z" fill={ink} />
      <path d="M14 48 L30 50.4 M86 48 L70 50.4" stroke={ink} strokeWidth={2} strokeLinecap="round" />
    </svg>
  )
}
