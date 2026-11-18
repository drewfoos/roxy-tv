// Pages loaded when first opened, so the first script is the pages people land on and
// not the player, the account pages and Discover as well.
import { createElement, lazy, type ComponentType, type LazyExoticComponent } from 'react'

/**
 * A page's script could not be loaded. The usual cause is a server updated while this tab
 * was open: the script names of the old build are gone (the server answers 404 for
 * them), so the page is loaded again once, which brings the new build. `RELOADED` keeps
 * that to one go: when the script still does not load after it (the server is away), the
 * page shows its error with a Reload button instead of reloading for ever. A page that
 * loads clears it, so the next update is handled the same way.
 *
 * Only with the server there: reloading while it is away would trade the app (which says
 * it is waiting for the server, and comes back by itself) for the browser's error page.
 */
const RELOADED = 'roxy:page-script-reload'

const flag = {
  get: () => {
    try {
      return sessionStorage.getItem(RELOADED) != null
    } catch {
      return true // nowhere to remember the reload: never reload by itself
    }
  },
  set: () => sessionStorage.setItem(RELOADED, '1'),
  clear: () => {
    try {
      sessionStorage.removeItem(RELOADED)
    } catch {
      /* nothing kept */
    }
  },
}

const serverThere = () =>
  fetch('/healthz', { cache: 'no-store', signal: AbortSignal.timeout(5000) }).then(
    (r) => r.ok,
    () => false,
  )

const COULD_NOT = 'This page could not be loaded. Check the connection to the server and reload.'

export async function loadPage<T>(
  load: () => Promise<T>,
  reload: () => void = () => window.location.reload(),
  there: () => Promise<boolean> = serverThere,
): Promise<T> {
  let page: T
  try {
    page = await load()
  } catch (e) {
    if (flag.get() || !(await there())) throw new Error(COULD_NOT, { cause: e })
    try {
      flag.set()
    } catch {
      throw e
    }
    reload()
    // The page is on its way out: nothing more is drawn (an error would flash first).
    return new Promise<T>(() => undefined)
  }
  flag.clear()
  return page
}

type Page = ComponentType<object>
const loaders: (() => Promise<unknown>)[] = []
/** Pages whose script did not load, each with the way to have it asked for again. */
const failed = new Set<() => void>()

/**
 * The page's error has been shown: the pages that failed are asked for again the next
 * time one is opened. React keeps a lazy component's failure for good, so without this a
 * page that failed once (the server was away for a moment) showed its error at every
 * later visit until the tab was reloaded. Called by the error boundary once the error is
 * on screen, never while React is still drawing: a new try during the failed draw would
 * be drawn at once, fail again and go round.
 */
export function retryFailedPages() {
  failed.forEach((again) => again())
  failed.clear()
}

/** `pick` names the page in its module (the pages are named exports). */
export function lazyPage<M>(load: () => Promise<M>, pick: (m: M) => Page): Page {
  // One import per page however often it is asked for: a failed one is asked again.
  let pending: Promise<{ default: Page }> | null = null
  let current: LazyExoticComponent<Page>
  const once = () =>
    (pending ??= loadPage(load)
      .then((m) => ({ default: pick(m) }))
      .catch((e) => {
        pending = null
        failed.add(renew)
        throw e
      }))
  const renew = () => {
    current = lazy(once)
  }
  renew()
  // Ahead of time, the script alone: a failure there must not reload a page in use. One
  // that loads shows this build's scripts are there, so the next update's missing script
  // gets its one reload again (the mark otherwise stayed for as long as the tab did).
  loaders.push(() => load().then(flag.clear, () => undefined))
  return (props: object) => createElement(current, props)
}

/**
 * Fetch the lazy pages' scripts once the first page is up and the browser is idle, so
 * opening one is as immediate as before. Not on a connection that asked to save data.
 * A failure here is left alone: the page's own load handles it when it is opened.
 */
export function preloadPages() {
  const nav = navigator as Navigator & { connection?: { saveData?: boolean } }
  if (nav.connection?.saveData) return
  const run = () => loaders.forEach((l) => void l())
  if (typeof requestIdleCallback === 'function') requestIdleCallback(run, { timeout: 5000 })
  else setTimeout(run, 2500)
}
