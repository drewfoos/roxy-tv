// For tests only.
// A stand-in for the browser's <audio>: it does what the player tells it and says what a
// test makes it say (time passing, the song ending, the file failing).
export class FakeAudio {
  static made: FakeAudio[] = []
  /** Files that never start sounding (the server is gone, a format with no decoder). */
  static broken = false
  src = ''
  preload = ''
  currentTime = 0
  duration = NaN
  paused = true
  ended = false
  readyState = 4
  volume = 1
  muted = false
  error: unknown = null
  loads = 0
  /** What play() answers with: null resolves, anything else rejects with that name. */
  refuse: string | null = null
  private listeners = new Map<string, Set<() => void>>()
  constructor() {
    FakeAudio.made.push(this)
  }
  addEventListener(name: string, l: () => void) {
    if (!this.listeners.has(name)) this.listeners.set(name, new Set())
    this.listeners.get(name)!.add(l)
  }
  removeEventListener(name: string, l: () => void) {
    this.listeners.get(name)?.delete(l)
  }
  removeAttribute(name: string) {
    if (name === 'src') this.src = ''
  }
  emit(name: string) {
    this.listeners.get(name)?.forEach((l) => l())
  }
  load() {
    this.loads++
    this.ended = false
    this.paused = true
  }
  play() {
    if (this.refuse) return Promise.reject(Object.assign(new Error(this.refuse), { name: this.refuse }))
    this.paused = false
    this.ended = false
    this.emit('play')
    if (!FakeAudio.broken) this.emit('playing')
    return Promise.resolve()
  }
  pause() {
    if (this.paused) return
    this.paused = true
    this.emit('pause')
  }
  // ---- what a test does to it ----
  /** Time passes by playing: one second at a time up to `to`. */
  playTo(to: number, length = this.duration) {
    this.duration = length
    while (this.currentTime < to) {
      this.currentTime = Math.min(to, this.currentTime + 1)
      this.emit('timeupdate')
    }
  }
  finish() {
    this.ended = true
    this.paused = true
    this.emit('ended')
  }
  fail() {
    this.error = { code: 4 }
    this.emit('error')
  }
}
