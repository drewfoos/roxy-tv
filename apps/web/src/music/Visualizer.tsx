// The sound, drawn: what is playing right now, as bars, mirrored bars, a wave or a ring.
// It reads the player's analyser (the real audio on its way out), so it shows nothing
// for a paused song and needs Web Audio.
import { useEffect, useRef, useState } from 'react'
import { IconCheck, IconFullscreen, IconFullscreenExit, IconTune } from '../components/Icons'
import { FloatingMenu, type MenuAt } from './Menus'
import { player } from './player'
import { musicPrefs, VISUALIZERS, type VisualizerStyle } from './prefs'
import { usePlayer } from './usePlayer'
import { usePrefs } from './usePrefs'

/** The bars: how many, and the range of pitch they cover (low notes to the top of hearing that matters). */
const BARS = 56
const MIN_HZ = 32
const MAX_HZ = 16000
/** The part of the height, under the bars, that their reflection takes. */
const REFLECTION = 0.22

/**
 * The stretch of the analyser's bins each bar covers, as places among them (bin 3.4 is
 * between the fourth and the fifth). Bars are spaced by pitch (each a constant ratio
 * above the one before), as the ear hears, not by frequency, which would give the bass
 * one bar and the hiss fifty.
 */
export function barSpans(bars: number, binCount: number, sampleRate: number, minHz = MIN_HZ, maxHz = MAX_HZ): [number, number][] {
  const perBin = sampleRate / 2 / binCount
  const top = Math.min(maxHz, sampleRate / 2)
  const at = (i: number) => (minHz * Math.pow(top / minHz, i / bars)) / perBin
  return Array.from({ length: bars }, (_, i) => [at(i), at(i + 1)] as [number, number])
}

/**
 * The level of each bar (0 to 1). A bar wider than a bin takes the loudest of its bins.
 * A bass bar is narrower than a bin (at 48 kHz a bin is 12 Hz, the lowest bars 4 Hz): it
 * reads the level at its own pitch, between the two bins around it, so neighbouring bars
 * neither move as one block nor show a pitch that is not theirs.
 */
export function barLevels(data: Uint8Array, spans: [number, number][], out: Float32Array) {
  const last = data.length - 1
  for (let i = 0; i < spans.length; i++) {
    const [from, to] = spans[i]
    if (to - from < 1) {
      const at = Math.min(last, (from + to) / 2)
      const k = Math.floor(at)
      const part = at - k
      out[i] = (data[k] * (1 - part) + data[Math.min(last, k + 1)] * part) / 255
      continue
    }
    let peak = 0
    for (let k = Math.floor(from); k < to && k <= last; k++) if (data[k] > peak) peak = data[k]
    out[i] = peak / 255
  }
}

/** A bar with rounded ends where the browser can draw one, a plain bar where it cannot. */
function bar(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, radii: number[]) {
  ctx.beginPath()
  if (typeof ctx.roundRect === 'function') ctx.roundRect(x, y, w, h, radii)
  else ctx.rect(x, y, w, h)
  ctx.fill()
}

