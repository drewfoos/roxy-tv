import { useState } from 'react'
import { ApiError, api } from '../../api/client'
import { useSession } from '../../app/useSession'
import { ActionButton, Field, Notice, PanelHead, TextInput } from '../../components/Form'

/** Change password (the phone's screen): current, new twice; signs out the other devices. */
export function PasswordPage() {
  const s = useSession()
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [again, setAgain] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [done, setDone] = useState(false)

  const submit = async () => {
    setError(null)
    setDone(false)
    if (next.length < 8) return setError('The new password needs at least 8 characters.')
    if (next !== again) return setError('The two new passwords are not the same.')
    try {
      await api.changePassword(current, next)
      setDone(true)
      setCurrent('')
      setNext('')
      setAgain('')
    } catch (e) {
      if (e instanceof ApiError && e.status === 403) setError("That's not your current password.")
      else if (e instanceof ApiError && e.status === 429) setError('Too many wrong passwords. Wait a minute and try again.')
      else setError(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <section className="acc-form">
      <PanelHead title="Change Password" sub={`Signed in as ${s?.userName ?? ''}. Changing the password signs you out of your other devices; you'll enter the new one when you sign back in.`} />
      <form
        className="acc-stack"
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <Field label="Current password">
          <TextInput type="password" value={current} onChange={setCurrent} autoComplete="current-password" />
        </Field>
        <Field label="New password (at least 8 characters)">
          <TextInput type="password" value={next} onChange={setNext} autoComplete="new-password" />
        </Field>
        <Field label="New password again">
          <TextInput type="password" value={again} onChange={setAgain} autoComplete="new-password" />
        </Field>
        {error && <Notice kind="error">{error}</Notice>}
        {done && <Notice kind="ok">Password changed. Your other devices are signed out.</Notice>}
        <div className="acc-actions">
          <ActionButton onClick={submit} disabled={!current || !next || !again}>
            Change password
          </ActionButton>
        </div>
      </form>
    </section>
  )
}
