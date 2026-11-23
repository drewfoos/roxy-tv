// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { MediaStream } from '../api/types'

// What the renderer (libass) was handed: it reads ASS only, and says nothing when it is
// given something else. A SubRip track went to it as it was and drew nothing.
const { made, setTracks, api } = vi.hoisted(() => ({
  made: [] as { subContent: string; fonts: Uint8Array[] }[],
  setTracks: [] as string[],
  api: { source: vi.fn(), subtitleText: vi.fn(), attachmentBytes: vi.fn(async (_source: string, _index: number) => new Uint8Array([1, 2, 3])) },
}))
vi.mock('jassub', () => ({
  default: class {
    timeOffset = 0
    constructor(o: { subContent: string; fonts: Uint8Array[] }) {
      made.push({ subContent: o.subContent, fonts: o.fonts })
    }
    setTrack(text: string) {
      setTracks.push(text)
    }
    addFont() {}
    freeTrack() {}
    destroy() {}
  },
}))

vi.mock('../api/client', () => ({ api, ApiError: class extends Error {} }))

import { createSubtitleRenderer } from './subtitles'

const SRT = '1\n00:00:01,000 --> 00:00:02,500\nHello there\n'
const ASS = '[Script Info]\nScriptType: v4.00+\n\n[Events]\nDialogue: 0,0:00:01.00,0:00:02.50,Default,,0,0,0,,Hello there\n'
const film = {
  id: 'src',
  attachments: [{ index: 9, filename: 'Sign.ttf', mime: 'font/ttf' }],
  streams: [
    { index: 0, type: 'video', codec: 'hevc', width: 3840, height: 1604 },
    { index: 3, type: 'subtitle', codec: 'subrip', isTextFormat: true },
    { index: 4, type: 'subtitle', codec: 'ass', isTextFormat: true },
  ],
}
const track = (index: number) => film.streams.find((s) => s.index === index) as MediaStream

describe('the subtitle renderer', () => {
  beforeEach(() => {
    vi.spyOn(console, 'warn').mockImplementation(() => {}) // a font that fails is logged
    made.length = 0
    setTracks.length = 0
    api.source.mockReset().mockResolvedValue(film)
    api.subtitleText.mockReset()
    api.attachmentBytes.mockClear()
  })

  it('is handed a SubRip track as ASS, in the proportions of the picture', async () => {
    api.subtitleText.mockResolvedValue(SRT)
    await createSubtitleRenderer(document.createElement('video')).setTrack('src', track(3))
    expect(made).toHaveLength(1)
    expect(made[0].subContent).toMatch(/^\[Script Info\]/)
    expect(made[0].subContent).toContain('PlayResY: 802')
    expect(made[0].subContent).toContain('Dialogue: 0,0:00:01.00,0:00:02.50,Default,,0,0,0,,Hello there')
    // The file's fonts are for its styled tracks: a plain one does not fetch them.
    expect(api.attachmentBytes).not.toHaveBeenCalled()
    expect(made[0].fonts).toEqual([])
  })

  it('is handed an ASS track as it is, with the file fonts', async () => {
    api.subtitleText.mockResolvedValue(ASS)
    const failed = vi.fn()
    await createSubtitleRenderer(document.createElement('video'), failed).setTrack('src', track(4))
    expect(made[0].subContent).toBe(ASS)
    expect(api.attachmentBytes).toHaveBeenCalledWith('src', 9)
    expect(made[0].fonts).toHaveLength(1)
    expect(failed).not.toHaveBeenCalled()
  })

  it('a track said to be plain that is ASS after all gets the fonts too', async () => {
    api.subtitleText.mockResolvedValue(ASS)
    await createSubtitleRenderer(document.createElement('video')).setTrack('src', track(3))
    expect(made[0].subContent).toBe(ASS)
    expect(api.attachmentBytes).toHaveBeenCalledWith('src', 9)
  })

  it('a plain track after a styled one is converted too, and reports no missing font', async () => {
    const failed = vi.fn()
    const subs = createSubtitleRenderer(document.createElement('video'), failed)
    api.attachmentBytes.mockRejectedValueOnce(new Error('gone')).mockRejectedValueOnce(new Error('gone'))
    api.subtitleText.mockResolvedValue(ASS)
    await subs.setTrack('src', track(4))
    expect(failed).toHaveBeenCalledTimes(1) // the styled track's font did not load
    api.subtitleText.mockResolvedValue(SRT)
    await subs.setTrack('src', track(3))
    expect(setTracks).toHaveLength(1)
    expect(setTracks[0]).toMatch(/^\[Script Info\]/)
    expect(failed).toHaveBeenCalledTimes(1)
  })
})
