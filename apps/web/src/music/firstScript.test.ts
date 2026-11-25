import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

// The Milkdrop pictures (butterchurn and its presets) are close to half a megabyte of
// script. They are fetched when that visualizer style is picked and at no other time:
// nobody who does not ask for them pays for them. That holds only while nothing brings
// them in by an ordinary import, which would put them in the script every page loads.
function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) return sources(path)
    return /\.(ts|tsx)$/.test(name) && !/\.test\.(ts|tsx)$/.test(name) && !/\.d\.ts$/.test(name) ? [path] : []
  })
}

/**
 * The modules a file brings in by an ordinary import, by their names: not by `import()`,
 * and not for their types alone (`import type` leaves nothing in the script).
 */
export function ordinaryImports(text: string): string[] {
  const found: string[] = []
  for (const m of text.matchAll(/^\s*(?:import|export)\s+(?!type\b)[^'"]*?from\s*['"]([^'"]+)['"]/gm)) found.push(m[1])
  for (const m of text.matchAll(/^\s*import\s*['"]([^'"]+)['"]/gm)) found.push(m[1])
  return found
}

describe('the script every page loads', () => {
  const src = join(__dirname, '..')
  const files = sources(src)

  it('tells an ordinary import from one made when asked', () => {
    expect(ordinaryImports("import { a } from './Milkdrop'\nimport x from \"butterchurn\"\nimport './side'\nexport { b } from './other'\n")).toEqual(['./Milkdrop', 'butterchurn', './other', './side'])
    expect(ordinaryImports("const M = lazy(() => import('./Milkdrop'))\nconst lib = await import('butterchurn')\nimport type { T } from 'butterchurn'\n")).toEqual([])
  })

  it('has no Milkdrop in it: only the visualizer asks for that part, and only when it is picked', () => {
    expect(files.length).toBeGreaterThan(50)
    const brought = files.flatMap((f) => {
      const name = f.slice(src.length + 1)
      return ordinaryImports(readFileSync(f, 'utf8'))
        .filter((m) => /(^|\/)Milkdrop$/.test(m) || /^butterchurn/.test(m))
        .map((m) => `${name} imports ${m}`)
    })
    expect(brought).toEqual([])
    // And it is asked for, where the style is shown.
    expect(readFileSync(join(src, 'music', 'Visualizer.tsx'), 'utf8')).toMatch(/lazy\(\(\) => import\('\.\/Milkdrop'\)/)
    // The library itself is named by the Milkdrop part alone.
    const users = files.filter((f) => /['"]butterchurn/.test(readFileSync(f, 'utf8'))).map((f) => f.slice(src.length + 1))
    expect(users).toEqual([join('music', 'Milkdrop.tsx')])
  })
})
