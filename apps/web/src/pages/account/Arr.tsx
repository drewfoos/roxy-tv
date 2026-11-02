import { useEffect, useState } from 'react'
import { api } from '../../api/client'
import type { ArrConfig, ArrSettings, ArrTest } from '../../api/admin'
import { useAsync } from '../../hooks/useAsync'
import { ActionButton, Field, Notice, PanelHead, Select, TextInput, ToggleRow, errorText, formatBytes } from '../../components/Form'
import { Spinner } from '../../components/Skeleton'

type Kind = 'sonarr' | 'radarr'
const EMPTY: ArrConfig = { url: '', apiKey: '', rootFolder: '', qualityProfileId: null, animeRootFolder: '', animeQualityProfileId: null, tags: [], animeTags: [], seasonFolders: true, minimumAvailability: 'released', searchOnAdd: true, externalUrl: '' }

const AVAILABILITY = [
  { id: 'announced', label: 'Announced' },
  { id: 'inCinemas', label: 'In cinemas' },
  { id: 'released', label: 'Released' },
]

/** Sonarr & Radarr (admin): where Discover requests go. */
export function ArrPage() {
  const { data, error, loading, reload } = useAsync('arr-settings', () => api.arrSettings())
  const [kind, setKind] = useState<Kind>('sonarr')
  const [draft, setDraft] = useState<ArrSettings | null>(null)
  const [tests, setTests] = useState<Partial<Record<Kind, ArrTest>>>({})
  const [err, setErr] = useState<string | null>(null)
  useEffect(() => {
    if (data && !draft) setDraft({ sonarr: { ...EMPTY, ...data.sonarr }, radarr: { ...EMPTY, ...data.radarr } })
  }, [data, draft])

  if (error) return <Notice kind="error">{error}</Notice>
  if (loading || !draft) return <Spinner />
  const cfg = draft[kind]
  const test = tests[kind]
  const patch = (p: Partial<ArrConfig>) => setDraft({ ...draft, [kind]: { ...cfg, ...p } })
  const name = kind === 'sonarr' ? 'Sonarr' : 'Radarr'
  // The key lives in Sonarr / Radarr itself: link straight to the page that shows it.
  const arrBase = (cfg.externalUrl?.trim() || cfg.url.trim()).replace(/\/+$/, '')
  const keyPage = /^https?:\/\/[^/]/i.test(arrBase) ? `${arrBase}/settings/general` : null

  const runTest = async () => {
    setErr(null)
    try {
      const t = await api.testArr(kind, cfg.url.trim(), cfg.apiKey)
      setTests({ ...tests, [kind]: t })
      if (t.error) setErr(t.error)
    } catch (e) {
      setErr(errorText(e))
    }
  }
  const save = async () => {
    setErr(null)
    try {
      const saved = await api.saveArrSettings({ sonarr: draft.sonarr, radarr: draft.radarr })
      setDraft({ sonarr: { ...EMPTY, ...saved.sonarr }, radarr: { ...EMPTY, ...saved.radarr } })
      reload(true)
    } catch (e) {
      setErr(errorText(e))
    }
  }
  const toggleTag = (key: 'tags' | 'animeTags', id: number) => {
    const cur = cfg[key] ?? []
    patch({ [key]: cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id] })
  }
  const rootHint = (path: string) => {
    const r = test?.rootFolders?.find((f) => f.path === path)
    if (!r) return null
    if (r.roxyMatch === 'yes') return `Roxy watches this folder as "${r.roxy}". Imports show up on their own.`
    if (r.roxyMatch === 'maybe') return `This may be inside a Roxy library (${r.roxy}). Check after the first download.`
    if (r.roxyMatch === 'no') return 'Roxy has no library on this folder: add it under Library or downloads will not appear.'
    return null
  }
  const folderOptions = (test?.rootFolders ?? []).map((f) => ({ id: f.path, label: `${f.path}${f.freeSpace ? ` · ${formatBytes(f.freeSpace)} free` : ''}` }))
  const profileOptions = (test?.qualityProfiles ?? []).map((p) => ({ id: p.id, label: p.name }))

  return (
    <section className="acc-form">
      <PanelHead title="Sonarr & Radarr" sub="Where Discover requests go. Sonarr fetches series, Radarr fetches movies; both import into folders Roxy watches." />
      <div className="chips">
        <button type="button" className={'chip' + (kind === 'sonarr' ? ' chip--on' : '')} onClick={() => setKind('sonarr')}>
          Sonarr (series)
        </button>
        <button type="button" className={'chip' + (kind === 'radarr' ? ' chip--on' : '')} onClick={() => setKind('radarr')}>
          Radarr (movies)
        </button>
      </div>
      <Field label={`${name} URL (http://host:port, with any URL base)`}>
        <TextInput value={cfg.url} onChange={(v) => patch({ url: v })} placeholder={kind === 'sonarr' ? 'http://192.168.1.10:8989' : 'http://192.168.1.10:7878'} autoComplete="off" />
      </Field>
      <Field
        label="API key"
        hint={
          <>
            {name}'s own key: in {name}, Settings → General → Security → API Key.{' '}
            {keyPage ? (
              <a href={keyPage} target="_blank" rel="noreferrer">
                Open {name}'s General settings
              </a>
            ) : (
              `Fill in the URL above for a link to it.`
            )}{' '}
            A saved key stays unless you type a new one.
          </>
        }
      >
        <TextInput type="password" value={cfg.apiKey} onChange={(v) => patch({ apiKey: v })} autoComplete="off" />
      </Field>
      <div className="acc-actions">
        <ActionButton kind="grey" onClick={runTest} disabled={!cfg.url.trim()}>
          Test connection
        </ActionButton>
        {test && !test.error && (
          <span className="ffield__hint">
            {test.appName} {test.version}
            {test.supported === false ? ' · this version is too old' : ''}
            {test.note ? ` · ${test.note}` : ''}
          </span>
        )}
      </div>

      {test && !test.error ? (
        <>
          <Field label={`Root folder (where ${name} saves downloads)`} hint={cfg.rootFolder ? rootHint(cfg.rootFolder) : undefined}>
            <Select value={cfg.rootFolder ?? ''} options={[{ id: '', label: 'Choose…' }, ...folderOptions]} onChange={(v) => patch({ rootFolder: v })} />
          </Field>
          <Field label="Quality profile">
            <Select value={cfg.qualityProfileId ?? 0} options={[{ id: 0, label: 'Choose…' }, ...profileOptions]} onChange={(v) => patch({ qualityProfileId: v || 0 })} />
          </Field>
          <Field label={kind === 'sonarr' ? 'Anime root folder' : 'Anime movies root folder'} hint={cfg.animeRootFolder ? rootHint(cfg.animeRootFolder) : undefined}>
            <Select value={cfg.animeRootFolder ?? ''} options={[{ id: '', label: 'Same as above' }, ...folderOptions]} onChange={(v) => patch({ animeRootFolder: v })} />
          </Field>
          <Field label={kind === 'sonarr' ? 'Anime quality profile' : 'Anime movies quality profile'}>
            <Select value={cfg.animeQualityProfileId ?? 0} options={[{ id: 0, label: 'Same as above' }, ...profileOptions]} onChange={(v) => patch({ animeQualityProfileId: v || 0 })} />
          </Field>
          {kind === 'sonarr' && (
            <>
              <Field
                label="Older anime quality profile"
                hint="For anime that first aired before 2010 and has ended, which often exists only as DVD or 480p releases. Shows still airing (One Piece) keep the anime profile."
              >
                <Select value={cfg.oldAnimeQualityProfileId ?? 0} options={[{ id: 0, label: 'Same as anime' }, ...profileOptions]} onChange={(v) => patch({ oldAnimeQualityProfileId: v || 0 })} />
              </Field>
              <Field
                label="Anime series type"
                hint={cfg.animeSeriesType === 'anime' ? 'Sonarr names and searches by absolute episode numbers (Episode 1047).' : 'Sonarr names and searches by season and episode (S21E1047), like the rest of your library.'}
              >
                <Select
                  value={cfg.animeSeriesType === 'anime' ? 'anime' : 'standard'}
                  options={[{ id: 'standard', label: 'Standard (recommended)' }, { id: 'anime', label: 'Anime (absolute numbering)' }]}
                  onChange={(v) => patch({ animeSeriesType: v as 'standard' | 'anime' })}
                />
              </Field>
            </>
          )}
          {(test.tags?.length ?? 0) > 0 && (
            <>
              <div className="ffield">
                <span className="ffield__label">Tags</span>
                <div className="chips chips--small">
                  {test.tags!.map((t) => (
                    <button key={t.id} type="button" className={'chip' + ((cfg.tags ?? []).includes(t.id) ? ' chip--on' : '')} onClick={() => toggleTag('tags', t.id)}>
                      {t.label}
                    </button>
                  ))}
                </div>
              </div>
              {kind === 'sonarr' && (
                <div className="ffield">
                  <span className="ffield__label">Anime tags</span>
                  <div className="chips chips--small">
                    {test.tags!.map((t) => (
                      <button key={t.id} type="button" className={'chip' + ((cfg.animeTags ?? []).includes(t.id) ? ' chip--on' : '')} onClick={() => toggleTag('animeTags', t.id)}>
                        {t.label}
                      </button>
                    ))}
                  </div>
                </div>
              )}
            </>
          )}
        </>
      ) : (
        cfg.url && (
          <Notice>
            Saved: root folder {cfg.rootFolder || 'not set'}, quality profile {cfg.qualityProfileId ?? 'not set'}
            {kind === 'sonarr' && cfg.animeRootFolder ? `, anime in ${cfg.animeRootFolder}` : ''}. Test the connection to change them.
          </Notice>
        )
      )}

      {kind === 'radarr' && (
        <Field label="Minimum availability" hint="How early Radarr starts looking for a movie.">
          <Select value={cfg.minimumAvailability ?? 'released'} options={AVAILABILITY} onChange={(v) => patch({ minimumAvailability: v })} />
        </Field>
      )}
      {kind === 'sonarr' && <ToggleRow title="Season folders" hint="Sonarr puts each season in its own folder, the way Roxy expects." on={cfg.seasonFolders !== false} onChange={(v) => patch({ seasonFolders: v })} />}
      <ToggleRow
        title="Search when added"
        hint={kind === 'sonarr' ? 'Sonarr searches each requested season as soon as a request arrives (season packs first). A show Sonarr has but does not monitor is left alone.' : 'Radarr starts looking as soon as a request arrives.'} on={cfg.searchOnAdd !== false} onChange={(v) => patch({ searchOnAdd: v })} />
      <Field label="External URL (optional, for links)" hint={`Where you open ${name} from your browser, if it differs from the URL above.`}>
        <TextInput value={cfg.externalUrl ?? ''} onChange={(v) => patch({ externalUrl: v })} autoComplete="off" />
      </Field>
      {err && <Notice kind="error">{err}</Notice>}
      <div className="acc-actions">
        <ActionButton onClick={save} done="Saved">
          Save
        </ActionButton>
      </div>
      <p className="acc-foot">Saving sends any requests made earlier. Leave the URL empty to disconnect; requests then wait until it is back.</p>
    </section>
  )
}
