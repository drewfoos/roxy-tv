import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router'
import { api, px } from '../../api/client'
import type { ActiveSession, ActiveTrack } from '../../api/admin'
import { episodeCode, isEpisode, isMovie } from '../../api/items'
import { useAsync, usePoll } from '../../hooks/useAsync'
import { Notice, PanelHead } from '../../components/Form'
import { IconPause, IconPlay } from '../../components/Icons'
import { Spinner } from '../../components/Skeleton'
import { Avatar } from '../../components/TopBar'
import { fmt } from '../../player/format'
import { METHODS, audioText, livePositionMs, outputText, subtitleText, tally, timeLeft, videoText } from './nowPlaying'

const CLIENTS: Record<string, string> = { 'roxy-web': 'Roxy web', Roxy: 'Roxy phone', 'Roxy TV': 'Roxy TV' }
const NETWORKS: Record<string, string> = { home: 'Home network', tailscale: 'Tailscale', server: 'This server', internet: 'Internet' }

/** How often the list is asked for again while the page is in view. */
const REFRESH_MS = 5000

/**
 * Now playing (admin): everything being watched from the server right now, any login's:
 * who, on what, where they are in it, and whether the server sends the file as it is,
 * repackages it or converts it (and why). Jellyfin's dashboard has the same on its
 * "Active devices" cards; here the detail is on the card, not behind a button.
 */
export function NowPlayingPage() {
  const { data, error, loading, reload } = useAsync('now-playing', () => api.activeSessions())
  // When the answer on screen arrived: playing sessions are counted on from it.
  const gotAt = useRef(Date.now())
  const [, setTick] = useState(0)
  useEffect(() => {
    gotAt.current = Date.now()
  }, [data])
  usePoll(reload, REFRESH_MS)
  useEffect(() => {
    const second = window.setInterval(() => {
      if (!document.hidden) setTick((n) => n + 1)
    }, 1000)
    // Back to the tab: the list on screen is as old as the time away.
    const onShow = () => {
      if (!document.hidden) reload(true)
    }
    document.addEventListener('visibilitychange', onShow)
    return () => {
      window.clearInterval(second)
      document.removeEventListener('visibilitychange', onShow)
    }
  }, [reload])

  const sessions = data?.sessions ?? []
  const since = Date.now() - gotAt.current
  return (
    <section className="acc-form acc-form--wide np">
      <PanelHead title="Now Playing" sub="Who is watching what right now, and how the server is sending it." />
      {error && !data && <Notice kind="error">{error}</Notice>}
      {loading && !data ? (
        <Spinner />
      ) : data ? (
        <>
          <div className="np-stats" aria-live="polite">
            <Stat value={String(sessions.length)} label={sessions.length === 1 ? 'Stream' : 'Streams'} sub={tally(sessions) || 'Nothing playing'} />
            <Stat value={`${data.converting} of ${data.convertingMax}`} label="Converting" sub={data.converting > 0 ? `Using ${data.encoder}` : 'No video is being converted'} busy={data.converting > 0} />
            <Stat value={data.encoder.replace(/ \(.*\)$/, '')} label="Encoder" sub="Used when a player asks for a lower quality" />
          </div>
          {sessions.length === 0 ? (
            <div className="np-empty">
              <span className="np-empty__icon">
                <IconPlay size={30} />
              </span>
              <b>Nothing is playing</b>
              <p>When someone starts watching, it shows up here within a few seconds.</p>
            </div>
          ) : (
            <div className="np-list">
              {sessions.map((s) => (
                <SessionCard key={s.id} s={s} sinceAnswerMs={since} />
              ))}
            </div>
          )}
        </>
      ) : null}
      <p className="acc-foot">Updates every few seconds. Device addresses are never shown here; where a device is (home, Tailscale, internet) is.</p>
    </section>
  )
}

function Stat({ value, label, sub, busy = false }: { value: string; label: string; sub: string; busy?: boolean }) {
  return (
    <div className={'np-stat' + (busy ? ' np-stat--busy' : '')}>
      <span className="np-stat__label">{label}</span>
      <b className="np-stat__value">{value}</b>
      <span className="np-stat__sub">{sub}</span>
    </div>
  )
}

