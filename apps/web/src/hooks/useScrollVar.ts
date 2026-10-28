import { useEffect } from 'react'

/**
 * Publishes the page's scroll offset as the unitless CSS variable `--sy` on <html>, so
 * the phone layout can do the app's scroll-linked effects in plain CSS: the home
 * header turning solid over 160 px, the title page's art pinned and fading over 320 px,
 * its header fading in between 250 and 320 px. One rAF-throttled listener per app.
 */
export function useScrollVar() {
  useEffect(() => {
    const root = document.documentElement
    let raf = 0
    const write = () => {
      raf = 0
      root.style.setProperty('--sy', String(Math.max(0, Math.round(window.scrollY))))
    }
    const onScroll = () => {
      if (!raf) raf = requestAnimationFrame(write)
    }
    write()
    window.addEventListener('scroll', onScroll, { passive: true })
    return () => {
      window.removeEventListener('scroll', onScroll)
      if (raf) cancelAnimationFrame(raf)
    }
  }, [])
}
