import { useState } from 'react'
import { api } from '../../api/client'
import { useAsync } from '../../hooks/useAsync'
import { ActionButton, Field, Notice, PanelHead, SecretInput, SectionHead, errorText } from '../../components/Form'

/**
 * TMDB & fanart.tv (admin): the metadata keys. TMDB is where titles, posters and
 * Discover come from; fanart.tv adds logos and backgrounds. Keys are never shown back.
 */
export function KeysPage() {
  const tmdb = useAsync('tmdb', () => api.tmdb())
  const fanart = useAsync('fanart', () => api.fanart())
  const [tmdbKey, setTmdbKey] = useState('')
  const [fanartKey, setFanartKey] = useState('')
  const [tmdbErr, setTmdbErr] = useState<string | null>(null)
  const [fanartErr, setFanartErr] = useState<string | null>(null)

  const saveTmdb = async () => {
    setTmdbErr(null)
    try {
      await api.saveTmdb(tmdbKey.trim())
      setTmdbKey('')
      tmdb.reload(true)
    } catch (e) {
      setTmdbErr(errorText(e))
      return false
    }
  }
  const saveFanart = async () => {
    setFanartErr(null)
    try {
      await api.saveFanart(fanartKey.trim())
      setFanartKey('')
      fanart.reload(true)
    } catch (e) {
      setFanartErr(errorText(e))
      return false
    }
  }

  return (
    <section className="acc-form">
      <PanelHead title="TMDB & fanart.tv" sub="The services that name your titles and dress them with art. Keys are stored on the server and never shown again." />

      <SectionHead>TMDB</SectionHead>
      {tmdb.error && !tmdb.data && <Notice kind="error">{tmdb.error}</Notice>}
      <Notice kind={tmdb.data?.set ? 'ok' : 'info'}>{tmdb.data == null ? (tmdb.error ? "Roxy could not read whether a TMDB key is saved. You can still save one." : '…') : tmdb.data.set ? `A TMDB key is saved${tmdb.data.source === 'server' ? ' on the server' : ''}. Paste a new one to replace it.` : 'No TMDB key yet. Discover and metadata lookups need one.'}</Notice>
      <Field label="TMDB API key" hint="Free at themoviedb.org: Settings → API → Create. Checked with TMDB when you save.">
        <SecretInput label="TMDB key" value={tmdbKey} onChange={setTmdbKey} />
      </Field>
      {tmdbErr && <Notice kind="error">{tmdbErr}</Notice>}
      <div className="acc-actions">
        <ActionButton onClick={saveTmdb} done="Saved" disabled={!tmdbKey.trim()}>
          Save TMDB key
        </ActionButton>
      </div>

      <SectionHead>fanart.tv</SectionHead>
      {fanart.error && !fanart.data && <Notice kind="error">{fanart.error}</Notice>}
      <Notice kind={fanart.data?.enabled ? 'ok' : 'info'}>{fanart.data == null ? (fanart.error ? 'Roxy could not read whether fanart.tv is on.' : '…') : fanart.data.enabled ? 'fanart.tv is on: posters, backgrounds and logos come from it when TMDB has none, and all of its art is in the art picker.' : 'Off. Add a project key to fill in posters, backgrounds and logos TMDB has none of, and to see fanart.tv art in the art picker.'}</Notice>
      <Field label="Project API key" hint="Free at fanart.tv under your account's API page. An empty key turns fanart.tv off.">
        <SecretInput label="fanart.tv key" value={fanartKey} onChange={setFanartKey} />
      </Field>
      {fanartErr && <Notice kind="error">{fanartErr}</Notice>}
      <div className="acc-actions">
        <ActionButton onClick={saveFanart} done="Saved">
          {fanartKey.trim() ? 'Save fanart.tv key' : 'Turn fanart.tv off'}
        </ActionButton>
      </div>
    </section>
  )
}
