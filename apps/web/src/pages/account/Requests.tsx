import { useState } from 'react'
import { Link } from 'react-router'
import { api } from '../../api/client'
import type { MediaRequest } from '../../api/admin'
import { useAsync } from '../../hooks/useAsync'
import { ActionButton, Notice, PanelHead, TextInput, errorText, formatWhen } from '../../components/Form'
import { Spinner } from '../../components/Skeleton'

type Filter = 'all' | 'waiting' | 'downloading' | 'problems'

const DOWNLOADING = new Set(['downloading', 'importing', 'queued', 'searching', 'stalled'])
// The arr's states in the viewer's words.
const STATE_WORDS: Record<string, string> = { notfound: 'Not found yet', complete: 'Downloaded', downloading: 'Downloading', importing: 'Importing', queued: 'Queued', searching: 'Searching', stalled: 'Stalled', failed: 'Failed', unaired: 'Not out yet', waiting: 'Waiting' }

function isProblem(r: MediaRequest) {
  return r.status === 'failed' || !!r.download?.isProblem
}
function isDownloading(r: MediaRequest) {
  return r.status === 'sent' && DOWNLOADING.has(r.download?.state ?? '')
}

function statusLine(r: MediaRequest): string {
  switch (r.status) {
    case 'approval':
      return 'Waiting for your approval'
    case 'declined':
      return r.error ? `Declined · ${r.error}` : 'Declined'
    case 'failed':
      return r.error ? `Couldn't send · ${r.error}` : "Couldn't send"
    case 'pending':
      return `Waiting for ${r.type === 'movie' ? 'Radarr' : 'Sonarr'}`
  }
  const d = r.download
  if (d?.state) {
    const pct = d.progress != null && d.progress > 0 ? ` · ${Math.round(d.progress * 100)}%` : ''
    const name = STATE_WORDS[d.state.toLowerCase()] ?? d.state.replace(/([a-z])([A-Z])/g, '$1 $2').replace(/^./, (c) => c.toUpperCase())
    return d.message ? `${name}${pct} · ${d.message}` : `${name}${pct}`
  }
  return `Sent to ${r.type === 'movie' ? 'Radarr' : 'Sonarr'}`
}

function asked(r: MediaRequest): string {
  if (r.type === 'movie') return 'Movie'
  const seasons = r.seasons ?? []
  const eps = r.episodes ?? []
  // [-1] marks a request for single episodes only.
  if ((seasons.length === 1 && seasons[0] === -1) || (!seasons.length && eps.length)) return `Series · ${eps.length} episode${eps.length === 1 ? '' : 's'}`
  if (!seasons.length) return 'Series · Whole series'
  return `Series · ${seasons
    .filter((n) => n >= 0)
    .sort((a, b) => a - b)
    .map((n) => (n === 0 ? 'Specials' : `S${n}`))
    .join(', ')}`
}

/** Requests (admin): every Discover request, with approve / decline for the waiting ones. */
export function RequestsPage() {
  const { data, error, loading, reload } = useAsync('requests-admin', () => api.requests())
  const [filter, setFilter] = useState<Filter>('all')
  const [declining, setDeclining] = useState<string | null>(null)
  const [reason, setReason] = useState('')
  const [err, setErr] = useState<string | null>(null)
  const all = data?.requests ?? []
  const counts = { all: all.length, waiting: all.filter((r) => r.status === 'approval').length, downloading: all.filter(isDownloading).length, problems: all.filter(isProblem).length }
  const shown = all.filter((r) => (filter === 'waiting' ? r.status === 'approval' : filter === 'downloading' ? isDownloading(r) : filter === 'problems' ? isProblem(r) : true))

  const act = async (fn: () => Promise<unknown>) => {
    setErr(null)
    try {
      await fn()
      setDeclining(null)
      setReason('')
      reload(true)
    } catch (e) {
      setErr(errorText(e))
    }
  }

  return (
    <section className="acc-form acc-form--wide">
      <PanelHead title="Requests" sub="Everything people asked for in Discover: approvals, what Sonarr and Radarr are fetching, and anything stuck." />
      <div className="chips">
        {(
          [
            ['all', 'All'],
            ['waiting', 'Waiting'],
            ['downloading', 'Downloading'],
            ['problems', 'Problems'],
          ] as [Filter, string][]
        ).map(([id, label]) => (
          <button key={id} type="button" className={'chip' + (filter === id ? ' chip--on' : '')} onClick={() => setFilter(id)}>
            {label} · {counts[id]}
          </button>
        ))}
        {counts.problems > 0 && (
          <ActionButton kind="grey" onClick={() => act(() => api.retryRequests())}>
            Retry failed
          </ActionButton>
        )}
      </div>
      {error && <Notice kind="error">{error}</Notice>}
      {err && <Notice kind="error">{err}</Notice>}
      {loading && !data ? (
        <Spinner />
      ) : shown.length === 0 ? (
        <Notice>{filter === 'all' ? 'Nobody has requested anything yet.' : 'Nothing here.'}</Notice>
      ) : (
        <div className="reqs">
          {shown.map((r) => (
            <div key={r.id} className={'req' + (isProblem(r) ? ' req--problem' : '')}>
              <Link to={`/discover?${r.type === 'movie' ? 'movie' : 'tv'}=${r.tmdbId}`} className="req__poster">
                {r.poster ? <img src={r.poster} alt="" loading="lazy" /> : <span />}
              </Link>
              <div className="req__body">
                <div className="req__title">
                  {r.title}
                  {r.year ? ` (${r.year})` : ''}
                </div>
                <div className="req__sub">{asked(r)}</div>
                <div className="req__sub">
                  {r.userName ?? 'someone'} · {formatWhen(r.createdAt)}
                </div>
                <div className={'req__status' + (r.status === 'approval' ? ' req__status--wait' : '')}>{statusLine(r)}</div>
                {r.status === 'approval' &&
                  (declining === r.id ? (
                    <div className="req__decline">
                      <TextInput value={reason} onChange={setReason} placeholder="Reason (optional)" />
                      <ActionButton kind="grey" onClick={() => act(() => api.declineRequest(r.id, reason.trim() || undefined))}>
                        Decline
                      </ActionButton>
                      <button className="btn btn--outline" onClick={() => setDeclining(null)}>
                        Cancel
                      </button>
                    </div>
                  ) : (
                    <div className="acc-actions acc-actions--tight">
                      <ActionButton onClick={() => act(() => api.approveRequest(r.id))}>Approve</ActionButton>
                      <button className="btn btn--grey" onClick={() => setDeclining(r.id)}>
                        Decline
                      </button>
                    </div>
                  ))}
              </div>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}
