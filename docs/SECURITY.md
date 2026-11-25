# Security: what Roxy does, and what it leaves to you

Roxy Server is written for a home: a few people, a library the owner collected, a network
the owner runs. This page says what the server defends by itself, where its defences end,
and what is yours to arrange.

## First-time setup

A new server has no account. The first person to open the app or the web page creates the
admin, and two things are asked of them:

- **They come from the home network.** The connection's own address has to be private
  (forwarding headers count only from `ROXY_TRUSTED_PROXIES`, and a public address
  anywhere in them fails).
- **They know the setup code.** The server makes one when it starts without an account,
  and writes it into its log and into `setup-code.txt` in its data folder
  (`docker compose logs roxy`). The wizard asks for it.

The address alone does not show who somebody is. A guest on the Wi-Fi has a private
address; and wherever the client's real address is hidden from the server (Docker Desktop
hands every client over from its own gateway, a plain TCP tunnel from the internet arrives
from the tunnel's local end) everybody has one. Only the owner can read the server's log.

Ten wrong codes and a new one is made. Once the admin exists the code and its file are
gone and setup is closed for good. `ROXY_ADMIN_PASSWORD` creates the admin at start
instead, with no wizard and no code.

## Sign-in and transport

- Passwords are bcrypt hashes; sign-in is throttled per address and per name; every
  sign-in is a device with its own token, which the owner can end.
- **Roxy serves plain HTTP.** On the home network and over a VPN such as Tailscale that
  is the usual arrangement, and it means what it says: on the home network a password
  travels unencrypted, readable by whoever can watch that network. A VPN encrypts the
  path itself.
- **Do not publish the port to the internet as it is.** Put a reverse proxy with HTTPS
  in front and name it in `ROXY_TRUSTED_PROXIES`, or reach the server over a VPN.
  Behind HTTPS the server sends `Strict-Transport-Security`, and the sign-in cookie is
  marked `Secure`.
- The apps do not send a login by plain HTTP to an address outside the private ranges.
  The web page cannot know as much about its own address, so it warns above the form
  instead, and the server logs a warning each time a password arrives unencrypted from
  outside the private networks.

## The web app and its token

The web app keeps its sign-in token in the browser's local storage and sends it as a
header; an `HttpOnly`, `SameSite=Strict` cookie carries the same token for what a page
fetches by itself (pictures, video). A script running in the page could read local
storage, so the defence is that no foreign script runs there:

- the Content-Security-Policy allows scripts from the server only (no inline script, no
  `eval` in the page; the subtitle renderer's worker alone may build functions);
- the app never builds HTML from data (no `innerHTML`; a test fails if one appears);
- files a library can carry (fonts, subtitles, pictures) are served as data: fixed
  content types, `nosniff`, and a sandbox policy on attachments;
- links out are built by the server from provider ids on fixed `https://` prefixes.

Moving the web app to the cookie alone would take the token out of a script's reach
altogether. It is not done yet.

## Media files and ffmpeg

Media comes from wherever the owner got it, and reading it is ffmpeg's job: a large
parser of many formats, in which holes are found every year. Roxy never links it; every
use is a child process, and each is held like this:

- inputs are opened from disk only (`-protocol_whitelist file,pipe`), so a file that is
  really a playlist cannot make the server fetch addresses;
- every run has a time limit, and its output is capped;
- the library is mounted read-only, and paths outside the library and the data folder
  are refused before ffmpeg sees them (symlinks included);
- the server, and ffmpeg with it, runs as an ordinary user (`PUID`/`PGID`), and the
  container forbids regaining privileges (`no-new-privileges`).

What this does not do: ffmpeg runs as the same user as the server, in the same container.
A file that took ffmpeg over could read what the server can, its database included, and
write to the data folder. Keeping the image current is the defence against known holes
(see below); a fence between ffmpeg and the server's own files is not built yet.

## Conversions

A conversion (a transcode, a remux, a song converted for a phone) is an ffmpeg run, the
most a request can cost. They have limits of their own, apart from the API's rate limit:

- the server runs 4 transcodes, 8 remuxes and 2 song conversions at once;
- one device has at most half of them (2 transcodes, 4 remuxes): its next one ends its
  oldest (a player has left that one behind), so no device can hold every place;
- transcode and remux starts are counted per device: 30 at once, then one a second;
- song conversions wait in a queue of 32, of which one client may hold 6; one that
  nobody waits for any more is dropped before it starts. Past the limits the answer is
  `429` with `Retry-After`;
- a stream whose player stops reading is ended after ten minutes, or at once when the
  same device asks for its next.

## Keeping what Roxy is built from current

- **Dependabot** opens a pull request for new releases of the Go modules, the web app's
  packages, the Android app's libraries, the Docker base images and the CI actions.
- The **Security** workflow runs every week and on every change to a lock file:
  `govulncheck` (the Go standard library and modules, as far as the server's code
  reaches them), `npm audit` (what ships in the web app), and a scan of the built server
  image's system packages, the ffmpeg build among them.
- `server/go.mod` names the oldest Go patch release the server may be built with; the
  Docker build fails on an older one rather than ship its holes.
- The Android app's player library (mpv, ffmpeg and libass, built from pinned sources by
  `apps/android/tools/libmpv-build`) is not covered by any scanner: its versions are
  raised by hand, in `recipe.patch`.

## Reporting a problem

For something that should not be public yet, use GitHub's private vulnerability report
on the repository (Security → Report a vulnerability) where it is offered; otherwise open
an issue that says only that you have something to report. Anything else: an issue.
