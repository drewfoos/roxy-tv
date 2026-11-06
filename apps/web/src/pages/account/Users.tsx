import { useState } from 'react'
import { api } from '../../api/client'
import type { AccountUser } from '../../api/admin'
import { useAsync } from '../../hooks/useAsync'
import { ActionButton, Field, ListRow, Notice, PanelHead, RadioList, SectionHead, TextInput, errorText } from '../../components/Form'
import { Spinner } from '../../components/Skeleton'

const ROLES = [
  { id: 'admin', label: 'Admin', hint: 'Everything, including these settings' },
  { id: 'requester', label: 'Requester', hint: 'Watches; requests go straight to Sonarr and Radarr' },
  { id: 'basic', label: 'Basic', hint: 'Watches; requests wait for an admin to approve them' },
]

const roleName = (r: string) => ROLES.find((x) => x.id === r)?.label ?? r

/** Users (admin): the accounts on this server, their level, password resets, disable, delete, add. */
export function UsersPage() {
  const { data, error, loading, reload } = useAsync('users', () => api.users())
  const [open, setOpen] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)
  const users = data?.users ?? []
  return (
    <section className="acc-form">
      <PanelHead title="Users" sub="Give each person or household their own account. Profiles under an account share its level." />
      {error && <Notice kind="error">{error}</Notice>}
      {loading && !data ? (
        <Spinner />
      ) : (
        <div className="acc-list">
          {users.map((u) => (
            <div key={u.id}>
              <ListRow
                title={
                  <>
                    {u.name}
                    {u.isYou && <span className="tag">You</span>}
                    {u.isDisabled && <span className="tag tag--quiet">Disabled</span>}
                  </>
                }
                sub={roleName(u.role)}
                right={<span className="link-brand">{open === u.id ? 'Close' : 'Edit'}</span>}
                onClick={() => setOpen(open === u.id ? null : u.id)}
              />
              {open === u.id && <UserEditor user={u} onChanged={() => reload(true)} onGone={() => reload(true)} />}
            </div>
          ))}
        </div>
      )}
      {adding ? (
        <AddUser
          onDone={(changed) => {
            setAdding(false)
            if (changed) reload(true)
          }}
        />
      ) : (
        <div className="acc-actions">
          <button className="btn btn--brand" onClick={() => setAdding(true)}>
            Add user
          </button>
        </div>
      )}
    </section>
  )
}

function UserEditor({ user, onChanged, onGone }: { user: AccountUser; onChanged: () => void; onGone: () => void }) {
  const [role, setRole] = useState(user.role)
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [confirm, setConfirm] = useState(false)
  if (user.isYou) return <Notice>You can't change your own level, disable or delete yourself. Change your password under Account.</Notice>
  const save = async () => {
    setError(null)
    if (password && password.length < 8) {
      setError('A new password needs at least 8 characters.')
      return false
    }
    try {
      await api.updateUser(user.id, { role: role !== user.role ? role : undefined, password: password || undefined })
      setPassword('')
      onChanged()
    } catch (e) {
      setError(errorText(e))
      return false
    }
  }
  const toggleDisabled = async () => {
    setError(null)
    try {
      await api.updateUser(user.id, { disabled: !user.isDisabled })
      onChanged()
    } catch (e) {
      setError(errorText(e))
      return false
    }
  }
  const remove = async () => {
    setError(null)
    try {
      await api.deleteUser(user.id)
      onGone()
    } catch (e) {
      setError(errorText(e))
    }
  }
  return (
    <div className="acc-sub">
      <div className="ffield">
        <span className="ffield__label">Level</span>
        <RadioList value={role} options={ROLES} onChange={setRole} />
      </div>
      <Field label="New password (leave empty to keep theirs)" hint="A new password or disabling signs them out everywhere.">
        <TextInput type="password" value={password} onChange={setPassword} autoComplete="new-password" />
      </Field>
      {error && <Notice kind="error">{error}</Notice>}
      <div className="acc-actions">
        <ActionButton onClick={save} done="Saved">
          Save
        </ActionButton>
        <ActionButton kind="grey" onClick={toggleDisabled}>
          {user.isDisabled ? 'Enable account' : 'Disable account'}
        </ActionButton>
        <button className={'btn ' + (confirm ? 'btn--danger' : 'btn--outline')} onClick={() => (confirm ? void remove() : setConfirm(true))}>
          {confirm ? 'Tap again to delete' : 'Delete account'}
        </button>
      </div>
      {confirm && <div className="ffield__hint">Deleting removes their profiles and watch history. Their requests stay. There is no undo.</div>}
    </div>
  )
}

function AddUser({ onDone }: { onDone: (changed: boolean) => void }) {
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [role, setRole] = useState('requester')
  const [error, setError] = useState<string | null>(null)
  const add = async () => {
    setError(null)
    if (!name.trim()) {
      setError('Give them a name to sign in with.')
      return false
    }
    if (password.length < 8) {
      setError('The password needs at least 8 characters.')
      return false
    }
    try {
      await api.createUser(name.trim(), password, role)
      onDone(true)
    } catch (e) {
      setError(errorText(e))
      return false
    }
  }
  return (
    <div className="acc-stack">
      <SectionHead>Add user</SectionHead>
      <Field label="Name (what they type to sign in)">
        <TextInput value={name} onChange={setName} autoComplete="off" />
      </Field>
      <Field label="Password (at least 8 characters)" hint="Tell them the name and password yourself; they can change the password once they're in.">
        <TextInput type="password" value={password} onChange={setPassword} autoComplete="new-password" />
      </Field>
      <div className="ffield">
        <span className="ffield__label">Level</span>
        <RadioList value={role} options={ROLES} onChange={setRole} />
      </div>
      {error && <Notice kind="error">{error}</Notice>}
      <div className="acc-actions">
        <ActionButton onClick={add}>Add user</ActionButton>
        <button className="btn btn--grey" onClick={() => onDone(false)}>
          Cancel
        </button>
      </div>
    </div>
  )
}
