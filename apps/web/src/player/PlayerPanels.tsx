import type { MediaStream } from '../api/types'
import { IconCheckCircle } from '../components/Icons'
import { trackLabel } from './format'
import { resolutionLabel, type QualityStep } from './stream'

/** Quality choices (Crunchyroll's gear): the file itself, or a server H.264 encode at a height. */
export type Quality = 'original' | QualityStep

/** Audio and Subtitles/CC side by side; "None" turns subtitles off. */
export function TracksPanel({ audioTracks, audioIndex, onAudio, subTracks, subIndex, subsOn, onSub }: {
  audioTracks: MediaStream[]
  audioIndex: number | null
  onAudio: (index: number) => void
  subTracks: MediaStream[]
  subIndex: number | null
  subsOn: boolean
  onSub: (index: number | null) => void
}) {
  return (
    <div className="panel panel--tracks">
      <div className="panel__col">
        <div className="panel__head">Audio</div>
        <div className="panel__list">
          {audioTracks.map((t) => (
            <button key={t.index} className={'panel__row' + (t.index === audioIndex ? ' panel__row--on' : '')} onClick={() => onAudio(t.index)}>
              <span>{trackLabel(t)}</span>
              {t.index === audioIndex && <IconCheckCircle size={22} />}
            </button>
          ))}
        </div>
      </div>
      <div className="panel__col">
        <div className="panel__head">Subtitles/CC</div>
        <div className="panel__list">
          {subTracks.map((t) => (
            <button key={t.index} className={'panel__row' + (subsOn && t.index === subIndex ? ' panel__row--on' : '')} onClick={() => onSub(t.index)}>
              <span>{trackLabel(t)}</span>
              {subsOn && t.index === subIndex && <IconCheckCircle size={22} />}
            </button>
          ))}
          <button className={'panel__row' + (!subsOn ? ' panel__row--on' : '')} onClick={() => onSub(null)}>
            <span>None</span>
            {!subsOn && <IconCheckCircle size={22} />}
          </button>
        </div>
      </div>
    </div>
  )
}

/** Playback speed choices. */
export function SpeedPanel({ speeds, rate, onRate }: { speeds: number[]; rate: number; onRate: (rate: number) => void }) {
  return (
    <div className="panel">
      <div className="panel__head">Playback Speed</div>
      <div className="panel__list">
        {speeds.map((s) => (
          <button key={s} className={'panel__row' + (s === rate ? ' panel__row--on' : '')} onClick={() => onRate(s)}>
            <span>{s}x</span>
            {s === rate && <IconCheckCircle size={22} />}
          </button>
        ))}
      </div>
    </div>
  )
}

/** The line under a quality: what picking it does. [standIn]: the row that plays in place of a file this browser cannot play. */
export function qualityHint(q: Quality, standIn = false): string {
  if (q === 'original') return 'The file as it is, nothing re-encoded.'
  if (standIn) return 'Converted by the server: this browser cannot play the file itself.'
  if (q > 1080) return 'A smaller picture, made by the server as it plays.'
  return q === 480 ? 'Uses the least data. Good for slower connections.' : 'Uses less data at a standard quality.'
}

/** The gear: Autoplay Next, then the quality choices with what each costs. [srcHeight]: the file's classHeight. */
export function SettingsPanel({ autoplay, onAutoplay, qualities, quality, srcHeight, onQuality, playable = true }: {
  autoplay: boolean
  onAutoplay: () => void
  qualities: Quality[]
  quality: Quality
  srcHeight: number
  onQuality: (q: Quality) => void
  /** False when the browser cannot play the file itself: the first row is then its stand-in. */
  playable?: boolean
}) {
  return (
    <div className="panel panel--settings">
      <div className="panel__head">Settings</div>
      <button className="panel__row panel__row--toggle" onClick={onAutoplay}>
        <span>Autoplay Next</span>
        <span className={'toggle' + (autoplay ? ' toggle--on' : '')} aria-hidden="true">
          <i />
        </span>
      </button>
      <div className="panel__head">Quality</div>
      <div className="panel__list">
        {qualities.map((q) => (
          <button key={q} className={'panel__row panel__row--tall' + (q === quality ? ' panel__row--on' : '')} onClick={() => onQuality(q)}>
            <span>
              <b>{q === 'original' ? (srcHeight ? `${resolutionLabel(srcHeight)} (Original)` : 'Original') : resolutionLabel(q)}</b>
              <small>{qualityHint(q, !playable && q === qualities[0])}</small>
            </span>
            {q === quality && <IconCheckCircle size={22} />}
          </button>
        ))}
      </div>
    </div>
  )
}
