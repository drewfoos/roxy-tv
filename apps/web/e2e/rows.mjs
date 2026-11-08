// Horizontal card rows (Discover, Home, title pages share the Row component) in a real
// browser: arrows page by whole cards (none skipped or repeated, quick clicks each a page),
// mouse drag (release outside the window, a new drag during a glide, flicks, click vs drag,
// no hover panels or text selection), resize and keyboard focus. Discover's rows are
// served by the test itself (no TMDB key needed): two rows of 20 titles.
//
// Env: BASE (the server), ROXY_USER / ROXY_PASS (an admin), CHROME (optional: a browser
// path; otherwise Google Chrome).
import { chromium } from 'playwright-core'
const base = process.env.BASE ?? 'http://127.0.0.1:18097'
const api = async (path, opts = {}, tok) => {
  const r = await fetch(base + path, { ...opts, headers: { 'Content-Type': 'application/json', ...(tok ? { Authorization: 'Bearer ' + tok } : {}) } })
  const t = await r.text(); try { return JSON.parse(t) } catch { return t }
}
const login = await api('/v1/auth/login', { method: 'POST', body: JSON.stringify({ username: process.env.ROXY_USER, password: process.env.ROXY_PASS, client: 'roxy-web', deviceId: 'probe', device: 'probe' }) })
const tok = login.token
const legal = await api('/v1/legal/status', {}, tok)
await api('/v1/legal/accept', { method: 'POST', body: JSON.stringify({ version: legal.version }) }, tok)
const prof = (await api('/v1/me/profiles', {}, tok)).profiles[0]
const browser = await chromium.launch({ channel: process.env.CHROME ? undefined : 'chrome', executablePath: process.env.CHROME })
// Discover's rows and requests, served here: 20 titles a row, posters drawn as SVG.
const poster = (i) => 'data:image/svg+xml,' + encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" width="200" height="300"><rect width="200" height="300" fill="hsl(${(i * 37) % 360} 50% 35%)"/><text x="20" y="160" font-size="48" fill="#fff">${i}</text></svg>`)
const items = (row) => Array.from({ length: 20 }, (_, i) => ({ type: 'movie', tmdbId: row * 100 + i + 1, title: `Title ${row}-${i + 1} with a longer name`, year: 2020, poster: poster(i), overview: 'A synopsis.', rating: 7 }))
const stub = async (ctx) => {
  await ctx.route('**/v1/discover', (r) => r.fulfill({ json: { rows: [{ key: 'a', title: 'Trending', items: items(1) }, { key: 'b', title: 'Popular', items: items(2) }], requestsEnabled: { tv: false, movie: false } } }))
  await ctx.route('**/v1/discover/requests', (r) => r.fulfill({ json: { items: [] } }))
}
let fails = 0
const report = (name, ok, detail = '') => { if (!ok) fails++; console.log((ok ? 'PASS ' : 'FAIL ') + name + (detail ? '  ' + detail : '')) }

