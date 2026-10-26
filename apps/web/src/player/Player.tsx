import { useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import { pickSubtitle, primarySource } from '../api/items'
import type { Item, MediaSegment } from '../api/types'
import { createSubtitleRenderer } from './subtitles'

export interface PlayerProps {
  item: Item
  /** Start here (seconds); the saved position is used when omitted. */
  startSeconds?: number
  onEnded?: () => void
  /** Progress the server has accepted, so the page can update its own state. */
  onProgress?: (positionMs: number) => void
}

/**
 * Direct play in the browser's own decoder, no transcoding: the file streams from
 * /v1/stream with a media token, jassub draws the ASS track with the MKV's fonts, and
 * progress goes to the server every ten seconds and on pause or leave. The browser's
 * controls are used for now; Roxy's own YouTube-style bar is the next milestone.
 */
export function Player({ item, startSeconds, onEnded, onProgress }: PlayerProps) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const [error, setError] = useState<string | null>(null)
  const [note, setNote] = useState<string | null>(null)
  const [skip, setSkip] = useState<MediaSegment | null>(null)
  const source = primarySource(item)

  useEffect(() => {
    const video = videoRef.current
    if (!video || !source) return
    let live = true
    setError(null)
    const subs = createSubtitleRenderer(video)
    const sub = pickSubtitle(source)
    let lastSent = 0
    let lastPos = 0

    const report = (positionMs: number, force = false) => {
      const now = Date.now()
      if (!force && now - lastSent < 10_000) return
      lastSent = now
      lastPos = positionMs
      api.progress(item.id, Math.round(positionMs)).then(() => onProgress?.(positionMs)).catch(() => undefined)
    }

    ;(async () => {
      try {
        const { token } = await api.mediaToken(source.id)
        if (!live) return
        const start = startSeconds ?? (item.userData?.played ? 0 : (item.userData?.positionMs ?? 0) / 1000)
        video.src = api.streamUrl(source.id, token)
        if (start > 5) video.currentTime = start
        subs.setTrack(source.id, sub).catch((e: unknown) => setNote('Subtitles failed: ' + (e instanceof Error ? e.message : String(e))))
        await video.play().catch(() => undefined)
      } catch (e) {
        if (live) setError(e instanceof Error ? e.message : String(e))
      }
    })()

    const onTime = () => {
      const t = video.currentTime * 1000
      if (!video.paused) report(t)
      const seg = (source.segments ?? []).find((s) => (s.type === 'intro' || s.type === 'outro') && t >= s.startMs && t < s.endMs - 1000)
      setSkip(seg ?? null)
    }
    const onPause = () => report(video.currentTime * 1000, true)
    const onError = () => {
      const code = video.error?.code
      setError(code === 4 ? 'This file cannot be played in this browser (unsupported video or audio codec).' : `Playback error ${code ?? ''}`.trim())
    }
    const onEnd = () => {
      report(video.duration * 1000, true)
      onEnded?.()
    }
    const onLeave = () => {
      if (lastPos > 0) report(video.currentTime * 1000, true)
    }
    video.addEventListener('timeupdate', onTime)
    video.addEventListener('pause', onPause)
    video.addEventListener('error', onError)
    video.addEventListener('ended', onEnd)
    window.addEventListener('beforeunload', onLeave)
    return () => {
      live = false
      report(video.currentTime * 1000, true)
      video.removeEventListener('timeupdate', onTime)
      video.removeEventListener('pause', onPause)
      video.removeEventListener('error', onError)
      video.removeEventListener('ended', onEnd)
      window.removeEventListener('beforeunload', onLeave)
      subs.destroy()
      video.removeAttribute('src')
      video.load()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [item.id, source?.id])

  const doSkip = () => {
    const v = videoRef.current
    if (v && skip) v.currentTime = skip.endMs / 1000
  }

  return (
    <div className="player">
      <video ref={videoRef} controls playsInline preload="metadata" />
      {note && <div className="player__note">{note}</div>}
      {skip && (
        <button className="btn btn--white player__skip" onClick={doSkip}>
          {skip.type === 'intro' ? 'Skip intro' : 'Skip outro'}
        </button>
      )}
      {error && <div className="player__error">{error}</div>}
      {!source && <div className="player__error">No media for this title.</div>}
    </div>
  )
}
