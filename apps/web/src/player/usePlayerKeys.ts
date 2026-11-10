import { useEffect, useRef } from 'react'

export interface PlayerKeyActions {
  togglePlay: () => void
  seekBy: (seconds: number) => void
  volumeBy: (delta: number) => void
  toggleFullscreen: () => void
  toggleMute: () => void
  toggleSubs: () => void
  next: (() => void) | null
  closeMenu: () => void
  /** Any handled key: show the controls again. */
  poke: () => void
}

/** Inputs nobody types in: a key pressed with the focus on one is still the player's. */
const NOT_TEXT = new Set(['range', 'checkbox', 'radio', 'button', 'submit', 'reset', 'color', 'file', 'image'])

/**
 * Whether a key pressed on this element is text being typed (or a choice in a list). Not
 * every <input>: the volume slider is one and keeps the focus after a drag, and ignoring
 * its keys left the player without a keyboard until something else was clicked.
 */
function typesInto(target: HTMLElement | null): boolean {
  if (!target) return false
  if (target.tagName === 'INPUT') return !NOT_TEXT.has((target as HTMLInputElement).type)
  return target.tagName === 'TEXTAREA' || target.tagName === 'SELECT' || target.isContentEditable
}

/**
 * The player's keyboard: space / K play-pause, ← / J and → / L 10 s, ↑ ↓ volume, F
 * fullscreen, M mute, C subtitles, N next episode, Esc closes a menu. Ignored while typing
 * in a field.
 */
export function usePlayerKeys(actions: PlayerKeyActions) {
  // Read through a ref, so the listener is added once and always calls the latest actions.
  const ref = useRef(actions)
  ref.current = actions
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // Ctrl/Cmd/Alt combinations belong to the browser and the OS (Ctrl+L, Cmd+F, Alt+Left
      // for back): they used to seek or go fullscreen as well as doing their own job.
      if (e.ctrlKey || e.metaKey || e.altKey) return
      if (typesInto(e.target as HTMLElement | null)) return
      const a = ref.current
      switch (e.key) {
        case ' ':
        case 'k':
          e.preventDefault()
          a.togglePlay()
          break
        case 'ArrowLeft':
        case 'j':
          e.preventDefault()
          a.seekBy(-10)
          break
        case 'ArrowRight':
        case 'l':
          e.preventDefault()
          a.seekBy(10)
          break
        case 'ArrowUp':
          e.preventDefault()
          a.volumeBy(0.1)
          break
        case 'ArrowDown':
          e.preventDefault()
          a.volumeBy(-0.1)
          break
        case 'f':
          a.toggleFullscreen()
          break
        case 'm':
          a.toggleMute()
          break
        case 'c':
          a.toggleSubs()
          break
        case 'n':
          a.next?.()
          break
        case 'Escape':
          a.closeMenu()
          break
        default:
          return
      }
      a.poke()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
}
