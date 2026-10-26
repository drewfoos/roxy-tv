# Roxy Web

The browser client for Roxy Server: the computer app for Windows, Linux and macOS, and
what the server serves at `/`. Crunchyroll's layout at desktop width, in Roxy's colours;
the design spec is `docs/design/roxy-look.md`.

## Playback: no transcoding

The browser decodes the file itself from `GET /v1/stream/{sourceId}?mt=...`: MKV and
MP4, H.264 (8-bit and Hi10P), HEVC 10-bit (GPU), AV1. Subtitles are the MKV's own ASS
track rendered by **jassub** (libass in WebAssembly) with the MKV's own fonts, fetched
from the attachments endpoint. Verified in Chrome on every file in the test library,
including KonoSuba's karaoke opening and ending (`docs/design/roxy-look.md` has the
look; the memory of the test is in the project notes).

Targets: Chrome and Edge. Firefox plays neither MKV nor Hi10P. AC3, E-AC3 and TrueHD
audio does not decode in Chrome; those files (and audio-track switching) need the
server's remux endpoint, which is on the roadmap.

A trap: jassub's worker fetches the WebAssembly relative to **itself**, so every URL
given to it must be absolute. Vite's `?url` imports are, so keep using them.

## Layout

```
src/
  main.tsx              mounts the app
  app/App.tsx           routes, auth gate (login -> profile picker -> app)
  api/                  session store, fetch client, wire types, item helpers (episode code, badges, image URLs)
  styles/               tokens.css (the palette and type scale), app.css (every component)
  components/           TopBar, Hero, cards and rows, icons, skeletons
  pages/                Login, Profiles, Home, Browse, Discover, Lists, Account, Series, Movie, Watch
  player/               the <video> player with jassub subtitles and progress reporting
```

## Build

```
npm ci
npm run dev      # http://127.0.0.1:5173, /v1 proxied to ROXY_SERVER (default http://127.0.0.1:8096)
npm run check    # tsc
npm run build    # into server/internal/web/dist, embedded by `go build`
```
