// The Milkdrop visualizer: the pictures Winamp drew to music, each a small program (a
// "preset") of flowing shapes and colours that the sound drives. It is the open-source
// butterchurn (MIT), which draws them with WebGL.
//
// Two things about how it is used here:
//
//  - It is fetched only when this style is picked (the code and its presets are half a
//    megabyte), like every other part of the app that few pages need.
//  - A preset's sums are compiled to WebAssembly (`onlyUseWASM`), never built into
//    JavaScript functions from text: the page's content policy forbids that, and so does
//    this app (see app/noHtmlFromData.test.ts). A preset that has no WebAssembly form is
//    not loaded at all.
import { useEffect, useRef, useState } from 'react'
import type { MilkdropPreset, MilkdropVisualizer } from 'butterchurn'
import { player } from './player'
import { usePlayer } from './usePlayer'

/** How long one preset is shown before the next is blended in, and how long the blend takes (seconds). */
export const PRESET_SECONDS = 30
export const BLEND_SECONDS = 2.7

/** The presets of a pack, whichever way the pack hands them over. */
export function presetsOf(pack: unknown): Record<string, MilkdropPreset> {
  let p = pack as { default?: unknown } | null
  // A pack built for another kind of module arrives wrapped, once or twice.
  for (let i = 0; i < 2 && p && typeof p === 'object' && 'default' in p && p.default && typeof p.default === 'object'; i++) p = p.default as { default?: unknown }
  return (p ?? {}) as Record<string, MilkdropPreset>
}

/** The order the presets are shown in: every one once, in an order of its own each time, before any comes again. */
export function presetOrder(names: string[], random: () => number = Math.random): string[] {
  const order = [...names]
  for (let i = order.length - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1))
    ;[order[i], order[j]] = [order[j], order[i]]
  }
  return order
}

/** A preset's name as it is shown: its file's name, without what the collection put before it. */
export function presetLabel(name: string): string {
  return name.replace(/^[_$\s]+/, '').replace(/\s+/g, ' ').trim()
}

interface Loaded {
  viz: MilkdropVisualizer
  node: AudioNode
}

export function Milkdrop({ onName }: { onName?: (name: string) => void }) {
  const canvas = useRef<HTMLCanvasElement>(null)
  const playing = usePlayer().playing
  const ready = player.analyser() != null
  const [failed, setFailed] = useState(false)
  const loaded = useRef<Loaded | null>(null)
  const draw = useRef(playing)
  draw.current = playing

  // Made once the sound can be read (after the first press of play), kept until the style
  // or the page goes.
  useEffect(() => {
    const el = canvas.current
    const node = player.analyser()
    if (!el || !node) return
    let gone = false
    let raf = 0
    let timer: ReturnType<typeof setInterval> | undefined
    const fit = () => {
      const ratio = Math.min(window.devicePixelRatio || 1, 2)
      const box = el.getBoundingClientRect()
      el.width = Math.max(1, Math.round(box.width * ratio))
      el.height = Math.max(1, Math.round(box.height * ratio))
      loaded.current?.viz.setRendererSize(el.width, el.height)
    }
    fit()
    const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(fit) : null
    ro?.observe(el)
    void (async () => {
      try {
        const [{ default: butterchurn }, pack] = await Promise.all([import('butterchurn'), import('butterchurn-presets/dist/minimal.min.js')])
        if (gone) return
        const presets = presetsOf(pack)
        const names = Object.keys(presets)
        if (!names.length) throw new Error('no presets')
        const viz = butterchurn.createVisualizer(node.context, el, { width: el.width, height: el.height, pixelRatio: 1, onlyUseWASM: true })
        viz.connectAudio(node)
        loaded.current = { viz, node }
        let order = presetOrder(names)
        let at = 0
        const show = async (blend: number) => {
          const name = order[at]
          await viz.loadPreset(presets[name], blend)
          if (!gone) onName?.(presetLabel(name))
        }
        await show(0)
        if (gone) return
        timer = setInterval(() => {
          if (!draw.current) return // paused: the picture waits where it is
          if (++at >= order.length) {
            order = presetOrder(names)
            at = 0
          }
          void show(BLEND_SECONDS).catch(() => undefined)
        }, PRESET_SECONDS * 1000)
        const frame = () => {
          raf = requestAnimationFrame(frame)
          if (draw.current) viz.render()
        }
        raf = requestAnimationFrame(frame)
      } catch {
        if (!gone) setFailed(true)
      }
    })()
    return () => {
      gone = true
      cancelAnimationFrame(raf)
      clearInterval(timer)
      ro?.disconnect()
      const had = loaded.current
      loaded.current = null
      try {
        had?.viz.disconnectAudio(had.node)
      } catch {
        /* the sound output went first: nothing is connected any more */
      }
    }
  }, [ready]) // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <>
      <canvas ref={canvas} className="mviz__canvas" aria-hidden="true" />
      {failed && <div className="mviz__note">This browser cannot draw these (they need WebGL 2 and WebAssembly). The other styles work everywhere.</div>}
    </>
  )
}
