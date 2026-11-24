// Rows put in another order by dragging a handle (mouse, pen or finger), or with the
// arrow keys on the handle. The list itself is not touched here: `onMove(from, to)` is
// told where a row was dropped, and the owner of the list moves it.
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
  /** Class names for a row: the one lifted, and where the line showing the drop goes. */
  rowClass: (index: number) => string
}

/** Where a row dragged from `from` lands for a pointer at `y`, given the rows' boxes. */
export function dropIndex(boxes: { top: number; bottom: number }[], from: number, y: number): number {
  let to = from
  for (let i = 0; i < boxes.length; i++) {
    const mid = (boxes[i].top + boxes[i].bottom) / 2
    if (i < from && y < mid) return i
    if (i > from && y > mid) to = i
  }
  return to
}

export function useReorder(count: number, onMove: (from: number, to: number) => void, rowSelector: string): Reorder {
  const list = useRef<HTMLElement | null>(null)
  const [drag, setDrag] = useState<{ from: number; over: number } | null>(null)
  const state = useRef<{ from: number; over: number; scroll: number; raf: number } | null>(null)

  const rows = () => Array.from(list.current?.querySelectorAll<HTMLElement>(rowSelector) ?? [])
  const end = useCallback(() => {
    if (state.current) cancelAnimationFrame(state.current.raf)
    state.current = null
    setDrag(null)
  }, [])

  const handle = (index: number) => ({
    onPointerDown: (e: ReactPointerEvent) => {
      if (e.button !== 0 && e.pointerType === 'mouse') return
      e.preventDefault()
      e.stopPropagation()
      ;(e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId)
      state.current = { from: index, over: index, scroll: 0, raf: 0 }
      setDrag({ from: index, over: index })
    },
    onPointerMove: (e: ReactPointerEvent) => {
      const s = state.current
      if (!s) return
      const boxes = rows().map((r) => r.getBoundingClientRect())
      const over = dropIndex(boxes, s.from, e.clientY)
      if (over !== s.over) {
        s.over = over
        setDrag({ from: s.from, over })
      }
      // Near the top or bottom of the list it scrolls, faster the nearer the edge.
      const el = list.current
      if (!el) return
      const box = el.getBoundingClientRect()
      const edge = 48
      s.scroll = e.clientY < box.top + edge ? -(box.top + edge - e.clientY) / 3 : e.clientY > box.bottom - edge ? (e.clientY - (box.bottom - edge)) / 3 : 0
      if (s.scroll && !s.raf) {
        const step = () => {
          const now = state.current
          if (!now || !now.scroll || !list.current) {
            if (now) now.raf = 0
            return
          }
          list.current.scrollTop += now.scroll
          now.raf = requestAnimationFrame(step)
        }
        s.raf = requestAnimationFrame(step)
      }
    },
    onPointerUp: (e: ReactPointerEvent) => {
      const s = state.current
      ;(e.currentTarget as HTMLElement).releasePointerCapture?.(e.pointerId)
      end()
      if (s && s.over !== s.from) onMove(s.from, s.over)
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
    if (index === drag.over) return drag.over < drag.from ? ' drop-before' : ' drop-after'
    return ''
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
