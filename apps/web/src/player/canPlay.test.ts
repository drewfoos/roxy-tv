import { describe, expect, it } from 'vitest'
import { canPlayVideo, NEVER, videoContentType, type Probe } from './canPlay'

// A browser with no HEVC decoder (Chrome without a hardware one, Firefox) was handed the
// HEVC file anyway and the page said "This file cannot be played in this browser". Asked
// first, the file starts as a conversion.
describe('videoContentType', () => {
  it('names HEVC by its bit depth', () => {
    expect(videoContentType({ codec: 'hevc', bitDepth: 10 })).toBe('video/mp4; codecs="hvc1.2.4.L153.B0"')
    expect(videoContentType({ codec: 'hevc', bitDepth: 8 })).toBe('video/mp4; codecs="hvc1.1.6.L153.B0"')
    expect(videoContentType({ codec: 'HEVC' })).toBe('video/mp4; codecs="hvc1.1.6.L153.B0"')
  })
  it('tells 10-bit H.264, which no browser decodes, from the ordinary kind', () => {
    expect(videoContentType({ codec: 'h264', bitDepth: 8 })).toBe('video/mp4; codecs="avc1.640033"')
    expect(videoContentType({ codec: 'h264', bitDepth: 10 })).toBe('video/mp4; codecs="avc1.6E0033"')
  })
  it('names AV1 and VP9', () => {
    expect(videoContentType({ codec: 'av1', bitDepth: 10 })).toBe('video/mp4; codecs="av01.0.13M.10"')
    expect(videoContentType({ codec: 'vp9' })).toBe('video/mp4; codecs="vp09.00.51.08"')
  })
  it('knows the codecs no browser has', () => {
    for (const codec of ['mpeg2video', 'mpeg4', 'vc1', 'wmv3']) expect(videoContentType({ codec }), codec).toBe(NEVER)
  })
  it('has nothing to ask about a codec it does not know', () => {
    expect(videoContentType({ codec: 'somethingnew' })).toBeNull()
    expect(videoContentType({})).toBeNull()
  })
})

const probe = (decoding: { supported: boolean; smooth: boolean } | null, canPlayType = ''): Probe & { asked: string[] } => {
  const asked: string[] = []
  return { asked, decoding: async (type) => { asked.push(type); return decoding }, canPlayType: (type) => { asked.push('cpt:' + type); return canPlayType } }
}
const film = { codec: 'hevc', bitDepth: 10, width: 3840, height: 1604, fps: 23.976 }

describe('canPlayVideo', () => {
  it('is yes when the browser decodes it smoothly', async () => {
    expect(await canPlayVideo(film, probe({ supported: true, smooth: true }))).toBe(true)
  })
  it('is no when the browser has no decoder for it', async () => {
    expect(await canPlayVideo(film, probe({ supported: false, smooth: false }))).toBe(false)
  })
  it('is no when the browser would decode it with stutter', async () => {
    expect(await canPlayVideo(film, probe({ supported: true, smooth: false }))).toBe(false)
  })
  it('asks the older way when the browser cannot say', async () => {
    const p = probe(null, 'probably')
    expect(await canPlayVideo(film, p)).toBe(true)
    expect(p.asked).toEqual(['video/mp4; codecs="hvc1.2.4.L153.B0"', 'cpt:video/mp4; codecs="hvc1.2.4.L153.B0"'])
    expect(await canPlayVideo(film, probe(null, ''))).toBe(false)
  })
  it('does not ask about a codec no browser has, and says no', async () => {
    const p = probe({ supported: true, smooth: true }, 'probably')
    expect(await canPlayVideo({ codec: 'mpeg2video' }, p)).toBe(false)
    expect(p.asked).toEqual([])
  })
  it('tries a file it knows nothing about as it is', async () => {
    const p = probe({ supported: false, smooth: false })
    expect(await canPlayVideo({ codec: 'somethingnew' }, p)).toBe(true)
    expect(await canPlayVideo(undefined, p)).toBe(true)
    expect(p.asked).toEqual([])
  })
})
