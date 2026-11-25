// The Milkdrop visualizer (butterchurn) and its presets ship without types. This is the
// part of them the music player uses (src/music/Milkdrop.tsx).
declare module 'butterchurn' {
  export interface MilkdropPreset {
    [key: string]: unknown
  }
  export interface MilkdropVisualizer {
    connectAudio(node: AudioNode): void
    disconnectAudio(node: AudioNode): void
    loadPreset(preset: MilkdropPreset, blendSeconds?: number): Promise<void>
    setRendererSize(width: number, height: number): void
    render(): void
  }
  const butterchurn: {
    createVisualizer(
      context: BaseAudioContext,
      canvas: HTMLCanvasElement,
      options: { width: number; height: number; pixelRatio?: number; textureRatio?: number; onlyUseWASM?: boolean },
    ): MilkdropVisualizer
  }
  export default butterchurn
}

declare module 'butterchurn-presets/dist/minimal.min.js' {
  import type { MilkdropPreset } from 'butterchurn'
  const presets: Record<string, MilkdropPreset> | { default: Record<string, MilkdropPreset> }
  export default presets
}
