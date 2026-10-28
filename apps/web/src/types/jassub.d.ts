// jassub 1.8.x ships JavaScript with JSDoc; this is the slice of its surface Roxy uses.
declare module 'jassub' {
  export interface JassubOptions {
    video?: HTMLVideoElement
    canvas?: HTMLCanvasElement
    subUrl?: string
    subContent?: string
    /** Font files (URLs or bytes) loaded up front: the MKV's attachments. */
    fonts?: (string | Uint8Array)[]
    /** Fonts by family name, loaded on demand. */
    availableFonts?: Record<string, string>
    fallbackFont?: string
    /** Every URL must be absolute: the worker resolves them against itself. */
    workerUrl: string
    wasmUrl?: string
    modernWasmUrl?: string
    legacyWasmUrl?: string
    useLocalFonts?: boolean
    offscreenRender?: boolean
    onDemandRender?: boolean
    prescaleFactor?: number
    debug?: boolean
  }
  export default class JASSUB {
    constructor(options: JassubOptions)
    /** Seconds added to the video's clock when looking up lines (a stream that started mid-file). */
    timeOffset: number
    setTrack(content: string): void
    setTrackByUrl(url: string): void
    freeTrack(): void
    setCurrentTime(isPaused?: boolean, currentTime?: number, rate?: number): void
    resize(width?: number, height?: number, top?: number, left?: number, force?: boolean): void
    addFont(font: string | Uint8Array): void
    destroy(): void
  }
}
