import { useNavigate } from 'react-router'
import { useSession } from '../../app/useSession'
import { signOut } from '../../components/TopBar'
import { PanelHead } from '../../components/Form'

/** Log out, with the same words as the phone's dialog. */
export function LogoutPage() {
  const s = useSession()
  const nav = useNavigate()
  return (
    <section className="acc-form acc-form--centre">
      <PanelHead title="Log Out" sub={`Every profile on ${s?.userName ?? 'this login'} signs out of this browser. Your watch history stays on the server.`} />
      <div className="acc-actions acc-actions--centre">
        <button className="btn btn--brand" onClick={signOut}>
          Log out
        </button>
        <button className="btn btn--grey" onClick={() => nav('/profiles')}>
          Switch profile instead
        </button>
      </div>
    </section>
  )
}