function SessionCard({ s, sinceAnswerMs }: { s: ActiveSession; sinceAnswerMs: number }) {
  const it = s.item
  const pos = livePositionMs(s, sinceAnswerMs)
  const frac = s.durationMs > 0 ? Math.min(pos / s.durationMs, 1) : 0
  const left = timeLeft(pos, s.durationMs)
  const playing = s.state === 'playing'
  const movie = isMovie(it)
  const art = movie ? (api.imageUrl(it, 'backdrop', px(320)) ?? api.imageUrl(it, 'poster', px(320))) : (api.imageUrl(it, 'thumb', px(320)) ?? api.imageUrl(it, 'backdrop', px(320)))
  const page = isEpisode(it) && it.seriesId ? `/series/${it.seriesId}` : movie ? `/movie/${it.id}` : null
  const over = isEpisode(it) ? it.seriesTitle : null
  const title = isEpisode(it) ? [episodeCode(it), it.title].filter(Boolean).join(' - ') : it.title
  const who = s.profile.name || s.user.name || 'Someone'
  const login = s.user.name && s.user.name !== s.profile.name ? s.user.name : null
  const device = s.device.name || CLIENTS[s.device.client ?? ''] || s.device.client || 'Unknown device'
  const app = [CLIENTS[s.device.client ?? ''] ?? s.device.client, s.device.version].filter(Boolean).join(' ')
  const method = METHODS[s.play.method] ?? METHODS['']
  return (
    <article className={'np-card np-card--' + s.state + ' np-card--by-' + (s.play.method || 'unknown')}>
      <div className="np-card__top">
        <div className="np-card__art">
          {art ? <img src={art} alt="" loading="lazy" /> : <span className="np-card__noart">{it.title}</span>}
          <span className={'np-state np-state--' + s.state}>
            {playing ? <IconPlay size={14} /> : <IconPause size={14} />}
            {playing ? 'Playing' : 'Paused'}
          </span>
          <div className="progress" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(frac * 100)}>
            <i style={{ width: `${frac * 100}%` }} />
          </div>
        </div>
        <div className="np-card__what">
          {over && <span className="np-card__over">{over}</span>}
          <h2 className="np-card__title">{page ? <Link to={page}>{title}</Link> : title}</h2>
          <div className="np-card__time">
            <span className="np-card__clock">
              {fmt(pos / 1000)}
              {s.durationMs > 0 && <i> / {fmt(s.durationMs / 1000)}</i>}
            </span>
            {left && <span>{left}</span>}
          </div>
          <div className="np-who">
            <Avatar name={who} url={s.profile.id ? api.profileAvatarUrl(s.profile.id, px(32) * 2) : null} size={32} />
            <div className="np-who__text">
              <b>
                {who}
                {login && <i> ({login})</i>}
              </b>
              <span>{[device, NETWORKS[s.device.network ?? ''], app && app !== device ? app : null].filter(Boolean).join(' · ')}</span>
            </div>
          </div>
        </div>
      </div>
      <div className="np-play">
        <div className="np-play__head">
          <span className={'np-method np-method--' + (s.play.method || 'unknown') + (s.play.sending && playing ? ' np-method--live' : '')}>
            <i />
            {method.label}
          </span>
          <p>{s.play.reason || method.plain}</p>
        </div>
        <dl className="np-tracks">
          {s.play.video && <TrackRow label="Video" from={videoText(s.play.video)} track={s.play.video} />}
          {s.play.audio && <TrackRow label="Audio" from={audioText(s.play.audio)} track={s.play.audio} />}
          {s.play.subtitle ? <TrackRow label="Subtitles" from={subtitleText(s.play.subtitle)} track={s.play.subtitle} /> : s.play.subtitlesOff ? <TrackRow label="Subtitles" from="Off" /> : null}
        </dl>
      </div>
    </article>
  )
}

/** One track: what it is in the file, then either "as it is" or what it is converted to. */
function TrackRow({ label, from, track }: { label: string; from: string; track?: ActiveTrack }) {
  const to = track?.to
  return (
    <div className="np-track">
      <dt>{label}</dt>
      <dd>
        <span>{from}</span>
        {to && (
          <>
            <span className="np-track__arrow" aria-label="converted to">
              →
            </span>
            <span className="np-track__to">{outputText(to)}</span>
            {to.encoder && (
              <span className={'tag' + (to.hardware ? '' : ' tag--quiet')} title={to.encoder}>
                {to.hardware ? 'GPU' : 'CPU'}
              </span>
            )}
          </>
        )}
      </dd>
    </div>
  )
}
