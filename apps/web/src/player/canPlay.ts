// Whether this browser can play a file's video as it is. Browsers have no decoder for
// some of what a library holds (HEVC without a hardware decoder, 10-bit H.264, MPEG-2),
// and the player used to find out by failing: "This file cannot be played in this
// browser". Asked first, such a file starts as a conversion instead.

export interface VideoFacts {
  codec?: string
  profile?: string
  bitDepth?: number
  width?: number
  height?: number
  fps?: number
  bitrate?: number
}

/** A codec no browser decodes: no question needs asking. */
export const NEVER = 'never'

/**
 * The type a browser is asked about for a video stream; NEVER for codecs none of them
 * decode; null when the codec is not known (the file is then tried as it is). Levels are
 * the highest that matter here (5.1): a browser that decodes the codec decodes that.
 */
export function videoContentType(s: VideoFacts): string | null {
  const codec = (s.codec ?? '').toLowerCase()
  const deep = (s.bitDepth ?? 8) > 8
  const type = (codecs: string) => `video/mp4; codecs="${codecs}"`
  switch (codec) {
    case 'h264':
    case 'avc':
    case 'avc1':
      // High 10 ("Hi10P"): common in older fansubs, decoded by no browser.
      return type(deep ? 'avc1.6E0033' : 'avc1.640033')
    case 'hevc':
    case 'h265':
    case 'hvc1':
    case 'hev1':
      return type(deep ? 'hvc1.2.4.L153.B0' : 'hvc1.1.6.L153.B0')
    case 'av1':
      return type(deep ? 'av01.0.13M.10' : 'av01.0.13M.08')
    case 'vp9':
      return type(deep ? 'vp09.02.51.10' : 'vp09.00.51.08')
    case 'vp8':
      return 'video/webm; codecs="vp8"'
    case 'mpeg2video':
    case 'mpeg1video':
    case 'mpeg4':
    case 'msmpeg4v3':
    case 'vc1':
    case 'wmv3':
    case 'rv40':
    case 'vvc':
      return NEVER
    case '':
      return null
    default:
      return null
  }
}

/** How the browser is asked; replaced in tests. */
export interface Probe {
  /** MediaCapabilities' answer, or null when the browser has none to give. */
  decoding(type: string, s: VideoFacts): Promise<{ supported: boolean; smooth: boolean } | null>
  /** canPlayType's answer. */
  canPlayType(type: string): string
}

const browser: Probe = {
  async decoding(type, s) {
    const mc = typeof navigator !== 'undefined' ? navigator.mediaCapabilities : undefined
    if (!mc?.decodingInfo) return null
    try {
      const info = await mc.decodingInfo({
        type: 'file',
        video: {
          contentType: type,
          width: s.width && s.width > 0 ? s.width : 1920,
          height: s.height && s.height > 0 ? s.height : 1080,
          bitrate: s.bitrate && s.bitrate > 0 ? s.bitrate : 8_000_000,
          framerate: s.fps && s.fps > 0 ? s.fps : 24,
        },
      })
      return { supported: info.supported, smooth: info.smooth }
    } catch {
      return null // a type the browser does not even parse: canPlayType decides
    }
  },
  canPlayType(type) {
    return typeof document === 'undefined' ? '' : document.createElement('video').canPlayType(type)
  },
}

const known = new Map<string, Promise<boolean>>()

/**
 * Whether the browser plays this video as it is. "Supported but not smooth" (a picture too
 * large for its decoder) counts as no: the conversion plays without stutter. Unknown
 * codecs count as yes, and are found out the old way (see the player's own fallback).
 */
export function canPlayVideo(s: VideoFacts | undefined, probe: Probe = browser): Promise<boolean> {
  if (!s) return Promise.resolve(true)
  const type = videoContentType(s)
  if (type === null) return Promise.resolve(true)
  if (type === NEVER) return Promise.resolve(false)
  const key = `${type}|${s.width ?? 0}x${s.height ?? 0}`
  const ask = async () => {
    const d = await probe.decoding(type, s)
    if (d) return d.supported && d.smooth
    return probe.canPlayType(type) !== ''
  }
  if (probe !== browser) return ask()
  let answer = known.get(key)
  if (!answer) {
    answer = ask()
    known.set(key, answer)
  }
  return answer
}
