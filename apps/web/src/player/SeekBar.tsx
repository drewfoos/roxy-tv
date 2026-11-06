import { useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from 'react'
import { api } from '../api/client'
import type { MediaSegment, TrickplayManifest } from '../api/types'
import { fmt } from './format'

export interface SeekBarProps {
  sourceId: string
  duration: number
  current: number
  buffered: number
  segments: MediaSegment[]
  trick: TrickplayManifest | null
  onSeek: (t: number) => void
}

/**
 * The bar across the top of the controls: buffered and played, a round thumb, the intro /
 * outro segments, and on hover the time with a trickplay still from the sprite sheets.
 */
export function SeekBar({ sourceId, duration, current, buffered, segments, trick, onSeek }: SeekBarProps) {
  const ref = useRef<HTMLDivElement>(null)
  const [hover, setHover] = useState<{ x: number; t: number } | null>(null)
  const barTime = (e: ReactMouseEvent) => {
    const bar = ref.current
    if (!bar || !duration) return null
    const r = bar.getBoundingClientRect()
    const f = Math.max(0, Math.min(1, (e.clientX - r.left) / r.width))
    return { x: e.clientX - r.left, t: f * duration }
  }
  const preview = useMemo(() => {
    if (!hover || !trick || trick.count === 0) return null
    const tile = Math.min(trick.count - 1, Math.max(0, Math.floor((hover.t * 1000) / trick.intervalMs)))
    const per = trick.columns * trick.rows
    const sheet = Math.floor(tile / per)
    const col = (tile % per) % trick.columns
    const row = Math.floor((tile % per) / trick.columns)
    return { sheet, x: col * trick.width, y: row * trick.height }
  }, [hover, trick])
  const played = duration ? Math.min(1, current / duration) : 0
  const buff = duration ? Math.min(1, buffered / duration) : 0
  return (
    <div
      ref={ref}
      className="seek"
      onMouseMove={(e) => setHover(barTime(e))}
      onMouseLeave={() => setHover(null)}
      onClick={(e) => {
        const h = barTime(e)
        if (h) onSeek(h.t)
      }}
    >
      <div className="seek__track">
        <div className="seek__buffered" style={{ width: `${buff * 100}%` }} />
        <div className="seek__played" style={{ width: `${played * 100}%` }} />
      </div>
      <div className="seek__thumb" style={{ left: `${played * 100}%` }} />
      {segments.map((s, i) =>
        duration ? <div key={i} className="seek__segment" style={{ left: `${(s.startMs / 1000 / duration) * 100}%`, width: `${((s.endMs - s.startMs) / 1000 / duration) * 100}%` }} /> : null,
      )}
      {hover && (
        <div className="seek__hover" style={{ left: hover.x }}>
          {preview && trick && (
            <div className="seek__thumbnail" style={{ width: trick.width, height: trick.height, backgroundImage: `url(${api.trickplaySheetUrl(sourceId, preview.sheet)})`, backgroundPosition: `-${preview.x}px -${preview.y}px` }} />
          )}
          <div className="seek__time">{fmt(hover.t)}</div>
        </div>
      )}
    </div>
  )
}
