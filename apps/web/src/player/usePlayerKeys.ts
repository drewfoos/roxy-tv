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
      const target = e.target as HTMLElement | null
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT')) return
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
