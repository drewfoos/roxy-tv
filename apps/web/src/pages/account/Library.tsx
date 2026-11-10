import { useEffect, useState } from 'react'
import { Link } from 'react-router'
import { api } from '../../api/client'
import type { BrowseResponse, IdentifyCandidate, LibraryFolder, ManageTitle } from '../../api/admin'
import type { Item } from '../../api/types'
import { markStale, useAsync } from '../../hooks/useAsync'
import { ActionButton, Field, ListRow, Notice, PanelHead, RadioList, SectionHead, TextArea, TextInput, ToggleRow, errorText, formatWhen } from '../../components/Form'
import { Spinner } from '../../components/Skeleton'

const KINDS = [
  { id: 'series', label: 'Shows & movies', hint: 'Finds shows and movies in the folder by itself' },
  { id: 'movies', label: 'Movies only', hint: 'Every video is a movie' },
]

/**
 * Library (admin): the media folders, scans and refreshes, and the titles the server
 * could not match, with Identify and Edit details. Mirrors the phone's screen.
 */
export function LibraryPage() {
  const manage = useAsync('library-manage', () => api.libraryManage())
  const [err, setErr] = useState<string | null>(null)
  const [refreshOpen, setRefreshOpen] = useState(false)
  const [attention, setAttention] = useState<{ title: ManageTitle; mode: 'identify' | 'edit' } | null>(null)

  const busy = !!manage.data && (manage.data.scanning || manage.data.queue > 0)
  useEffect(() => {
    if (!busy) return
    const t = setInterval(() => manage.reload(true), 3000)
    return () => clearInterval(t)
  }, [busy, manage])

  const status = !manage.data ? '…' : manage.data.scanning ? 'Scanning folders…' : manage.data.queue > 0 ? `Looking up details: ${manage.data.queue} waiting` : manage.data.lastScanAt ? `Last scan ${formatWhen(manage.data.lastScanAt)}` : 'Up to date'
  // The libraries the last full scan could not read ("Anime: open /data/Anime: no such
  // file or directory"), shown until a clean scan; the others were still scanned.
  const scanErrors = manage.data && !manage.data.scanning && manage.data.lastScanError
    ? manage.data.lastScanError.split('\n').filter(Boolean).map((l) => l.replace(/^library "(.+?)": /, '$1: '))
    : []
  // A locked title was fixed by hand, so it doesn't need attention.
  const unmatched = (manage.data?.titles ?? []).filter((t) => !t.matched && !t.locked)

  const run: Run = async (fn, after) => {
    setErr(null)
    try {
      await fn()
      after?.()
      manage.reload(true)
      return true
    } catch (e) {
      setErr(errorText(e))
      return false
    }
  }

  return (
    <section className="acc-form">
      <PanelHead title="Library" sub="Where your files live, when they were last read, and the titles that still need a hand." />
      <Notice kind={busy ? 'info' : scanErrors.length ? 'error' : 'ok'}>{status}</Notice>
      {scanErrors.length > 0 && (
        <Notice kind="error">
          The last scan had problems:
          {scanErrors.map((l) => (
            <div key={l}>{l}</div>
          ))}
        </Notice>
      )}
      {!busy &&
        (manage.data?.emptyLibraries ?? []).map((lib) => (
          <Notice key={lib.id} kind="error">
            {lib.name}: the folder {lib.root} has no videos, but Roxy has {lib.files} on record, so it kept them. That is what a drive that is not mounted looks like. If the files really are gone, confirm it and the next scan removes them (anyone's watch history for them comes back if the files return within 30 days).
            <div className="acc-actions">
              <ActionButton
                kind="danger"
                done="Removing"
                onClick={() =>
                  window.confirm(`Remove the ${lib.files} file records of ${lib.name}? Only do this if its files really are gone, not if the drive is just unplugged.`)
                    ? run(() => api.acceptEmptyFolder(lib.id))
                    : Promise.resolve(false)
                }
              >
                The files are gone: remove them
              </ActionButton>
            </div>
          </Notice>
        ))}
      {err && <Notice kind="error">{err}</Notice>}
      {manage.error && <Notice kind="error">{manage.error}</Notice>}

      <SectionHead>Media folders</SectionHead>
      <MediaFolders onChanged={() => manage.reload(true)} />

      <SectionHead>Actions</SectionHead>
      <div className="acc-actions">
        <ActionButton kind="grey" onClick={() => run(() => api.libraryScan('files'))} done="Started">
          Scan for new files
        </ActionButton>
        <ActionButton kind="grey" onClick={() => run(() => api.libraryScan('missing'))} done="Started">
          Find missing metadata
        </ActionButton>
        <button className="btn btn--grey" onClick={() => setRefreshOpen((o) => !o)}>
          Refresh all titles…
        </button>
      </div>
      {refreshOpen && <RefreshForm onDone={() => setRefreshOpen(false)} run={run} />}

      <SectionHead>
        Needs attention
        {unmatched.length > 0 && <span className="tag">{unmatched.length}</span>}
      </SectionHead>
      {unmatched.length === 0 ? (
        <Notice kind="ok">Every title is matched.</Notice>
      ) : (
        <div className="acc-list">
          {unmatched.map((t) => (
            <div key={t.id}>
              <ListRow
                title={`${t.title}${t.year ? ` (${t.year})` : ''}`}
                sub={t.type === 'movie' ? 'Movie' : 'Series'}
                right={
                  <span className="acc-actions acc-actions--tight">
                    <button className="link-brand" onClick={() => setAttention(attention?.title.id === t.id && attention.mode === 'identify' ? null : { title: t, mode: 'identify' })}>
                      Identify
                    </button>
                    <button className="link-brand" onClick={() => setAttention(attention?.title.id === t.id && attention.mode === 'edit' ? null : { title: t, mode: 'edit' })}>
                      Edit details
                    </button>
                    <Link className="link-brand" to={`/${t.type === 'movie' ? 'movie' : 'series'}/${t.id}`}>
                      Open
                    </Link>
                  </span>
                }
              />
              {attention?.title.id === t.id && attention.mode === 'identify' && <Identify title={t} onDone={() => setAttention(null)} run={run} />}
              {attention?.title.id === t.id && attention.mode === 'edit' && <EditDetails title={t} onDone={() => setAttention(null)} run={run} />}
            </div>
          ))}
        </div>
      )}
    </section>
  )
}

/** Runs a change and shows its error; false when it failed, so an ActionButton shows no done label. */
type Run = (fn: () => Promise<unknown>, after?: () => void) => Promise<boolean>

/** The media folders with Edit and Add folder (a folder browser). Also the setup wizard's media step. */
export function MediaFolders({ onChanged }: { onChanged?: () => void }) {
  const folders = useAsync('library-folders', () => api.folders())
  const [err, setErr] = useState<string | null>(null)
  const [editing, setEditing] = useState<LibraryFolder | null>(null)
  const [adding, setAdding] = useState(false)
  const run: Run = async (fn, after) => {
    setErr(null)
    try {
      await fn()
      after?.()
      folders.reload(true)
      onChanged?.()
      return true
    } catch (e) {
      setErr(errorText(e))
      return false
    }
  }
  return (
    <>
      {err && <Notice kind="error">{err}</Notice>}
      {folders.error && <Notice kind="error">{folders.error}</Notice>}
      {folders.loading && !folders.data ? (
        <Spinner />
      ) : (
        <div className="acc-list">
          {(folders.data?.libraries ?? []).map((f) => (
            <div key={f.id}>
              <ListRow
                title={f.name}
                sub={
                  <>
                    <div>{f.root}</div>
                    <div>{f.reachable === false ? "The server can't see this folder right now" : `${f.kind === 'movies' ? 'Movies only' : 'Shows & movies'}${f.animeMode ? ' · anime' : ''} · ${f.series ?? 0} shows, ${f.movies ?? 0} movies`}</div>
                  </>
                }
                right={<span className="link-brand">{editing?.id === f.id ? 'Close' : 'Edit'}</span>}
                onClick={() => setEditing(editing?.id === f.id ? null : f)}
              />
              {editing?.id === f.id && <FolderEditor folder={f} onDone={() => setEditing(null)} run={run} />}
            </div>
          ))}
          {folders.data && !folders.data.libraries?.length && <Notice>No folders yet. Add the one your downloads land in.</Notice>}
        </div>
      )}
      {adding ? (
        <AddFolder onDone={() => setAdding(false)} run={run} />
      ) : (
        <div className="acc-actions">
          <button className="btn btn--brand" onClick={() => setAdding(true)}>
            Add folder
          </button>
        </div>
      )}
    </>
  )
}

function FolderEditor({ folder, onDone, run }: { folder: LibraryFolder; onDone: () => void; run: Run }) {
  const [name, setName] = useState(folder.name)
  const [kind, setKind] = useState(folder.kind)
  const [anime, setAnime] = useState(!!folder.animeMode)
  const [confirm, setConfirm] = useState(false)
  const changedKind = kind !== folder.kind || anime !== !!folder.animeMode
  return (
    <div className="acc-sub">
      <Field label="Name">
        <TextInput value={name} onChange={setName} />
      </Field>
      <div className="ffield">
        <span className="ffield__label">Type</span>
        <RadioList value={kind} options={KINDS} onChange={setKind} />
      </div>
      <ToggleRow title="Anime" hint="Look titles up on AniList first, then TMDB." on={anime} onChange={setAnime} />
      {changedKind && <Notice>Changing the type or anime setting looks every title in this folder up again.</Notice>}
      <div className="acc-actions">
        <ActionButton onClick={() => run(() => api.updateFolder(folder.id, { name: name.trim(), kind, anime }), onDone)}>Save</ActionButton>
        <button className="btn btn--grey" onClick={onDone}>
          Cancel
        </button>
        <button className={'btn ' + (confirm ? 'btn--danger' : 'btn--outline')} onClick={() => (confirm ? void run(() => api.deleteFolder(folder.id), onDone) : setConfirm(true))}>
          {confirm ? 'Tap again to remove' : 'Remove folder'}
        </button>
      </div>
      {confirm && <div className="ffield__hint">Removing forgets the titles and watch history under it. The files stay where they are.</div>}
    </div>
  )
}

function AddFolder({ onDone, run }: { onDone: () => void; run: Run }) {
  const [tree, setTree] = useState<BrowseResponse | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [chosen, setChosen] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [kind, setKind] = useState('series')
  const [anime, setAnime] = useState(false)
  const go = (path?: string) => {
    setErr(null)
    api
      .browse(path)
      .then(setTree)
      .catch((e) => setErr(errorText(e)))
  }
  useEffect(() => go(), [])
  const pick = (path: string) => {
    setChosen(path)
    // Guess the type and anime from the folder's name, like the phone.
    const base = path.split(/[\\/]/).filter(Boolean).pop() ?? ''
    if (!name) setName(base)
    setKind(/movie|film/i.test(base) ? 'movies' : 'series')
    setAnime(/anime/i.test(base))
  }
  return (
    <div className="acc-sub">
      <SectionHead>New folder</SectionHead>
      {err && <Notice kind="error">{err}</Notice>}
      {chosen == null ? (
        tree ? (
          <>
            <div className="ffield__hint">
              {tree.path || 'Starting places'}
              {tree.parent != null && (
                <>
                  {' · '}
                  <button className="link-brand" onClick={() => go(tree.parent ?? undefined)}>
                    Up
                  </button>
                </>
              )}
            </div>
            <div className="acc-list acc-list--scroll">
              {(tree.path ? tree.dirs : (tree.roots ?? tree.dirs)).map((d) => (
                <ListRow key={d.path} title={d.name} sub={d.skip ? d.skip : d.videos ? `${d.videos} videos` : undefined} right={<span className="link-brand">Open</span>} onClick={() => go(d.path)} />
              ))}
            </div>
            {tree.path && (
              <div className="acc-actions">
                <button className="btn btn--brand" onClick={() => pick(tree.path)}>
                  Use this folder
                </button>
                <button className="btn btn--grey" onClick={onDone}>
                  Cancel
                </button>
              </div>
            )}
          </>
        ) : (
          <Spinner />
        )
      ) : (
        <>
          <Notice>{chosen}</Notice>
          <Field label="Name">
            <TextInput value={name} onChange={setName} />
          </Field>
          <div className="ffield">
            <span className="ffield__label">Type</span>
            <RadioList value={kind} options={KINDS} onChange={setKind} />
          </div>
          <ToggleRow title="Anime" hint="Look titles up on AniList first, then TMDB." on={anime} onChange={setAnime} />
          <div className="acc-actions">
            <ActionButton onClick={() => run(() => api.addFolder({ path: chosen, name: name.trim() || chosen, kind, anime }), onDone)} disabled={!name.trim()}>
              Add and scan
            </ActionButton>
            <button className="btn btn--grey" onClick={() => setChosen(null)}>
              Back
            </button>
          </div>
        </>
      )}
    </div>
  )
}

function RefreshForm({ onDone, run, ids }: { onDone: () => void; run: Run; ids?: string[] }) {
  const [opts, setOpts] = useState({ metadata: true, images: false, trickplay: false, segments: false })
  const row = (key: keyof typeof opts, title: string, hint: string) => <ToggleRow title={title} hint={hint} on={opts[key]} onChange={(v) => setOpts({ ...opts, [key]: v })} />
  return (
    <div className="acc-sub">
      {row('metadata', 'Metadata', 'Titles, synopses, episode lists and ratings')}
      {row('images', 'Replace images', 'Download posters and backgrounds again')}
      {row('trickplay', 'Trickplay', 'Rebuild the seek-preview thumbnails')}
      {row('segments', 'Intro & outro', 'Find the openings and endings again')}
      <div className="acc-actions">
        <ActionButton onClick={() => run(() => api.libraryRefresh({ ...opts, ids, all: !ids }), onDone)} done="Started" disabled={!Object.values(opts).some(Boolean)}>
          {ids ? 'Refresh this title' : 'Refresh all titles'}
        </ActionButton>
        <button className="btn btn--grey" onClick={onDone}>
          Cancel
        </button>
      </div>
      <p className="acc-foot">Titles you edited by hand keep their details.</p>
    </div>
  )
}

function Identify({ title, onDone, run }: { title: ManageTitle; onDone: () => void; run: Run }) {
  const [provider, setProvider] = useState<'tmdb' | 'anilist'>('tmdb')
  const [query, setQuery] = useState(title.title)
  const [cands, setCands] = useState<IdentifyCandidate[] | null>(null)
  const [err, setErr] = useState<string | null>(null)
  const [picked, setPicked] = useState<IdentifyCandidate | null>(null)
  const search = async () => {
    setErr(null)
    try {
      const r = await api.identifySearch(title.id, provider, query.trim())
      setCands(r.candidates)
    } catch (e) {
      setErr(errorText(e))
      return false
    }
  }
  return (
    <div className="acc-sub">
      <div className="chips chips--small">
        <button type="button" className={'chip' + (provider === 'tmdb' ? ' chip--on' : '')} onClick={() => setProvider('tmdb')}>
          TMDB
        </button>
        <button type="button" className={'chip' + (provider === 'anilist' ? ' chip--on' : '')} onClick={() => setProvider('anilist')}>
          AniList
        </button>
      </div>
      <Field label="Search">
        <TextInput value={query} onChange={setQuery} />
      </Field>
      <div className="acc-actions">
        <ActionButton kind="grey" onClick={search} disabled={!query.trim()}>
          Search
        </ActionButton>
        <button className="btn btn--outline" onClick={onDone}>
          Close
        </button>
      </div>
      {err && <Notice kind="error">{err}</Notice>}
      {cands && (
        <div className="acc-list acc-list--scroll">
          {cands.length === 0 && <Notice>Nothing found. Try another spelling or the other provider.</Notice>}
          {cands.map((c) => (
            <ListRow
              key={String(c.id)}
              title={
                <span className="prow">
                  {c.poster ? <img className="prow__poster" src={c.poster} alt="" /> : null}
                  <span>
                    {c.title}
                    {c.year ? ` (${c.year})` : ''}
                  </span>
                </span>
              }
              sub={c.overview ? c.overview.slice(0, 140) + (c.overview.length > 140 ? '…' : '') : undefined}
              right={<span className="link-brand">{picked?.id === c.id ? 'Picked' : 'Use'}</span>}
              onClick={() => setPicked(c)}
            />
          ))}
        </div>
      )}
      {picked && (
        <Notice>
          Use <b>{picked.title}</b>
          {picked.year ? ` (${picked.year})` : ''} for "{title.title}"? Its details and art replace what is there.
          <div className="acc-actions acc-actions--tight" style={{ marginTop: 10 }}>
            <ActionButton onClick={() => run(() => api.identifyApply(title.id, provider, picked.id), onDone)}>Yes, use this match</ActionButton>
            <button className="btn btn--grey" onClick={() => setPicked(null)}>
              No
            </button>
          </div>
        </Notice>
      )}
    </div>
  )
}

/**
 * Save sends every field (the server overwrites them all), so the form must start from
 * the title as it is now: it is filled from a fresh fetch, never the page cache (which
 * reopened the form with the values from before the last edit), and Save waits for it
 * (saving early sent an empty synopsis, genres and studio).
 */
function EditDetails({ title, onDone, run }: { title: ManageTitle; onDone: () => void; run: Run }) {
  const [item, setItem] = useState<Item | null>(null)
  const [loadErr, setLoadErr] = useState<string | null>(null)
  const [f, setF] = useState({ title: title.title, year: title.year ? String(title.year) : '', overview: '', genres: '', studio: '', premiered: '', locked: !!title.locked })
  useEffect(() => {
    let live = true
    api.item(title.id).then(
      (it) => {
        if (!live) return
        setItem(it)
        // A series or movie carries its date as premiered; airDate is only set on episodes,
        // so reading it alone left the field empty and saving erased the date.
        const date = it.premiered ?? it.airDate ?? ''
        setF({ title: it.title, year: it.year ? String(it.year) : '', overview: it.overview ?? '', genres: (it.genres ?? []).join(', '), studio: it.studio ?? '', premiered: /^\d{4}-\d{2}-\d{2}/.test(date) ? date.slice(0, 10) : date, locked: !!title.locked })
      },
      (e: unknown) => live && setLoadErr(errorText(e)),
    )
    return () => {
      live = false
    }
    // Only when the title changes: the list reloads every few seconds during a scan and
    // must not refill the form over what is being typed.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [title.id])
  const set = (k: keyof typeof f) => (v: string | boolean) => setF({ ...f, [k]: v })
  return (
    <div className="acc-sub">
      <Field label="Title">
        <TextInput value={f.title} onChange={set('title')} />
      </Field>
      <Field label="Year">
        <TextInput value={f.year} onChange={set('year')} />
      </Field>
      <Field label="Synopsis">
        <TextArea value={f.overview} onChange={set('overview')} />
      </Field>
      <Field label="Genres (comma separated)">
        <TextInput value={f.genres} onChange={set('genres')} />
      </Field>
      <Field label="Studio">
        <TextInput value={f.studio} onChange={set('studio')} />
      </Field>
      <Field label="Premiered (YYYY-MM-DD)">
        <TextInput value={f.premiered} onChange={set('premiered')} />
      </Field>
      <ToggleRow title="Keep my edits" hint="Refreshes leave these fields alone." on={f.locked} onChange={set('locked')} />
      {loadErr ? <Notice kind="error">{loadErr}</Notice> : !item && <div className="ffield__hint">Loading the current details…</div>}
      <div className="acc-actions">
        <ActionButton
          disabled={!item}
          onClick={() =>
            run(
              async () => {
                // Nothing is sent unless both can be read: "20x6" went out as no year at
                // all (and erased the year), and the date went out as typed.
                const year = f.year.trim()
                const premiered = f.premiered.trim()
                if (year && !/^\d{4}$/.test(year)) throw new Error('The year must be four digits, like 2019.')
                if (premiered && !/^\d{4}-\d{2}-\d{2}$/.test(premiered)) throw new Error('The date must look like 2019-04-06 (year-month-day).')
                await api.putMetadata(title.id, {
                  title: f.title.trim(),
                  year: year ? Number(year) : null,
                  overview: f.overview,
                  genres: f.genres
                    .split(',')
                    .map((g) => g.trim())
                    .filter(Boolean),
                  studio: f.studio.trim(),
                  premiered,
                  locked: f.locked,
                })
              },
              () => {
                // The title's own page asks the server again instead of trusting its copy.
                markStale(`item:${title.id}`)
                onDone()
              },
            )
          }
        >
          Save
        </ActionButton>
        <button className="btn btn--grey" onClick={onDone}>
          Cancel
        </button>
      </div>
    </div>
  )
}