for (const [w, h] of [[1920, 1080], [3440, 1440], [1100, 800]]) {
  console.log(`--- ${w}x${h}`)
  const ctx = await browser.newContext({ viewport: { width: w, height: h } })
  await stub(ctx)
  const page = await ctx.newPage()
  await page.goto(base + '/')
  await page.evaluate((s) => localStorage.setItem('roxy.session', JSON.stringify(s)), { token: tok, userId: login.user.id, userName: login.user.name, isAdmin: true, profileId: prof.id, profileName: prof.name, profileHasAvatar: false, profileAvatarKey: '' })
  await page.goto(base + '/discover')
  await page.waitForSelector('.row__track .dcard', { timeout: 20000 })
  await page.waitForTimeout(1500)
  const T = '.row:nth-of-type(1) .row__track'
  const row = page.locator('.row').first()
  const track = row.locator('.row__track')
  // State of the first row: scroll, which cards are fully visible, alignment of the first.
  const st = () => track.evaluate((el) => {
    const r = el.getBoundingClientRect()
    const pad = parseFloat(getComputedStyle(el).scrollPaddingLeft) || 0
    const cards = Array.from(el.children).map((c) => c.getBoundingClientRect())
    const full = cards.map((c, i) => (c.left >= r.left + pad - 1 && c.right <= r.right - pad + 1 ? i : -1)).filter((i) => i >= 0)
    const first = full[0] ?? -1
    const max = el.scrollWidth - el.clientWidth
    const aligned = first < 0 ? false : Math.abs(cards[first].left - (r.left + pad)) <= 1.5 || Math.abs(el.scrollLeft - max) <= 1.5
    return { left: el.scrollLeft, max, full, first, last: full[full.length - 1], aligned, n: cards.length, free: el.classList.contains('row__track--free'), hovers: Array.from(el.querySelectorAll('.card__hover')).filter((h) => getComputedStyle(h).display !== 'none').length }
  })
  const settle = async () => {
    let prev = -1
    for (let i = 0; i < 40; i++) {
      const s = await st()
      if (Math.abs(s.left - prev) < 0.5) return s
      prev = s.left
      await page.waitForTimeout(100)
    }
    return st()
  }
  const reset = async () => { await track.evaluate((el) => { el.scrollTo({ left: 0 }) }); await page.waitForTimeout(300); return settle() }
  const box = await track.boundingBox()
  const midY = box.y + box.height * 0.35

  // A. Arrow paging: each page starts at the first card that was not fully visible, aligned.
  let s0 = await reset()
  await row.hover()
  let skipped = [], misaligned = 0, pages = 0
  while (pages < 10) {
    const before = await st()
    if (before.left >= before.max - 1) break
    await row.locator('.row__arrow--right').click()
    const after = await settle()
    pages++
    if (!after.aligned) misaligned++
    const expectFirst = before.last + 1
    if (after.left < after.max - 1 && after.first !== expectFirst) skipped.push(`${before.last}->${after.first}`)
  }
  report('arrow right pages start at the next unseen card', skipped.length === 0, skipped.join(' '))
  report('arrow right pages end aligned', misaligned === 0, `${misaligned} misaligned of ${pages}`)
  // And back.
  skipped = []; misaligned = 0
  for (let i = 0; i < 10; i++) {
    const before = await st()
    if (before.left <= 1) break
    await row.locator('.row__arrow--left').click()
    const after = await settle()
    if (!after.aligned) misaligned++
    if (after.left > 1 && after.last !== before.first - 1) skipped.push(`${before.first}->${after.last}`)
  }
  report('arrow left pages end at the card before the first seen', skipped.length === 0, skipped.join(' '))
  report('arrow left pages end aligned', misaligned === 0, `${misaligned} misaligned`)

  // B. Rapid clicks: three quick clicks move three pages, aligned.
  s0 = await reset()
  const perPage = s0.full.length
  await row.hover()
  for (let i = 0; i < 3; i++) { await row.locator('.row__arrow--right').click({ delay: 10 }); await page.waitForTimeout(60) }
  let s = await settle()
  report('three quick arrow clicks move three pages', s.first === Math.min(3 * perPage, s.n - perPage) || s.left >= s.max - 1, `first=${s.first} perPage=${perPage}`)
  report('three quick arrow clicks end aligned', s.aligned)

  // C. Mouse released outside the window: the row must stop following the mouse.
  s0 = await reset()
  await page.mouse.move(box.x + box.width * 0.6, midY)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width * 0.4, midY, { steps: 5 })
  // The release happened outside: the next move arrives with no button held.
  await page.evaluate(() => window.dispatchEvent(new PointerEvent('pointermove', { pointerType: 'mouse', buttons: 0, clientX: 10, clientY: 300, bubbles: true })))
  const leftA = (await st()).left
  await page.evaluate(() => window.dispatchEvent(new PointerEvent('pointermove', { pointerType: 'mouse', buttons: 0, clientX: 600, clientY: 300, bubbles: true })))
  await page.waitForTimeout(100)
  const leftB = (await st()).left
  await page.mouse.up()
  const sC = await settle()
  // Following the mouse would have moved the row ~590 px; letting go only settles on a card.
  const cardW = await track.evaluate((el) => el.children[0].getBoundingClientRect().width)
  report('release outside the window stops the drag', Math.abs(sC.left - leftA) < cardW && sC.aligned, `moved ${Math.round(sC.left - leftA)} px after release (card ${Math.round(cardW)})`)

  // D. Hover panels stay shut while dragging; E. a second drag during the glide keeps snapping off.
  s0 = await reset()
  await page.mouse.move(box.x + box.width * 0.7, midY)
  await page.waitForTimeout(150)
  await page.mouse.down()
  let maxHovers = 0
  for (let i = 1; i <= 10; i++) { await page.mouse.move(box.x + box.width * 0.7 - i * 40, midY); maxHovers = Math.max(maxHovers, (await st()).hovers) }
  report('no hover panel while dragging', maxHovers === 0, `${maxHovers} open`)
  await page.mouse.up()
  // Straight into a second drag while the first glide runs.
  await page.mouse.down()
  const startLeft = (await st()).left
  let drift = 0, snappedDuring = false
  for (let i = 1; i <= 12; i++) {
    await page.mouse.move(box.x + box.width * 0.7 - 400 - i * 7, midY)
    await page.waitForTimeout(60)
    const now = await st()
    if (!now.free) snappedDuring = true
  }
  report('snapping stays off for a drag started during a glide', !snappedDuring)
  await page.mouse.up()
  s = await settle()
  report('a drag ends aligned on a card', s.aligned)

  // Flick: a very fast short drag coasts at most about a screen, and stops on a card.
  s0 = await reset()
  await page.mouse.move(box.x + box.width * 0.8, midY)
  await page.mouse.down(); await page.mouse.move(box.x + box.width * 0.8 - 300, midY, { steps: 2 }); await page.mouse.up()
  s = await settle()
  const view = await track.evaluate((el) => el.clientWidth)
  const step = await track.evaluate((el) => el.children[1].getBoundingClientRect().left - el.children[0].getBoundingClientRect().left)
  report('a hard flick coasts at most a screen and lands on a card', s.left - s0.left <= view + 300 + step / 2 + 1 && s.aligned, `moved ${Math.round(s.left - s0.left)} of view ${view}`)

  // F. A press that barely moves is a click; a drag is not.
  s0 = await reset()
  const url0 = page.url()
  await page.mouse.move(box.x + box.width * 0.5, midY)
  await page.mouse.down(); await page.mouse.move(box.x + box.width * 0.5 - 200, midY, { steps: 8 }); await page.mouse.up()
  await page.waitForTimeout(400)
  report('a drag does not open the card under the mouse', page.url() === url0)
  await settle()
  const card = track.locator('.dcard').nth((await st()).first)
  const cb = await card.boundingBox()
  await page.mouse.move(cb.x + cb.width / 2, cb.y + cb.height * 0.3)
  await page.mouse.down(); await page.mouse.move(cb.x + cb.width / 2 + 3, cb.y + cb.height * 0.3); await page.mouse.up()
  await page.waitForTimeout(600)
  report('a press moving 3 px still opens the card', page.url() !== url0, page.url().replace(base, ''))
  if (page.url() !== url0) { await page.goBack(); await page.waitForSelector('.row__track .dcard'); await page.waitForTimeout(800) }

  // G. A drag must not leave text selected.
  s0 = await reset()
  const nb = await track.locator('.card__name').nth(1).boundingBox()
  await page.mouse.move(nb.x + 5, nb.y + nb.height / 2)
  await page.mouse.down(); await page.mouse.move(nb.x - 250, nb.y + nb.height / 2, { steps: 10 }); await page.mouse.up()
  const sel = await page.evaluate(() => String(window.getSelection()))
  report('a drag starting on a card name selects no text', sel.trim() === '', JSON.stringify(sel.slice(0, 40)))
  await settle()

  // H. Resize: stays aligned on a card.
  await row.hover(); await row.locator('.row__arrow--right').click(); await settle()
  await page.setViewportSize({ width: w - 137, height: h }); await page.waitForTimeout(600)
  s = await settle()
  report('resize keeps the row aligned on a card', s.aligned, `left=${Math.round(s.left)}`)
  await page.setViewportSize({ width: w, height: h }); await page.waitForTimeout(600); await settle()

  // I. Keyboard: tabbing to a card off screen brings it fully into view, aligned.
  s0 = await reset()
  await track.locator('a').first().focus()
  const linksPerCard = await track.evaluate((el) => el.children[0].querySelectorAll('a').length)
  for (let i = 0; i < s0.full.length * linksPerCard + 1; i++) await page.keyboard.press('Tab')
  await page.waitForTimeout(700)
  const kb = await page.evaluate((sel) => {
    const el = document.querySelector(sel); const a = document.activeElement
    if (!el || !a || !el.contains(a)) return { inRow: false, active: a ? a.outerHTML.slice(0, 80) : null }
    const r = el.getBoundingClientRect(); const c = a.getBoundingClientRect(); const pad = parseFloat(getComputedStyle(el).scrollPaddingLeft) || 0
    return { inRow: true, visible: c.left >= r.left + pad - 2 && c.right <= r.right - pad + 2 }
  }, T)
  s = await settle()
  report('tabbing to an off-screen card shows it whole', kb.inRow && kb.visible, JSON.stringify(kb))
  report('tabbing leaves the row aligned', s.aligned)
  await ctx.close()
}
await browser.close()
console.log(fails ? `${fails} FAILED` : 'ALL PASS')
process.exit(fails ? 1 : 0)
