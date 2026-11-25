// Rows put in another order by dragging a handle (mouse, pen or finger), or with the
// arrow keys on the handle. The row goes with the pointer and the others make room as it
// passes. The list itself is not touched here: `onMove(from, to)` is told where a row was
// dropped, and the owner of the list moves it.
import { useCallback, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent } from 'react'

export interface Reorder {
  /** The row being dragged, and the place it would land (null when nothing is dragged). */
  dragging: number | null
  over: number | null
  /** Put on the element that holds the rows (it is what scrolls while dragging near an edge). */
  listRef: (el: HTMLElement | null) => void
  /** Spread on a row's handle. */
  handle: (index: number) => {
    onPointerDown: (e: ReactPointerEvent) => void
    onPointerMove: (e: ReactPointerEvent) => void
    onPointerUp: (e: ReactPointerEvent) => void
    onPointerCancel: () => void
    onKeyDown: (e: ReactKeyboardEvent) => void
    onClick: (e: { stopPropagation: () => void }) => void
  }
  /** Class names for a row while one is carried: the one lifted, and the others (which step aside). */
  rowClass: (index: number) => string
}

/**
 * Where a row carried `offset` from its place at `from` would land, given the rows'
 * boxes as they lay when it was taken up: it takes a neighbour's place once its leading
 * edge has passed that neighbour's middle (it covers more than half of it).
 */
export function dropIndex(boxes: { top: number; bottom: number }[], from: number, offset: number): number {
  const top = boxes[from].top + offset
  const bottom = boxes[from].bottom + offset
  let to = from
  for (let i = 0; i < boxes.length; i++) {
    const mid = (boxes[i].top + boxes[i].bottom) / 2
    if (i < from && top < mid) return i
    if (i > from && bottom > mid) to = i
  }
  return to
}

/** How long rows take to slide out of a dragged row's way, and the dragged row to settle where it is let go. */
export const SLIDE_MS = 160

/**
 * How far the dragged row has gone from where it lay, for a pointer that has moved `dy`
 * (the list's own scrolling counted in): no further than the first row's place or the
 * last one's, so it cannot be carried out of the list.
 */
export function dragOffset(boxes: { top: number; bottom: number }[], from: number, dy: number): number {
  const min = boxes[0].top - boxes[from].top
  const max = boxes[boxes.length - 1].bottom - boxes[from].bottom
  return Math.max(min, Math.min(max, dy))
}

/**
 * Where each row is drawn while the row at `from` is carried `offset` from its place and
 * would land at `over`: the carried row goes with the pointer, and the rows between its
 * old place and the new one step aside by its height (`slot`), into the room it left.
 */
export function rowShifts(count: number, from: number, over: number, offset: number, slot: number): number[] {
  return Array.from({ length: count }, (_, i) => {
    if (i === from) return offset
    if (i > from && i <= over) return -slot
    if (i < from && i >= over) return slot
    return 0
  })
}

/** How far the row at `from` is from the place it lands in at `over`: where it is let go to. */
export function settleOffset(boxes: { top: number; bottom: number }[], from: number, over: number): number {
  if (over > from) return boxes[over].bottom - boxes[from].bottom
  if (over < from) return boxes[over].top - boxes[from].top
  return 0
}

interface Live {
  from: number
  over: number
  /** Where the pointer went down, and how far the list was scrolled then. */
  startY: number
  startScroll: number
  pointerY: number
  /** The rows as they lay when the drag began (they do not lie there while it lasts). */
  els: HTMLElement[]
  boxes: { top: number; bottom: number }[]
  /** The carried row's height with the gap to its neighbour: what the others step aside by. */
  slot: number
  scroll: number
  raf: number
  settling: ReturnType<typeof setTimeout> | undefined
}

