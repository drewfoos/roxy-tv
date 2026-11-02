import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { api, px } from '../../api/client'
import { session } from '../../api/session'
import type { AvatarCharacter } from '../../api/admin'
import type { Profile } from '../../api/types'
import { useAsync, clearAsyncCache } from '../../hooks/useAsync'
import { useSession } from '../../app/useSession'
import { Avatar } from '../../components/TopBar'
import { ActionButton, Field, ListRow, Notice, PanelHead, SectionHead, TextInput, errorText } from '../../components/Form'
import { Spinner } from '../../components/Skeleton'

const MAX_PROFILES = 6

/**
 * The profiles on this login (the phone's "Manage profiles" editor): name, an avatar
 * from the library's characters or an AniList search, add up to six, delete any but
 * the default.
 */
export function ProfilesPage() {
  const s = useSession()
  const { data, error, loading, reload } = useAsync('profiles', () => api.profiles())
  const [editing, setEditing] = useState<Profile | 'new' | null>(null)
  const profiles = data?.profiles ?? []
  return (
    <section className="acc-form">
      <PanelHead title="Profiles" sub="Everyone on this login gets their own watchlist, history and place in each show." />
      {error && <Notice kind="error">{error}</Notice>}
      {loading && !data ? (
        <Spinner />
      ) : editing ? (
        <ProfileEditor
          profile={editing === 'new' ? null : editing}
          onDone={(changed) => {
            setEditing(null)
            if (changed) {
              clearAsyncCache()
              reload()
            }
          }}
        />
      ) : (
        <>
          <div className="acc-list">
            {profiles.map((p) => (
              <ListRow
                key={p.id}
                title={
                  <span className="prow">
                    <Avatar name={p.name} url={p.hasAvatar ? api.profileAvatarUrl(p.id, px(44) * 2, p.avatarKey ?? '') : null} size={44} />
                    <span>
                      {p.name}
                      {p.id === s?.profileId && <span className="tag">Current</span>}
                      {p.isDefault && <span className="tag tag--quiet">Default</span>}
                    </span>
                  </span>
                }
                sub={p.avatarName ? `Avatar: ${p.avatarName}` : undefined}
                right={<span className="link-brand">Edit</span>}
                onClick={() => setEditing(p)}
              />
            ))}
          </div>
          <div className="acc-actions">
            {profiles.length < MAX_PROFILES && (
              <button className="btn btn--brand" onClick={() => setEditing('new')}>
                Add profile
              </button>
            )}
            <Link className="btn btn--grey" to="/profiles">
              Switch profile
            </Link>
          </div>
        </>
      )}
    </section>
  )
}

export function ProfileEditor({ profile, onDone }: { profile: Profile | null; onDone: (changed: boolean) => void }) {
  const s = useSession()
  const [name, setName] = useState(profile?.name ?? '')
  const [picked, setPicked] = useState<AvatarCharacter | null>(null)
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<AvatarCharacter[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const { data: suggested } = useAsync('avatars-suggested', () => api.avatarsSuggested().catch(() => ({ characters: [] as AvatarCharacter[] })))

  useEffect(() => {
    const q = query.trim()
    if (q.length < 2) {
      setResults(null)
      return
    }
    let live = true
    const t = setTimeout(() => {
      api
        .avatarsSearch(q)
        .then((r) => live && setResults(r.characters))
        .catch(() => live && setResults([]))
    }, 350)
    return () => {
      live = false
      clearTimeout(t)
    }
  }, [query])

  const save = async () => {
    setError(null)
    const n = name.trim()
    if (!n) return setError('Give the profile a name.')
    try {
      let saved: Profile
      if (profile) saved = await api.updateProfile(profile.id, { name: n, avatarUrl: picked?.image, avatarName: picked?.name })
      else saved = await api.createProfile(n, picked?.image, picked?.name)
      if (profile && profile.id === s?.profileId) session.update({ profileName: saved.name, profileHasAvatar: !!saved.hasAvatar, profileAvatarKey: saved.avatarKey ?? '' })
      onDone(true)
    } catch (e) {
      setError(errorText(e))
    }
  }
  const remove = async () => {
    if (!profile) return
    try {
      await api.deleteProfile(profile.id)
      if (profile.id === s?.profileId) session.update({ profileId: null, profileName: '', profileHasAvatar: false, profileAvatarKey: '' })
      onDone(true)
    } catch (e) {
      setError(errorText(e))
    }
  }

  const preview = picked ? picked.image : profile?.hasAvatar ? api.profileAvatarUrl(profile.id, px(96) * 2, profile.avatarKey ?? '') : null
  const grid = (chars: AvatarCharacter[]) => (
    <div className="avatar-grid">
      {chars.map((c) => (
        <button key={c.image} type="button" className={'avatar-grid__one' + (picked?.image === c.image ? ' avatar-grid__one--on' : '')} onClick={() => setPicked(c)} title={c.series ? `${c.name} · ${c.series}` : c.name}>
          <img src={c.image} alt="" loading="lazy" />
          <span>{c.name}</span>
        </button>
      ))}
    </div>
  )

  return (
    <div className="acc-stack">
      <SectionHead>{profile ? 'Edit profile' : 'New profile'}</SectionHead>
      <div className="prow prow--big">
        <Avatar name={name || '?'} url={preview} size={96} />
        <div style={{ flex: 1 }}>
          <Field label="Name">
            <TextInput value={name} onChange={setName} maxLength={24} />
          </Field>
          {picked && (
            <div className="ffield__hint">
              Picked: {picked.name}
              {picked.series ? ` · ${picked.series}` : ''}
            </div>
          )}
        </div>
      </div>
      {(suggested?.characters.length ?? 0) > 0 && (
        <>
          <div className="ffield__label">From your library</div>
          {grid(suggested!.characters.slice(0, 12))}
        </>
      )}
      <Field label="Search characters">
        <TextInput value={query} onChange={setQuery} placeholder="Any anime character, e.g. Roxy Migurdia" />
      </Field>
      {results && (results.length ? grid(results.slice(0, 16)) : <div className="ffield__hint">Nobody by that name.</div>)}
      {error && <Notice kind="error">{error}</Notice>}
      <div className="acc-actions">
        <ActionButton onClick={save}>Save</ActionButton>
        <button className="btn btn--grey" onClick={() => onDone(false)}>
          Cancel
        </button>
        {profile && !profile.isDefault && (
          <button className={'btn ' + (confirmDelete ? 'btn--danger' : 'btn--outline')} onClick={() => (confirmDelete ? void remove() : setConfirmDelete(true))}>
            {confirmDelete ? 'Tap again to delete' : 'Delete profile'}
          </button>
        )}
      </div>
    </div>
  )
}
