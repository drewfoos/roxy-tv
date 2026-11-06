// ASS/SSA over the video with jassub (libass in WebAssembly), using the MKV's own fonts.
// Every URL handed to jassub is absolute: Vite's ?url imports resolve to root-relative
// paths that the worker, which fetches the wasm relative to itself, can follow.
import JASSUB from 'jassub'
import workerUrl from 'jassub/dist/jassub-worker.js?url'
import wasmUrl from 'jassub/dist/jassub-worker.wasm?url'
import modernWasmUrl from 'jassub/dist/jassub-worker-modern.wasm?url'
import legacyWasmUrl from 'jassub/dist/jassub-worker.wasm.js?url'
import defaultFontUrl from 'jassub/dist/default.woff2?url'
import { api, ApiError } from '../api/client'
import type { Attachment, MediaStream } from '../api/types'

const abs = (u: string) => new URL(u, window.location.href).href

const FONT_MIME = /font|ttf|otf|truetype|opentype|woff/i
const FONT_EXT = /\.(ttf|otf|ttc|woff2?)$/i

export function fontAttachments(attachments: Attachment[] | undefined): Attachment[] {
  return (attachments ?? []).filter((a) => FONT_MIME.test(a.mime ?? '') || FONT_EXT.test(a.filename ?? ''))
}

export interface SubtitleRenderer {
  setTrack(sourceId: string, stream: MediaStream | null): Promise<void>
  /** The stream's clock starts this many seconds into the file (a remux started mid-file). */
  setOffset(seconds: number): void
  destroy(): void
}

/**
 * One font from the file, retried once unless the server said it is not there (a 4xx
 * other than timeout / rate limit): a dropped connection should not cost a font.
 */
async function fetchFont(sourceId: string, a: Attachment): Promise<Uint8Array | null> {
  for (let attempt = 0; ; attempt++) {
    try {
      return await api.attachmentBytes(sourceId, a.index)
    } catch (e) {
      const final = e instanceof ApiError && e.status >= 400 && e.status < 500 && e.status !== 408 && e.status !== 429
      if (final || attempt >= 1) {
        console.warn(`subtitle font ${a.filename || a.index} did not load:`, e)
        return null
      }
      await new Promise((r) => setTimeout(r, 600))
    }
  }
}

/**
 * One renderer per video element; tracks and fonts are swapped without rebuilding it.
 * [onFontsFailed] hears the names of fonts that could not be loaded: libass then draws
 * those styles in the fallback font, so signs and karaoke can look wrong.
 */
export function createSubtitleRenderer(video: HTMLVideoElement, onFontsFailed?: (names: string[]) => void): SubtitleRenderer {
  let instance: JASSUB | null = null
  // Fonts handed to libass, as "source/index". Only fonts that loaded are recorded, so a
  // failed one is tried again with the next track (it used to be skipped for good).
  const loadedFonts = new Set<string>()
  let gen = 0
  let offset = 0

  const ensure = (subContent: string, fonts: Uint8Array[]) => {
    if (instance) {
      instance.setTrack(subContent)
      for (const f of fonts) instance.addFont(f)
      return
    }
    instance = new JASSUB({
      video,
      subContent,
      fonts,
      workerUrl: abs(workerUrl),
      wasmUrl: abs(wasmUrl),
      modernWasmUrl: abs(modernWasmUrl),
      legacyWasmUrl: abs(legacyWasmUrl),
      availableFonts: { 'liberation sans': abs(defaultFontUrl) },
      fallbackFont: 'liberation sans',
      useLocalFonts: false,
      prescaleFactor: 1,
    })
    instance.timeOffset = offset
  }

  return {
    setOffset(seconds) {
      offset = seconds
      if (instance) instance.timeOffset = seconds
    },
    async setTrack(sourceId, stream) {
      const my = ++gen
      if (!stream) {
        instance?.freeTrack()
        return
      }
      const source = await api.source(sourceId)
      const wanted = fontAttachments(source.attachments).filter((a) => !loadedFonts.has(`${sourceId}/${a.index}`))
      const [text, fonts] = await Promise.all([api.subtitleText(sourceId, stream.index), Promise.all(wanted.map((a) => fetchFont(sourceId, a)))])
      if (my !== gen) return
      ensure(text, fonts.filter((f): f is Uint8Array => !!f))
      const failed: string[] = []
      wanted.forEach((a, i) => (fonts[i] ? loadedFonts.add(`${sourceId}/${a.index}`) : failed.push(a.filename || `font ${a.index}`)))
      if (failed.length) onFontsFailed?.(failed)
    },
    destroy() {
      gen++
      instance?.destroy()
      instance = null
    },
  }
}