function draw(ctx: CanvasRenderingContext2D, w: number, h: number, style: VisualizerStyle, levels: Float32Array, peaks: Float32Array, wave: Uint8Array | null) {
  ctx.clearRect(0, 0, w, h)
  const accent = '#3ea6ff'
  const light = '#8fd0ff'
  if (style === 'wave') {
    if (!wave) return
    ctx.lineWidth = Math.max(2, h / 220)
    ctx.lineJoin = 'round'
    ctx.strokeStyle = light
    ctx.shadowColor = accent
    ctx.shadowBlur = h / 18
    ctx.beginPath()
    const step = Math.max(1, Math.floor(wave.length / w))
    for (let x = 0, i = 0; i < wave.length; i += step, x += (w * step) / wave.length) {
      const y = h / 2 + ((wave[i] - 128) / 128) * (h * 0.42)
      if (i === 0) ctx.moveTo(x, y)
      else ctx.lineTo(x, y)
    }
    ctx.stroke()
    ctx.shadowBlur = 0
    return
  }
  const n = levels.length
  if (style === 'ring') {
    const cx = w / 2
    const cy = h / 2
    const inner = Math.min(w, h) * 0.2
    const reach = Math.min(w, h) * 0.26
    ctx.lineCap = 'round'
    ctx.lineWidth = Math.max(2, (2 * Math.PI * inner) / n / 2.2)
    for (let i = 0; i < n; i++) {
      // Low notes at the top, running down both sides.
      const side = i % 2 === 0 ? 1 : -1
      const angle = -Math.PI / 2 + side * ((Math.floor(i / 2) + 0.5) / (n / 2)) * Math.PI
      const len = 4 + levels[i] * reach
      ctx.strokeStyle = levels[i] > 0.7 ? light : accent
      ctx.globalAlpha = 0.45 + levels[i] * 0.55
      ctx.beginPath()
      ctx.moveTo(cx + Math.cos(angle) * inner, cy + Math.sin(angle) * inner)
      ctx.lineTo(cx + Math.cos(angle) * (inner + len), cy + Math.sin(angle) * (inner + len))
      ctx.stroke()
    }
    ctx.globalAlpha = 1
    return
  }
  const gap = Math.max(2, w / n / 5)
  const bw = (w - gap * (n - 1)) / n
  const mirror = style === 'mirror'
  // Bars stand on a floor a little above the bottom and are reflected in it.
  const base = mirror ? h / 2 : Math.round(h * (1 - REFLECTION))
  const full = mirror ? h / 2 - 2 : base - 4
  const grad = ctx.createLinearGradient(0, base, 0, base - full)
  grad.addColorStop(0, '#1b5fa8')
  grad.addColorStop(0.6, accent)
  grad.addColorStop(1, light)
  const radius = Math.min(bw / 2, 4)
  for (let i = 0; i < n; i++) {
    const x = i * (bw + gap)
    const bh = Math.max(2, levels[i] * full)
    ctx.fillStyle = grad
    bar(ctx, x, base - bh, bw, bh, [radius, radius, 0, 0])
    if (mirror) {
      ctx.globalAlpha = 0.35
      bar(ctx, x, base, bw, bh * 0.85, [0, 0, radius, radius])
      ctx.globalAlpha = 1
    } else {
      // The highest each bar reached lately, coming down slowly.
      ctx.fillStyle = 'rgba(255,255,255,0.85)'
      ctx.fillRect(x, base - Math.max(2, peaks[i] * full) - 3, bw, 2)
      // Its reflection in the floor: shorter, and fading away from it.
      const rh = Math.min(h - base, bh * (REFLECTION / (1 - REFLECTION)))
      const fade = ctx.createLinearGradient(0, base, 0, base + (h - base))
      fade.addColorStop(0, 'rgba(62,166,255,0.34)')
      fade.addColorStop(1, 'rgba(62,166,255,0)')
      ctx.fillStyle = fade
      ctx.fillRect(x, base + 2, bw, Math.max(0, rh - 2))
    }
  }
}

