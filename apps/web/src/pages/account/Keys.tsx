import { useState } from 'react'
import { api } from '../../api/client'
import { useAsync } from '../../hooks/useAsync'
import { ActionButton, Field, Notice, PanelHead, SectionHead, TextInput, errorText } from '../../components/Form'

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
    }
  }

  return (
    <section className="acc-form">
      <PanelHead title="TMDB & fanart.tv" sub="The services that name your titles and dress them with art. Keys are stored on the server and never shown again." />

      <SectionHead>TMDB</SectionHead>
      <Notice kind={tmdb.data?.set ? 'ok' : 'info'}>{tmdb.data == null ? '…' : tmdb.data.set ? `A TMDB key is saved${tmdb.data.source === 'server' ? ' on the server' : ''}. Paste a new one to replace it.` : 'No TMDB key yet. Discover and metadata lookups need one.'}</Notice>
      <Field label="TMDB API key" hint="Free at themoviedb.org: Settings → API → Create. Checked with TMDB when you save.">
        <TextInput type="password" value={tmdbKey} onChange={setTmdbKey} autoComplete="off" />
      </Field>
      {tmdbErr && <Notice kind="error">{tmdbErr}</Notice>}
      <div className="acc-actions">
        <ActionButton onClick={saveTmdb} done="Saved" disabled={!tmdbKey.trim()}>
          Save TMDB key
        </ActionButton>
      </div>

      <SectionHead>fanart.tv</SectionHead>
      <Notice kind={fanart.data?.enabled ? 'ok' : 'info'}>{fanart.data == null ? '…' : fanart.data.enabled ? 'fanart.tv is on: logos and backgrounds come from it when TMDB has none.' : 'Off. Add a project key to get logos and backgrounds for the art picker.'}</Notice>
      <Field label="Project API key" hint="Free at fanart.tv under your account's API page. An empty key turns fanart.tv off.">
        <TextInput type="password" value={fanartKey} onChange={setFanartKey} autoComplete="off" />
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
