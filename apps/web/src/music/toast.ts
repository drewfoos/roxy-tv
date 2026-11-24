// A line that says what a press just did ("Added to Morning"), for a moment. Drawn
// straight into the page, so that it shows whatever part of the music interface made it.
let el: HTMLDivElement | null = null
let timer: ReturnType<typeof setTimeout> | undefined

export const toast = {
  show(message: string, ms = 2600) {
    if (typeof document === 'undefined') return
    if (!el) {
      el = document.createElement('div')
      el.className = 'mtoast'
      el.setAttribute('role', 'status')
    }
    el.textContent = message
    if (!el.isConnected) document.body.appendChild(el)
    // Started again for a second message while the first still shows.
    el.classList.remove('mtoast--in')
    void el.offsetWidth
    el.classList.add('mtoast--in')
    clearTimeout(timer)
    timer = setTimeout(() => el?.remove(), ms)
  },
  /** What is showing now (for tests). */
  text: () => (el?.isConnected ? el.textContent : null),
}
