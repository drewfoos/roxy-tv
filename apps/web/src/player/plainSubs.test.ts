import { describe, expect, it } from 'vitest'
import { assText, isAss, parsePlain, plainToAss } from './plainSubs'

const SRT = `1
00:26:15,115 --> 00:26:18,243
Typhoon Devil.

2
00:26:21,538 --> 00:26:26,084
The contract is that once
I get my hands on Chainsaw's heart,

3
01:02:03,004 --> 01:02:04,500
<i>[door creaks]</i>
`

const events = (script: string) => script.split('\n').filter((l) => l.startsWith('Dialogue:'))

describe('plain-text subtitles as ASS', () => {
  it('writes each SubRip cue as an ASS line, at its times', () => {
    expect(events(plainToAss(SRT))).toEqual([
      'Dialogue: 0,0:26:15.12,0:26:18.24,Default,,0,0,0,,Typhoon Devil.',
      "Dialogue: 0,0:26:21.54,0:26:26.08,Default,,0,0,0,,The contract is that once\\NI get my hands on Chainsaw's heart,",
      'Dialogue: 0,1:02:03.00,1:02:04.50,Default,,0,0,0,,{\\i1}[door creaks]{\\i0}',
    ])
  })

  it('is a script libass reads: the three sections, one style', () => {
    const script = plainToAss(SRT)
    expect(isAss(script)).toBe(true)
    expect(script).toMatch(/^\[Script Info\]\n/)
    expect(script).toContain('\n[V4+ Styles]\n')
    expect(script).toContain('\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n')
    expect(script.split('\n').filter((l) => l.startsWith('Style:'))).toHaveLength(1)
  })

  it('takes the proportions of the picture, so text is not stretched over a wide film', () => {
    expect(plainToAss(SRT, { width: 3840, height: 1604 })).toContain('PlayResX: 1920\nPlayResY: 802\n')
    expect(plainToAss(SRT, { width: 1440, height: 1080 })).toContain('PlayResX: 1920\nPlayResY: 1440\n')
    expect(plainToAss(SRT)).toContain('PlayResX: 1920\nPlayResY: 1080\n')
    expect(plainToAss(SRT, { width: 0, height: 0 })).toContain('PlayResY: 1080\n')
  })

  it('reads Windows line ends and a byte-order mark', () => {
    const windows = '﻿' + SRT.replace(/\n/g, '\r\n')
    expect(events(plainToAss(windows))).toEqual(events(plainToAss(SRT)))
  })

  it('reads WebVTT: no header, note or style as a line; hours may be left out', () => {
    const vtt = `WEBVTT - made by hand

NOTE this is a comment
over two lines

STYLE
::cue { color: lime }

intro
00:05.000 --> 00:07.500 line:10% align:center
<v Makima>Hello &amp; <c.loud>welcome</c>

01:00:00.000 --> 01:00:01.000
Bottom line
`
    expect(events(plainToAss(vtt))).toEqual(['Dialogue: 0,0:00:05.00,0:00:07.50,Default,,0,0,0,,{\\an8}Hello & welcome', 'Dialogue: 0,1:00:00.00,1:00:01.00,Default,,0,0,0,,Bottom line'])
  })

  it('keeps what a cue is written with, and nothing that would hide text', () => {
    expect(assText(['<b>Bold</b> and <u>under</u> and <s>struck</s>'])).toBe('{\\b1}Bold{\\b0} and {\\u1}under{\\u0} and {\\s1}struck{\\s0}')
    expect(assText(['<font color="#FF8000">orange</font> plain'])).toBe('{\\c&H0080FF&}orange{\\c} plain')
    expect(assText(['<font face="Arial" size="20">sized</font>'])).toBe('sized')
    expect(assText(['<i></i>', '{\\an8}'])).toBe('')
    // ASS tags some .srt files carry are tags; any other brace is text and must show.
    expect(assText(['{\\an8}Sign at the top'])).toBe('{\\an8}Sign at the top')
    expect(assText(['{laughs} really'])).toBe('\\{laughs\\} really')
    expect(assText(['1 < 2 and 3 > 2'])).toBe('1 < 2 and 3 > 2')
    expect(assText(['&lt;not a tag&gt; &#9834; &nbsp;x'])).toBe('<not a tag> ♪  x')
    expect(assText(['  padded  ', '', 'second'])).toBe('padded\\Nsecond')
  })

  it('leaves out cues with no text or no length, and a cue number is not text', () => {
    const odd = `1
00:00:01,000 --> 00:00:02,000

2
00:00:03,000 --> 00:00:03,000
never shown

3
00:00:04,000 --> 00:00:05,000
kept
4
00:00:06,000 --> 00:00:07,000
<font></font>
`
    expect(parsePlain(odd).map((c) => c.lines)).toEqual([['kept'], ['<font></font>']])
    expect(events(plainToAss(odd))).toEqual(['Dialogue: 0,0:00:04.00,0:00:05.00,Default,,0,0,0,,kept'])
  })

  it('knows ASS when it sees it', () => {
    expect(isAss('[Script Info]\nTitle: x\n')).toBe(true)
    expect(isAss('﻿[Script Info]\r\n')).toBe(true)
    expect(isAss('; comment\n[V4 Styles]\n\n[Events]\nDialogue: ...')).toBe(true)
    expect(isAss(SRT)).toBe(false)
    expect(isAss('WEBVTT\n\n00:01.000 --> 00:02.000\n[Events] were held')).toBe(false)
    expect(isAss('')).toBe(false)
  })
})
