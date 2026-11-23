// Plain-text subtitles (SubRip, WebVTT) as the ASS script the renderer reads. The server
// sends a text track byte for byte, and libass reads ASS only: handed an .srt as it is, it
// finds no lines in it and draws nothing, with no error (every SubRip track and every
// .srt beside a video was blank on the web).

/** ASS/SSA as it is; anything else is taken for SubRip or WebVTT. */
export function isAss(text: string): boolean {
  return /^﻿?\s*\[(Script Info|V4\+? Styles|Events)\]/i.test(text) || /^\s*\[Events\]\s*$/im.test(text)
}

/** One line of a plain-text track. */
export interface PlainCue {
  startMs: number
  endMs: number
  /** As written: its lines, with whatever tags the file has. */
  lines: string[]
  /** WebVTT cue settings (what follows the times), '' for SubRip. */
  settings: string
}

// 00:01:02,345 --> 00:01:04,000 (SubRip), 01:02.345 --> 01:04.000 line:10% (WebVTT)
const TIMES = /^\s*((?:\d+:)?\d{1,2}:\d{2}[.,]\d{1,3})\s*-->\s*((?:\d+:)?\d{1,2}:\d{2}[.,]\d{1,3})(.*)$/

function ms(stamp: string): number {
  const [clock, frac = '0'] = stamp.split(/[.,]/)
  const parts = clock.split(':').map(Number)
  const [h, m, s] = parts.length === 3 ? parts : [0, parts[0], parts[1]]
  return ((h * 60 + m) * 60 + s) * 1000 + Math.round(Number('0.' + frac) * 1000)
}

/** The cues of a SubRip or WebVTT file, in the file's order. Headers, notes and styles have no times and are left out. */
export function parsePlain(text: string): PlainCue[] {
  const lines = text.replace(/^﻿/, '').split(/\r\n|\r|\n/)
  const cues: PlainCue[] = []
  for (let i = 0; i < lines.length; i++) {
    const m = TIMES.exec(lines[i])
    if (!m) continue
    const body: string[] = []
    while (i + 1 < lines.length && lines[i + 1].trim() !== '' && !TIMES.test(lines[i + 1])) body.push(lines[++i])
    // SubRip numbers its cues on the line before the times: a number left at the end of
    // this cue's lines, with the next cue's times right after it, is the next one's.
    if (body.length > 1 && /^\d+$/.test(body[body.length - 1].trim()) && i + 1 < lines.length && TIMES.test(lines[i + 1])) body.pop()
    const startMs = ms(m[1])
    const endMs = ms(m[2])
    if (body.length === 0 || endMs <= startMs) continue
    cues.push({ startMs, endMs, lines: body, settings: m[3].trim() })
  }
  return cues
}

const ENTITIES: Record<string, string> = { amp: '&', lt: '<', gt: '>', nbsp: ' ', quot: '"', apos: "'", lrm: '‎', rlm: '‏' }

const SWITCHES: Record<string, string> = { i: 'i', em: 'i', b: 'b', strong: 'b', u: 'u', s: 's', strike: 's' }

/** #RRGGBB or a few names, as ASS writes a colour (blue first). */
function assColour(value: string): string | null {
  const named: Record<string, string> = { white: 'ffffff', black: '000000', red: 'ff0000', green: '008000', lime: '00ff00', blue: '0000ff', yellow: 'ffff00', cyan: '00ffff', magenta: 'ff00ff' }
  const hex = /^#?([0-9a-f]{6})$/i.exec(value.trim())?.[1] ?? named[value.trim().toLowerCase()]
  if (!hex) return null
  return `&H${hex.slice(4, 6)}${hex.slice(2, 4)}${hex.slice(0, 2)}&`.toUpperCase()
}

/**
 * One cue's text as an ASS event's text. Kept: italics, bold, underline, strike-through,
 * a font colour, and the ASS tags some .srt files carry ({\an8} and the like). Every other
 * tag goes, and a brace that is not such a tag is written so that it shows.
 */
