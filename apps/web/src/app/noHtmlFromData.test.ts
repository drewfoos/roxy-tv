import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

// The web app keeps its sign-in token where a script in the page could read it, so no
// script but Roxy's own may ever run there. The Content-Security-Policy sees to foreign
// and inline script; this sees to the other way in: the app never builds HTML, or code,
// out of text. A title, a synopsis, a file's name, a lyric are always text nodes.
const SINKS = [/dangerouslySetInnerHTML/, /\.innerHTML\s*=/, /\.outerHTML\s*=/, /insertAdjacentHTML/, /document\.write/, /\beval\s*\(/, /new Function\s*\(/, /srcdoc\s*=/, /javascript:/i]

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) return sources(path)
    return /\.(ts|tsx)$/.test(name) && !/\.test\.(ts|tsx)$/.test(name) ? [path] : []
  })
}

describe('the web app', () => {
  it('never builds markup or code from text', () => {
    const src = join(__dirname, '..')
    const files = sources(src)
    expect(files.length).toBeGreaterThan(50)
    const found = files.flatMap((f) => {
      const text = readFileSync(f, 'utf8')
      return SINKS.filter((s) => s.test(text)).map((s) => `${f.slice(src.length + 1)}: ${s}`)
    })
    expect(found).toEqual([])
  })
})
