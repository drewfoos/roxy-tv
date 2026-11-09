// Web player, in real Google Chrome against a real server: a remux stream cut part-way
// (its ffmpeg killed, as the server's stall limit, a restart or a crash would) must be
// reopened where it got to and play on, not count as the end of the film (which marked
// it watched and stopped). Playwright's own Chromium has no H.264/AAC: Chrome is needed.
//
// Env: BASE (the server), ROXY_USER / ROXY_PASS (an admin), CUT_CMD (a shell command that
// kills the running remux's ffmpeg), CHROME (optional: Chrome's path). The server must
// serve one film with audio the browser cannot play (AC3). Chrome's own network
// emulation slows the page to a home network's pace (8 Mbit/s): over loopback a remux
// arrives whole in seconds, and cutting it then would prove nothing.
import { execSync } from 'node:child_process'
import { chromium } from 'playwright-core'

const base = process.env.BASE ?? 'http://127.0.0.1:18097'
const fail = (msg) => {
  console.error('FAIL:', msg)
  process.exit(1)
}
const api = async (path, opts = {}, tok) => {
  const r = await fetch(base + path, { ...opts, headers: { 'Content-Type': 'application/json', ...(tok ? { Authorization: 'Bearer ' + tok } : {}) } })
  const t = await r.text()
  try {
    return JSON.parse(t)
  } catch {
    return t
  }
}

const login = await api('/v1/auth/login', { method: 'POST', body: JSON.stringify({ username: process.env.ROXY_USER, password: process.env.ROXY_PASS, client: 'roxy-web', deviceId: 'e2e-browser', device: 'e2e' }) })
const tok = login.token ?? fail('login: ' + JSON.stringify(login))
const legal = await api('/v1/legal/status', {}, tok)
await api('/v1/legal/accept', { method: 'POST', body: JSON.stringify({ version: legal.version }) }, tok)
const prof = (await api('/v1/me/profiles', {}, tok)).profiles[0]
let item
for (let i = 0; i < 120 && !item; i++) {
  item = (await api('/v1/items?type=movie', {}, tok)).items?.[0]
  if (!item) await new Promise((r) => setTimeout(r, 1000))
}
if (!item) fail('no film in the library')

