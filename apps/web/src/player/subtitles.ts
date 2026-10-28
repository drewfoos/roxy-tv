// ASS/SSA over the video with jassub (libass in WebAssembly), using the MKV's own fonts.
// Every URL handed to jassub is absolute: Vite's ?url imports resolve to root-relative
// paths that the worker, which fetches the wasm relative to itself, can follow.
import JASSUB from 'jassub'
import workerUrl from 'jassub/dist/jassub-worker.js?url'
import wasmUrl from 'jassub/dist/jassub-worker.wasm?url'
import modernWasmUrl from 'jassub/dist/jassub-worker-modern.wasm?url'
import legacyWasmUrl from 'jassub/dist/jassub-worker.wasm.js?url'
import defaultFontUrl from 'jassub/dist/default.woff2?url'
import { api } from '../api/client'
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

/** One renderer per video element; tracks and fonts are swapped without rebuilding it. */
export function createSubtitleRenderer(video: HTMLVideoElement): SubtitleRenderer {
  let instance: JASSUB | null = null
  let loadedFontsFor = ''
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
      const wantFonts = loadedFontsFor !== sourceId
      const [text, fonts] = await Promise.all([
        api.subtitleText(sourceId, stream.index),
        wantFonts ? Promise.all(fontAttachments(source.attachments).map((a) => api.attachmentBytes(sourceId, a.index).catch(() => null))) : Promise.resolve([] as (Uint8Array | null)[]),
      ])
      if (my !== gen) return
      ensure(text, fonts.filter((f): f is Uint8Array => !!f))
      if (wantFonts) loadedFontsFor = sourceId
    },
    destroy() {
      gen++
      instance?.destroy()
      instance = null
    },
  }
}