/** The canvas. It draws only while it is on screen, and rests once the sound has died away. */
export function Visualizer({ style }: { style: VisualizerStyle }) {
  const canvas = useRef<HTMLCanvasElement>(null)
  const playing = usePlayer().playing
  useEffect(() => {
    const el = canvas.current
    const ctx = el?.getContext('2d')
    if (!el || !ctx) return
    let raf = 0
    let quiet = 0
    let spans: [number, number][] = []
    let data = new Uint8Array(0)
    let wave = new Uint8Array(0)
    const levels = new Float32Array(BARS)
    const peaks = new Float32Array(BARS)
    const size = () => {
      const ratio = Math.min(window.devicePixelRatio || 1, 2)
      const box = el.getBoundingClientRect()
      el.width = Math.max(1, Math.round(box.width * ratio))
      el.height = Math.max(1, Math.round(box.height * ratio))
    }
    size()
    const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(size) : null
    ro?.observe(el)
    const frame = () => {
      raf = requestAnimationFrame(frame)
      const analyser = player.analyser()
      if (!analyser) return
      if (data.length !== analyser.frequencyBinCount) {
        data = new Uint8Array(analyser.frequencyBinCount)
        wave = new Uint8Array(analyser.fftSize)
        spans = barSpans(BARS, analyser.frequencyBinCount, analyser.context.sampleRate)
      }
      analyser.getByteFrequencyData(data)
      if (style === 'wave') analyser.getByteTimeDomainData(wave)
      barLevels(data, spans, levels)
      let loud = 0
      for (let i = 0; i < BARS; i++) {
        peaks[i] = Math.max(levels[i], peaks[i] - 0.008)
        if (levels[i] > loud) loud = levels[i]
      }
      draw(ctx, el.width, el.height, style, levels, peaks, style === 'wave' ? wave : null)
      // Paused and fallen silent: nothing left to draw until it plays again.
      quiet = loud < 0.01 && !player.get().playing ? quiet + 1 : 0
      if (quiet > 90) {
        cancelAnimationFrame(raf)
        raf = 0
      }
    }
    raf = requestAnimationFrame(frame)
    return () => {
      cancelAnimationFrame(raf)
      ro?.disconnect()
    }
  }, [style, playing])
  return <canvas ref={canvas} className="mviz__canvas" aria-hidden="true" />
}

/**
 * The visualizer for "now playing": it fills its panel, and what there is to set (its
 * style, taking the whole window) is behind two small buttons in its corner, which show
 * when the pointer is over it.
 */
export function VisualizerPanel({ wide = false, onWide, caption }: { wide?: boolean; onWide?: (wide: boolean) => void; caption?: string }) {
  const prefs = usePrefs()
  const state = usePlayer()
  const [menu, setMenu] = useState<MenuAt | null>(null)
  const ready = player.analyser() != null
  return (
    <div className={'mviz' + (menu ? ' mviz--menu' : '')}>
      <Visualizer style={prefs.visualizer} />
      {!ready && <div className="mviz__note">{state.playing ? 'This browser gives no access to the sound, so there is nothing to draw.' : 'Press play to see the sound.'}</div>}
      {caption && <div className="mviz__caption">{caption}</div>}
      <div className="mviz__tools">
        {onWide && (
          <button type="button" className="micon" onClick={() => onWide(!wide)} aria-pressed={wide} aria-label={wide ? 'Back beside the song' : 'Fill the window'} title={wide ? 'Back beside the song' : 'Fill the window'}>
            {wide ? <IconFullscreenExit size={22} /> : <IconFullscreen size={22} />}
          </button>
        )}
        <button
          type="button"
          className={'micon' + (menu ? ' micon--on' : '')}
          onClick={(e) => {
            const box = e.currentTarget.getBoundingClientRect()
            setMenu((m) => (m ? null : { x: box.right, y: box.bottom, box }))
          }}
          aria-label="Visualizer style"
          title="Style"
          aria-haspopup="menu"
          aria-expanded={!!menu}
        >
          <IconTune size={22} />
        </button>
      </div>
      {menu && (
        <FloatingMenu at={menu} onClose={() => setMenu(null)}>
          {VISUALIZERS.map((v) => (
            <button key={v.id} type="button" role="menuitemradio" aria-checked={prefs.visualizer === v.id} className={'mmenu__pick' + (prefs.visualizer === v.id ? ' on' : '')} onClick={() => musicPrefs.set({ visualizer: v.id })}>
              <span className="mmenu__name">{v.label}</span>
              {prefs.visualizer === v.id && <IconCheck size={18} />}
            </button>
          ))}
        </FloatingMenu>
      )}
    </div>
  )
}