const browser = await chromium.launch({ channel: process.env.CHROME ? undefined : 'chrome', executablePath: process.env.CHROME, args: ['--autoplay-policy=no-user-gesture-required'] })
try {
  const page = await browser.newPage()
  const cdp = await page.context().newCDPSession(page)
  await cdp.send('Network.enable')
  await cdp.send('Network.emulateNetworkConditions', { offline: false, latency: 10, downloadThroughput: 1_000_000, uploadThroughput: 1_000_000 })
  const remux = []
  page.on('request', (r) => {
    // One entry per stream: Chrome sometimes asks for the same stream twice, which counted
    // as a reopening, and the next cut found no new stream running.
    if (!r.url().includes('/remux')) return
    const start = Number(new URL(r.url()).searchParams.get('start'))
    if (remux.length === 0 || remux[remux.length - 1] !== start) remux.push(start)
  })
  await page.goto(base + '/')
  await page.evaluate((s) => localStorage.setItem('roxy.session', JSON.stringify(s)), {
    token: tok, userId: login.user.id, userName: login.user.name, isAdmin: true, profileId: prof.id, profileName: prof.name, profileHasAvatar: false, profileAvatarKey: '',
  })
  await page.goto(base + '/watch/' + item.id)
  const state = () => page.evaluate(() => {
    const v = document.querySelector('video')
    return v ? { t: v.currentTime, ended: v.ended, paused: v.paused, buffered: v.buffered.length ? v.buffered.end(v.buffered.length - 1) : 0 } : null
  })
  for (let i = 0; i < 120 && !remux.length; i++) await page.waitForTimeout(250)
  if (!remux.length) fail('the player never asked for a remux (is the film\'s audio AC3?)')
  await page.waitForTimeout(3000)
  execSync(process.env.CUT_CMD ?? fail('CUT_CMD is not set'), { stdio: 'inherit' })
  const cut = await state()
  console.log(`cut: at ${cut.t.toFixed(1)} s, ${cut.buffered.toFixed(1)} s buffered of ${item.runtimeMs / 1000 || '?'} s`)
  if (item.runtimeMs && cut.buffered > item.runtimeMs / 1000 - 30) fail('the whole film arrived before the cut (the network was not slowed)')
  // Play out what was buffered quickly; a reopened stream plays at the player's rate.
  await page.evaluate(() => { document.querySelector('video').playbackRate = 4 })
  let reopened = false
  for (let i = 0; i < 240 && !reopened; i++) {
    await page.waitForTimeout(500)
    const s = await state()
    if (s.ended) fail(`the cut stream counted as the end (at ${s.t.toFixed(1)} s)`)
    reopened = remux.length >= 2
  }
  if (!reopened) fail('the stream was never reopened')
  await page.waitForTimeout(4000)
  const after = await state()
  const played = (await api('/v1/items/' + item.id, {}, tok)).userData?.played
  console.log(`reopened at ${remux[1].toFixed(1)} s; now ${after.t.toFixed(1)} s into the new stream, paused=${after.paused}, marked watched=${played}`)
  if (remux[1] < 2) fail('reopened from the start, not where the stream got to')
  if (after.paused || after.ended) fail('not playing after reopening')
  if (played) fail('the film was marked watched')
  console.log('OK: the cut stream was reopened and plays on')

  // Cut every reopened stream too, in a row, until the player stops reopening (it gives up
  // after a few in a row). Found by an audit: the cut after that counted as the end, so the
  // film was marked watched at the stream header's duration and autoplay moved on. It must
  // say the connection keeps dropping, save where it got to, and stay on the film.
  const url = page.url()
  // The cut command fails when no remux is running at that moment (the reopened one not
  // started yet, or the player already gave up): retry briefly, then judge what is shown.
  const cutNow = async () => {
    for (let i = 0; i < 10; i++) {
      try {
        execSync(process.env.CUT_CMD, { stdio: 'ignore' })
        return true
      } catch {
        await page.waitForTimeout(500)
      }
    }
    return false
  }
  // Set only once every assertion about the given-up state has passed: the loop running out
  // of cuts (the player reopening every time) must fail, not pass unchecked.
  let verifiedFailureState = false
  for (let cuts = 2; cuts <= 8; cuts++) {
    const before = remux.length
    // Soon after the stream starts: CI's film is small (about 6 MB), and a reopened remux
    // there arrives whole in about 4 s, its ffmpeg gone before a later cut.
    await page.waitForTimeout(700)
    if (!(await cutNow()) && (await page.locator('text=keeps dropping').count()) === 0) fail('no stream to cut, and the player said nothing')
    await page.evaluate(() => { const v = document.querySelector('video'); if (v) v.playbackRate = 4 })
    let next = false
    for (let i = 0; i < 120 && !next; i++) {
      await page.waitForTimeout(500)
      const s = await state()
      if (s?.ended && remux.length === before && (await page.locator('text=keeps dropping').count()) > 0) break
      next = remux.length > before
    }
    if (next) continue
    const shown = await page.locator('text=keeps dropping').count()
    const watched = (await api('/v1/items/' + item.id, {}, tok)).userData
    console.log(`after ${cuts} cuts in a row: ${remux.length} streams, error shown=${shown > 0}, marked watched=${watched?.played}, position ${watched?.positionMs} ms`)
    if (!shown) fail('the player stopped reopening without saying the connection keeps dropping')
    if (watched?.played) fail('a stream cut too often in a row counted as the end: the film was marked watched')
    if (page.url() !== url) fail('the player moved on (autoplay) after a cut stream')
    if (!(watched?.positionMs > 0) || watched.positionMs > (item.runtimeMs ?? Infinity) * 0.5) fail(`the saved position is not where the stream got to: ${watched?.positionMs} ms`)
    console.log('OK: cut too often in a row, the player says so and keeps the film unwatched')
    verifiedFailureState = true
    break
  }
  if (!verifiedFailureState) fail(`the player never gave up after ${remux.length} streams: kept reopening without limit, or the end state was never checked`)
} finally {
  await browser.close()
}
