/**
 * The picture's shape, as a CSS aspect-ratio value. The player's box takes this shape so
 * the picture fills it: a box that is always 16:9 leaves black bands above and below a
 * wider film, which shows most on a tall (portrait) window, where the box is as wide as
 * the page. Null when the size is not known; the box is then 16:9.
 */
export function pictureShape(width?: number, height?: number): string | null {
  if (!width || !height || !Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) return null
  return `${width} / ${height}`
}
