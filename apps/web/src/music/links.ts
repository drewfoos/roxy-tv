// A song's own address: what is in the address bar while "now playing" is open, and what
// "Copy link" gives. Opened, it shows that song (pages/Music.tsx, MusicSongPage).

/** The path of a song's own page. */
export function songPath(id: string): string {
  return '/music/song/' + encodeURIComponent(id)
}

/** Whether a path is a song's own. */
export function isSongPath(path: string): boolean {
  return path.startsWith('/music/song/')
}

/** The whole address of a song, to hand to somebody. */
export function songLink(id: string, origin: string = window.location.origin): string {
  return origin + songPath(id)
}

// "Now playing" is the bar's to open; a page that wants it open (a song arrived at by its
// link) asks here. Asked before the bar is there, the wish is kept until it is.
const openers = new Set<() => void>()
let wished = false

/** Opens "now playing". */
export function openNowPlaying(): void {
  if (openers.size === 0) wished = true
  else openers.forEach((open) => open())
}

/** The bar says how "now playing" is opened; it is opened at once when that was wished for already. */
export function onOpenNowPlaying(open: () => void): () => void {
  openers.add(open)
  if (wished) {
    wished = false
    open()
  }
  return () => {
    openers.delete(open)
  }
}
