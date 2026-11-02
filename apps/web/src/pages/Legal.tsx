import { useParams } from 'react-router'
import { api } from '../api/client'
import { useAsync } from '../hooks/useAsync'
import { TopBar } from '../components/TopBar'
import { Footer } from '../components/Footer'
import { Spinner } from '../components/Skeleton'
import { LegalText } from '../components/LegalText'

/** The server's Terms of Use or Privacy Policy, as it serves them. */
export function Legal() {
  const { doc = 'terms' } = useParams()
  const key = doc === 'privacy' ? 'privacy' : 'terms'
  const { data, error, loading } = useAsync(`legal:${key}`, () => api.legal(key), [key])
  return (
    <div className="page page--tab">
      <TopBar />
      <div className="tabhead">
        <h1>{data?.title ?? (key === 'privacy' ? 'Privacy Policy' : 'Terms of Use')}</h1>
      </div>
      {loading && !data ? <Spinner /> : error && !data ? <div className="empty error">{error}</div> : <div className="legal"><LegalText body={data?.body ?? ''} /></div>}
      <Footer />
    </div>
  )
}
