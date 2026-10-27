import { Link } from 'react-router'
import { APP_VERSION, api } from '../../api/client'
import { useAsync } from '../../hooks/useAsync'
import { useSession } from '../../app/useSession'
import { Facts, PanelHead, SectionHead } from '../../components/Form'

/** Version, server and the legal documents (the phone's ABOUT group). */
export function AboutPage() {
  const s = useSession()
  const { data: info } = useAsync('system-info', () => api.systemInfo())
  const { data: legal } = useAsync('legal-status', () => api.legalStatus().catch(() => null))
  return (
    <section className="acc-form">
      <PanelHead title="About Roxy" sub="This app and the server it talks to." />
      <Facts
        rows={[
          ['Roxy Web', APP_VERSION],
          ['Server', info ? `${info.name ?? 'Roxy'}${info.version ? ` · ${info.version}` : ''}` : '…'],
          ['Address', window.location.host],
          ['Signed in as', `${s?.userName ?? ''}${s?.isAdmin ? ' · admin' : ''}`],
        ]}
      />
      <SectionHead>Legal</SectionHead>
      <div className="acc-links">
        <Link to="/legal/terms">
          Terms of Use
          <small>{legal?.accepted ? `Accepted · version ${legal.version ?? ''}` : 'Read the terms'}</small>
        </Link>
        <Link to="/legal/privacy">
          Privacy
          <small>What stays on your server and your device</small>
        </Link>
      </div>
    </section>
  )
}
