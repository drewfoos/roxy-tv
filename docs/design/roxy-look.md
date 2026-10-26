# The Roxy look

What the phone app looks like today (2026-10-26), written down so the desktop and web
clients draw the same thing. The phone follows Crunchyroll's app with Roxy's own colours
(blue, never orange) and a Discover tab Crunchyroll does not have. Source of truth for
the values: `apps/android/app/src/main/kotlin/tv/roxy/tv/ui/theme/Theme.kt` and
`ui/phone/PhoneUi.kt`; screenshots were taken on a Galaxy S25 Ultra.

## 1. Tokens

| Token | Value | Used for |
| --- | --- | --- |
| background | `#0B0B0F` | every page; near black, not pure black |
| surface | `#17171E` | cards on Account, dialogs, bottom sheets |
| surfaceHigh | `#23232C` | image placeholders, search box, hairlines |
| navBar | `#23252B` | the bottom bar (Crunchyroll's grey over the black page) |
| buttonGrey | `#2B2B2B` | secondary buttons ("Try again", "Media folders") |
| chipGrey | `#333333` | unselected chips, the Search field |
| accent | `#3EA6FF` | brand blue: selected tab, Play pill, bookmark ring, progress bars, links, tab underline |
| accentLight / accentDark | `#8FD0FF` / `#1B5FA8` | hat shading, pressed states |
| onBrand | `#07182B` | text and icon on the blue pill (dark, like Crunchyroll's black-on-orange) |
| onBackground | `#F2F2F5` | body text |
| muted | `#9AA0A6` | captions, years, "Dub \| Sub" under cards, placeholder text |
| factsGrey | white @ 72% | the facts line and genres on title pages |
| agePill | `#4A4A52` | background of the "14+" / "PG" block |
| error | `#FF6B6B` | |
| skeleton | `#1C1C24` to `#2A2A34` | shimmer sweep, 1.3 s loop |

Type: the system sans (Roboto on Android; use Inter or the OS UI font on desktop), no
display face. Weights: Bold for titles and buttons, SemiBold for card names, Normal for
body. Sizes on the phone: tab title 24, row title 18, section title 17, hero title 26
(when there is no logo), card name 14, caption 12, badge 10, body 15/22 line height.

Radii: posters 6 (grids) or 8 (rows), thumbs 6, buttons 4, pills full, chips 17, search 21.
Page gutter 16. Rows are padded 16 at the edge and 10 between cards.

Icons: Material Symbols, outlined, 26 in the bar, 22 in cards, white; the brand mark is
the witch hat (`ui/phone/WitchHat.kt`, a shaded blue vector with a gold band and sparkle).

## 2. Components

**Title card** (shows and movies in rows): 2:3 poster, name on one line, then
"Dub | Sub" (anime) or "2026 · Movie" in muted, with the three-dot menu at the right.
Sized so two and a half fit across a phone.

**Poster tile** (grids): the 2:3 poster alone, no caption. Three across on a phone.

**Thumb tile** (episodes, films in Recently added): 16:9 still, a translucent play
ring in the middle, "Dub | Sub" pill top-left, a download corner top-right, the blue
progress bar along the bottom edge; caption = series name, then "S1 E5  Title".

**Continue card**: one 16:9 card almost full width with the next one peeking in, a
"12m left" pill bottom-right over the picture, progress bar, series name and episode
under it, snapping scroll.

**Hero** (Home only): full-bleed art, 640 tall on a phone, running under the status bar.
A dark gradient from 30% down; over its foot, left-aligned: the title logo (or the name
at 26 ExtraBold), the facts line "★ 8.5  Dub | Sub  •  Action, Comedy" at 12 in white 85%,
three lines of synopsis at 13/18, then the Play row. Below the row, story-style timer
bars: one segment per slide, done ones full, the current one filling in blue over 8 s,
the rest white 55%. Swiping resets the timer. Picks are the shows worth resuming or
starting (`HeroPick`), each with a play target ("Resume S01E05" / "Start watching S01E01").

**Play row**: a blue pill filling the width ("Resume S01E03", bold, dark text, 44 tall)
and a 44 round bookmark outlined in blue to its right (filled when saved).

**Buttons**: primary = white, 4 radius, black bold label (Netflix) or the blue pill
when it is the page's main action; secondary = buttonGrey. Text links are accent blue.

**Chips**: 34 tall pills, white with black text when selected, chipGrey otherwise; a
trailing icon for a swap (Series ⇄ Movies) or a sheet (Popular with a tune icon).

**Section tabs**: bold 15 labels, open one white, others muted, a 2 px blue underline
on a 1 px surfaceHigh hairline (Episodes | More like this; Watchlist | History | Downloads).

**Season row**: "Season 1 ▾" bold 16 at the left opening a full page of seasons with
episode counts (Crunchyroll's, not a popup), a download-all control at the right.

**Episode row**: 16:9 thumb at the left (play ring, "24m" or "20m left" pill, "Watched"
pill with a replay icon when done, progress bar), "3. Title" bold at the right over two
lines, then a download circle and the three-dot menu.

**Badges**: "Dub | Sub" translucent black pill at 10 SemiBold; age "14+" / "PG" small
block; Discover's corner box (check = in library, clock = requested, "In cinemas",
"S1 E5 · 45%" while downloading).

**Tab header**: the tab name at 24 Bold left, Cast and Search icons right. Home has the
hat instead of a name, and its header is transparent over the hero and turns black over
the first 160 px of scroll.

**Search box** (Discover): rounded surfaceHigh field, magnifier, "Search shows and
movies on TMDB", results replace the rows as a 3-column grid as you type.

**Skeletons**: shimmer bones shaped like the content (row title + four posters; grids).
Home shows one blue spinner instead; title pages paint their header from the card that
opened them and only the episodes load in.

**Art fallback**: a title with no poster gets a gradient tinted per title (hue 200–270,
blue to violet), a faint hat and the name in white, so a row of them is not grey boxes.

## 3. Screens

**Bottom bar**: Home, Discover, Browse, My Lists, Account; 56 tall, 26 icons, 11
labels, white, the selected one blue, Account drawn as the profile avatar.

**Home**: hero carousel, "Recently added" (title cards, shows and movies by date added),
"Continue Watching" (continue cards, one per show), then "Series" and "Movies" rows.
Empty library: the hat, "Add your media" or "Nothing to watch yet" with the right next
step for an admin vs a member. Server unreachable: sonar rings around the hat, "Can't
reach your server", Watch downloads / Try again.

**Discover**: header, search box, chips (Series ⇄, Anime, Popular ⚙, Clear), then
"Your requests", "Trending this week", "New anime this season", popular and top-rated
anime, anime movies, popular movies, upcoming, popular series. Filters open a bottom
sheet (Show, Sort by, Genres, Original language, Years, Rating, Status) and turn the page
into a paged 3-column grid. A title page mirrors the library's with Request instead of
Play, per-season requests and live download state.

**Browse**: header, Series | Movies chips, a 3-column poster grid of the whole library
sorted by title. Search (from the magnifier) filters the same list by title and genre.

**My Lists**: Watchlist | History | Downloads tabs. Watchlist is a poster grid; History
is a 2-column grid of thumb tiles newest first; Downloads lists what is on the device.

**Account**: the profile card (avatar, name, "user · server", Edit profile), Switch
profile, Change password; PLAYBACK (audio language, subtitle language, subtitle rule,
quality on Wi-Fi / mobile data); then Downloads, Server (admin: library, media folders,
Sonarr/Radarr, fanart, transcoding, webhooks, users, requests), legal, version.

**Title page** (series, movie, Discover): tall key art under a black status bar with a
short fade at its top; a plain white X at the left and Cast + ⋮ at the right over it.
The art stays put and darkens as the page scrolls over it; the header strip turns solid
and the name fades into it once the logo has gone under. Centred under the art: logo,
facts line "14+ · Dub | Sub · ★ 8.5 · 2016 · 4 Seasons", genres, three lines of synopsis
ending in "…", "More details" link; then Episodes | More like this tabs, the season row,
the episode rows. The Play row is pinned at the bottom over a dark fade. A movie's tab
is "Movie" and its one row is the film as a thumb tile with "83m left".

**Episode page**: the player in a 16:9 box at the top (fullscreen on rotate or by the
button, PiP on leaving); under it the series name in blue, "E3 - Title" bold, "14+ ◆
Dub | Sub", the file's facts (codec, size) muted with download and ⋮ at the right, the
synopsis with an inline "Read more", Content Advisory, "Next Episode" as one episode row,
and "All Episodes" as a centred blue link.

**Player** (YouTube's layout): a thin seek bar with a round thumb (red on YouTube, blue
here), trickplay thumbnails while scrubbing; left: play/pause, volume, time / total;
right: captions, settings (quality as plain resolutions, audio, subtitles), PiP,
fullscreen; centre: translucent dark discs for −10 s / play / +10 s; Skip intro / Skip
outro pills; autoplay always on with a next-episode card at the end; "Ends at 3:30 AM"
readout.

## 4. Mapping to a desktop / web layout

Crunchyroll's site is the same system at a wider size. What changes:

- **Top bar instead of the bottom bar**: the hat at the left, then Home, Discover,
  Browse (a dropdown on Crunchyroll: Popular, New, Genres), My Lists; search and the
  profile avatar at the right. Dark `#23252B`, 60 tall, sticky; transparent over the Home
  hero and solid once scrolled, as on the phone.
- **Hero**: full-width, 16:9-ish cropped to about 70% of the viewport height, text and
  the Play row left-aligned in a column no wider than 560; the timer bars under the row
  become dots or stay as bars at the same width as the button.
- **Rows**: title cards about 200 wide, six to eight per row, with arrow buttons at the
  row ends on hover; a card lifts and shows its synopsis on hover (Crunchyroll), the
  three-dot menu appears on hover.
- **Grids** (Browse, Watchlist, search): posters 180–220 wide, as many columns as fit,
  a filter bar at the top (chips become a dropdown row on Crunchyroll).
- **Title page**: two columns above 1024 wide: the art fills the top as a wide
  backdrop with the logo and facts over its left foot, the Play row under them; below,
  the Episodes tab, season dropdown and a **grid of episode cards** (Crunchyroll shows
  four 16:9 cards per row, "S1 E3 - Title" under each) rather than a list. Narrow windows
  fall back to the phone's list.
- **Watch page**: the player fills the width up to 16:9 of the viewport; below it the
  series name, "E3 - Title", facts, synopsis; "Next Episode" and "More like this" on
  the right as a column above 1200 wide (Crunchyroll's "Up next").
- **Discover**: the same rows and filters; the filter sheet becomes a side panel.
- **Account**: a settings page with the sections as a left list.
- **Keyboard**: arrows move between cards, Enter opens, Escape closes a page, Space
  pauses, ← → seek 10 s, F fullscreen, M mute, C captions.

Everything else (colours, radii, pills, badges, skeletons, the art fallback, the
"Dub | Sub" rule, the facts line order) stays exactly as above.
