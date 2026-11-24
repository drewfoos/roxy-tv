/** Whether things may move here: not where less motion was asked for, nor where that cannot be asked. */
export function animates(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function' && !window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/** Whether less motion was asked for (a setting of the system's). Where that cannot be asked, it was not. */
export function lessMotionAsked(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}