/** Whether things may glide here (not where less motion was asked for, nor where that cannot be asked). */
function glides(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function' && !window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/**
 * A row is carried by its handle and goes with the pointer as it moves; the rows it
 * passes slide out of its way at once, so the list looks, all the while, the way it will
 * be when the row is let go. (It used to stay where it was, greyed, with a line drawn
 * where it would land.) The rows are moved by their own `transform`, set straight on
 * them: the list is put in its new order once, when the row is let go and has settled.
 */
export function useReorder(count: number, onMove: (from: number, to: number) => void, rowSelector: string): Reorder {
  const list = useRef<HTMLElement | null>(null)
  const [drag, setDrag] = useState<{ from: number; over: number } | null>(null)
  const state = useRef<Live | null>(null)

  const rows = () => Array.from(list.current?.querySelectorAll<HTMLElement>(rowSelector) ?? [])

  /** Puts every row back where the list has it, with nothing left on it of the drag. */
  const release = (s: Live) => {
    cancelAnimationFrame(s.raf)
    clearTimeout(s.settling)
    for (const r of s.els) {
      r.style.transform = ''
      r.style.transition = ''
    }
  }
  const end = useCallback(() => {
    if (state.current) release(state.current)
    state.current = null
    setDrag(null)
  }, [])

  /** Draws the rows for where the pointer is now. */
  const place = (s: Live) => {
    const scrolled = (list.current?.scrollTop ?? 0) - s.startScroll
    const offset = dragOffset(s.boxes, s.from, s.pointerY - s.startY + scrolled)
    const over = dropIndex(s.boxes, s.from, offset)
    rowShifts(s.els.length, s.from, over, offset, s.slot).forEach((by, i) => {
      s.els[i].style.transform = by ? `translateY(${by}px)` : ''
    })
    if (over !== s.over) {
      s.over = over
      setDrag({ from: s.from, over })
    }
  }

  const handle = (index: number) => ({
    onPointerDown: (e: ReactPointerEvent) => {
      if (e.button !== 0 && e.pointerType === 'mouse') return
      if (state.current) end() // a drag still settling: finished here
      e.preventDefault()
      e.stopPropagation()
      ;(e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId)
      const els = rows()
      if (!els[index]) return
      const boxes = els.map((r) => {
        const b = r.getBoundingClientRect()
        return { top: b.top, bottom: b.bottom }
      })
      const near = boxes[index + 1] ? boxes[index + 1].top - boxes[index].bottom : boxes[index - 1] ? boxes[index].top - boxes[index - 1].bottom : 0
      const smooth = glides()
      els.forEach((r, i) => {
        // The carried row is where the pointer is, at once; the others slide.
        r.style.transition = i === index || !smooth ? 'none' : `transform ${SLIDE_MS}ms cubic-bezier(0.2, 0.8, 0.2, 1)`
      })
      state.current = {
        from: index, over: index, startY: e.clientY, pointerY: e.clientY, startScroll: list.current?.scrollTop ?? 0,
        els, boxes, slot: boxes[index].bottom - boxes[index].top + Math.max(0, near), scroll: 0, raf: 0, settling: undefined,
      }
      setDrag({ from: index, over: index })
    },
    onPointerMove: (e: ReactPointerEvent) => {
      const s = state.current
      if (!s || s.settling !== undefined) return
      s.pointerY = e.clientY
      place(s)
      // Near the top or bottom of the list it scrolls, faster the nearer the edge, and
      // the row is carried along with the pointer over what scrolls by.
      const el = list.current
      if (!el) return
      const box = el.getBoundingClientRect()
      const edge = 48
      s.scroll = e.clientY < box.top + edge ? -(box.top + edge - e.clientY) / 3 : e.clientY > box.bottom - edge ? (e.clientY - (box.bottom - edge)) / 3 : 0
      if (s.scroll && !s.raf) {
        const step = () => {
          const now = state.current
          if (!now || !now.scroll || !list.current || now.settling !== undefined) {
            if (now) now.raf = 0
            return
          }
          list.current.scrollTop += now.scroll
          place(now)
          now.raf = requestAnimationFrame(step)
        }
        s.raf = requestAnimationFrame(step)
      }
    },
    onPointerUp: (e: ReactPointerEvent) => {
      const s = state.current
      ;(e.currentTarget as HTMLElement).releasePointerCapture?.(e.pointerId)
      if (!s || s.settling !== undefined) return
      cancelAnimationFrame(s.raf)
      s.raf = 0
      s.scroll = 0
      const finish = () => {
        if (state.current !== s) return
        // The rows' own places and the list's new order arrive together: nothing jumps.
        end()
        if (s.over !== s.from) onMove(s.from, s.over)
      }
      if (!glides()) return finish()
      // Let go: the row glides the rest of the way into the room made for it.
      const row = s.els[s.from]
      row.style.transition = `transform ${SLIDE_MS}ms cubic-bezier(0.2, 0.8, 0.2, 1)`
      row.style.transform = `translateY(${settleOffset(s.boxes, s.from, s.over)}px)`
      s.settling = setTimeout(finish, SLIDE_MS)
    },
    onPointerCancel: end,
    onKeyDown: (e: ReactKeyboardEvent) => {
      const by = e.key === 'ArrowUp' ? -1 : e.key === 'ArrowDown' ? 1 : 0
      if (!by) return
      e.preventDefault()
      e.stopPropagation()
      const to = index + by
      if (to < 0 || to >= count) return
      onMove(index, to)
      // The handle goes with its row: keep the keyboard on it.
      requestAnimationFrame(() => rows()[to]?.querySelector<HTMLElement>('[data-reorder-handle]')?.focus())
    },
    onClick: (e: { stopPropagation: () => void }) => e.stopPropagation(),
  })

  const rowClass = (index: number) => {
    if (!drag) return ''
    if (index === drag.from) return ' is-dragged'
    return ' is-aside'
  }

  return {
    dragging: drag?.from ?? null,
    over: drag?.over ?? null,
    listRef: (el) => {
      list.current = el
    },
    handle,
    rowClass,
  }
}
