// @vitest-environment jsdom
import { act } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// The Milkdrop visualizer (butterchurn) with the library itself stood in for: what is
// asked of it, and when.
const lib = {
  made: [] as { context: unknown; canvas: HTMLCanvasElement; options: Record<string, unknown> }[],
  connected: [] as unknown[],
  disconnected: [] as unknown[],
  loaded: [] as { preset: unknown; blend: number | undefined }[],
  renders: 0,
  sizes: [] as [number, number][],
  fail: false,
}
vi.mock('butterchurn', () => ({
  default: {
    createVisualizer(context: unknown, canvas: HTMLCanvasElement, options: Record<string, unknown>) {
      if (lib.fail) throw new Error('no WebGL 2 here')
      lib.made.push({ context, canvas, options })
      return {
        connectAudio: (n: unknown) => lib.connected.push(n),
        disconnectAudio: (n: unknown) => lib.disconnected.push(n),
        loadPreset: async (preset: unknown, blend?: number) => void lib.loaded.push({ preset, blend }),
        setRendererSize: (w: number, h: number) => void lib.sizes.push([w, h]),
        render: () => void lib.renders++,
      }
    },
  },
}))
vi.mock('butterchurn-presets/dist/minimal.min.js', () => ({ default: { default: { '_One - first': { n: 1 }, '$$$ Two': { n: 2 }, Three: { n: 3 } } } }))

const sound = { node: { context: { sampleRate: 48000 } } as unknown as AudioNode | null, playing: true }
vi.mock('./player', () => ({ player: { analyser: () => sound.node } }))
vi.mock('./usePlayer', () => ({ usePlayer: () => ({ playing: sound.playing }) }))

import { BLEND_SECONDS, Milkdrop, PRESET_SECONDS, presetLabel, presetOrder, presetsOf } from './Milkdrop'

let host: HTMLDivElement
let root: Root
let frames: FrameRequestCallback[]
const names: string[] = []
const frame = (n = 1) => act(async () => { for (let i = 0; i < n; i++) frames.splice(0).forEach((f) => f(0)) })
const settle = async () => { for (let i = 0; i < 8; i++) await act(async () => { await Promise.resolve() }) }
const show = async () => {
  await act(async () => root.render(<Milkdrop onName={(n) => names.push(n)} />))
  await settle()
}

beforeEach(() => {
  ;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
  Object.assign(lib, { made: [], connected: [], disconnected: [], loaded: [], renders: 0, sizes: [], fail: false })
  Object.assign(sound, { node: { context: { sampleRate: 48000 } }, playing: true })
  names.length = 0
  frames = []
  vi.stubGlobal('requestAnimationFrame', (f: FrameRequestCallback) => frames.push(f))
  vi.stubGlobal('cancelAnimationFrame', () => { frames = [] })
  host = document.createElement('div')
  document.body.appendChild(host)
  root = createRoot(host)
})
afterEach(() => {
  act(() => root.unmount())
  host.remove()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('the Milkdrop visualizer', () => {
  it('draws on the page\'s canvas from the player\'s sound, with a preset\'s sums as WebAssembly only', async () => {
    await show()
    expect(lib.made.length).toBe(1)
    expect(lib.made[0].canvas).toBe(host.querySelector('canvas.mviz__canvas'))
    expect(lib.made[0].context).toBe(sound.node!.context)
    // Never JavaScript built from a preset's text: the page's content policy forbids it.
    expect(lib.made[0].options.onlyUseWASM).toBe(true)
    expect(lib.connected).toEqual([sound.node])
    // A preset is on show from the start, with no blend, and is named.
    expect(lib.loaded.length).toBe(1)
    expect(lib.loaded[0].blend).toBe(0)
    expect(names.length).toBe(1)
    expect(['One - first', 'Two', 'Three']).toContain(names[0])
    await frame(3)
    expect(lib.renders).toBe(3)
  })

  it('rests while the music is paused, and neither draws nor changes its picture', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'setTimeout', 'clearTimeout'] }) // not the frames: the test hands those out
    await show()
    await frame(2)
    expect(lib.renders).toBe(2)
    sound.playing = false
    await act(async () => root.render(<Milkdrop onName={(n) => names.push(n)} />))
    await frame(5)
    expect(lib.renders).toBe(2)
    await act(async () => { vi.advanceTimersByTime(PRESET_SECONDS * 1000 * 3) })
    expect(lib.loaded.length).toBe(1)
    expect(lib.made.length).toBe(1) // not made anew for a pause
    sound.playing = true
    await act(async () => root.render(<Milkdrop onName={(n) => names.push(n)} />))
    await frame(1)
    expect(lib.renders).toBe(3)
  })

  it('blends in another preset every so often, each of them once before any comes again', async () => {
    vi.useFakeTimers({ toFake: ['setInterval', 'clearInterval', 'setTimeout', 'clearTimeout'] }) // not the frames: the test hands those out
    await show()
    for (let i = 0; i < 2; i++) {
      await act(async () => { vi.advanceTimersByTime(PRESET_SECONDS * 1000) })
      await settle()
    }
    expect(lib.loaded.length).toBe(3)
    expect(lib.loaded.slice(1).every((l) => l.blend === BLEND_SECONDS)).toBe(true)
    expect(new Set(lib.loaded.map((l) => (l.preset as { n: number }).n)).size).toBe(3)
    expect(new Set(names).size).toBe(3)
  })

  it('lets go of the sound and stops when it is taken away', async () => {
    await show()
    await act(async () => root.render(<p>another style</p>))
    expect(lib.disconnected).toEqual([sound.node])
    const before = lib.renders
    await frame(3)
    expect(lib.renders).toBe(before)
  })

  it('waits for the sound: nothing is fetched or made before the first play', async () => {
    sound.node = null
    await show()
    expect(lib.made.length).toBe(0)
    expect(host.querySelector('canvas')).not.toBeNull() // its place is held
  })

  it('says so where the browser cannot draw it', async () => {
    lib.fail = true
    await show()
    expect(host.querySelector('.mviz__note')!.textContent).toContain('cannot draw')
    expect(lib.connected).toEqual([])
  })

  it('reads a preset pack however it is wrapped, shows every preset once a round, and names them plainly', () => {
    const map = { a: { x: 1 }, b: { x: 2 } }
    expect(presetsOf(map)).toBe(map)
    expect(presetsOf({ default: map })).toBe(map)
    expect(presetsOf({ default: { default: map } })).toBe(map)
    expect(presetsOf(null)).toEqual({})
    const order = presetOrder(['a', 'b', 'c', 'd'], () => 0)
    expect([...order].sort()).toEqual(['a', 'b', 'c', 'd'])
    expect(order).not.toEqual(['a', 'b', 'c', 'd'])
    expect(presetLabel('_Aderrasi - Wanderer  in Curved Space')).toBe('Aderrasi - Wanderer in Curved Space')
    expect(presetLabel('$$$ Royal - Mashup (197)')).toBe('Royal - Mashup (197)')
  })
})