export function assText(lines: string[]): string {
  let coloured = 0 // font colours open: a closing </font> ends a colour only if one was set
  const one = (line: string): string => {
    let out = ''
    for (let i = 0; i < line.length; ) {
      const c = line[i]
      if (c === '<') {
        const end = line.indexOf('>', i)
        const tag = end > i ? line.slice(i + 1, end) : ''
        const name = /^\/?\s*([a-z]+)/i.exec(tag)?.[1]?.toLowerCase()
        if (end > i && (name || /^\/?\d/.test(tag) || tag.startsWith('/'))) {
          const closing = tag.startsWith('/')
          if (name && SWITCHES[name]) out += `{\\${SWITCHES[name]}${closing ? 0 : 1}}`
          else if (name === 'font') {
            const colour = closing ? null : assColour(/color\s*=\s*["']?([^"'\s>]+)/i.exec(tag)?.[1] ?? '')
            if (colour) {
              coloured++
              out += `{\\c${colour}}`
            } else if (closing && coloured > 0) {
              coloured--
              out += '{\\c}'
            }
          }
          i = end + 1
          continue
        }
      }
      if (c === '{') {
        const end = line.indexOf('}', i)
        if (end > i && line[i + 1] === '\\') {
          out += line.slice(i, end + 1)
          i = end + 1
          continue
        }
        out += '\\{'
        i++
        continue
      }
      if (c === '}') {
        out += '\\}'
        i++
        continue
      }
      if (c === '&') {
        const m = /^&(#\d+|#x[0-9a-f]+|[a-z]+);/i.exec(line.slice(i, i + 12))
        if (m) {
          const code = m[1]
          const text = code[0] === '#' ? String.fromCodePoint(code[1].toLowerCase() === 'x' ? parseInt(code.slice(2), 16) : Number(code.slice(1))) : ENTITIES[code.toLowerCase()]
          if (text !== undefined) {
            out += text
            i += m[0].length
            continue
          }
        }
      }
      out += c
      i++
    }
    return out.trim()
  }
  const written = lines.map(one).filter((l) => l !== '')
  // Tags and nothing to read: not a line.
  if (!written.some((l) => l.replace(/\{\\[^}]*\}/g, '').trim() !== '')) return ''
  return written.join('\\N')
}

function assTime(msIn: number): string {
  const cs = Math.max(0, Math.round(msIn / 10))
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${Math.floor(cs / 360000)}:${pad(Math.floor(cs / 6000) % 60)}:${pad(Math.floor(cs / 100) % 60)}.${pad(cs % 100)}`
}

/** A WebVTT cue placed in the top part of the picture (line:0, line:10%) is written at the top. */
function atTop(settings: string): boolean {
  const line = /(?:^|\s)line:(-?[\d.]+)(%?)/.exec(settings)
  if (!line) return false
  const n = Number(line[1])
  return line[2] === '%' ? n < 40 : n >= 0 && n < 5
}

/** The script is this wide whatever the picture's shape, so text is the same share of the picture's width on every file. */
const SCRIPT_WIDTH = 1920

/**
 * A SubRip or WebVTT file as an ASS script, in one plain style: white, a thin dark edge,
 * bottom centre, in the renderer's own font. [shape] is the picture's size (any units):
 * the script takes its proportions, so the text is not stretched over a wide film.
 */
export function plainToAss(text: string, shape?: { width: number; height: number } | null): string {
  const tall = shape && shape.width > 0 && shape.height > 0 ? Math.round((SCRIPT_WIDTH * shape.height) / shape.width) : 1080
  const margin = Math.max(30, Math.round(tall * 0.045))
  const events = parsePlain(text)
    .map((c) => ({ c, text: assText(c.lines) }))
    .filter((e) => e.text !== '')
    .map((e) => `Dialogue: 0,${assTime(e.c.startMs)},${assTime(e.c.endMs)},Default,,0,0,0,,${atTop(e.c.settings) && !/\{\\an?\d/.test(e.text) ? '{\\an8}' : ''}${e.text}`)
  return [
    '[Script Info]',
    'ScriptType: v4.00+',
    `PlayResX: ${SCRIPT_WIDTH}`,
    `PlayResY: ${tall}`,
    'WrapStyle: 0',
    'ScaledBorderAndShadow: yes',
    '',
    '[V4+ Styles]',
    'Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding',
    `Style: Default,Liberation Sans,52,&H00FFFFFF,&H000000FF,&H00000000,&H80000000,0,0,0,0,100,100,0,0,1,2.6,0.8,2,100,100,${margin},1`,
    '',
    '[Events]',
    'Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text',
    ...events,
    '',
  ].join('\n')
}
