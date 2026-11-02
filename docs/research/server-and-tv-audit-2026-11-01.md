# Server data audit and TV UI performance review (2026-11-01)

Two read-only passes: the live pookiebox database and logs, and the TV UI code. Nothing here is
fixed yet. Spot-checked against the live DB: 854 placeholder items in a library other than
their series', JJK Season 1 = 24 real + 35 virtual (duplicating the real S2 and S3), and both
Mewtwo movies stored as the 2019 film.

## Server bugs found in the data (most severe first)

1. **AniList 429 replaces anime metadata with TMDB's and wipes relations.** 27 of 36 anime
   series lost their AniList poster and related titles (Chainsaw Man, Frieren, Classroom of the
   Elite, Clannad, BEASTARS...) during the 10-28 refresh-all. `metadata/service.go:305` treats a
   provider error like "no match", then `ReplaceRelations(..., [])` at `service.go:269` deletes
   the relations. Fix: an error aborts the refresh instead of overwriting; a global AniList rate
   limiter with Retry-After. This also blocks AniSkip, which needs AniList's per-season ids.
2. **Placeholders duplicate seasons when TMDB merges what the files split.** JJK S1 holds
   virtual 25-59 = the real S2/S3; InuYasha S1 has 140 virtual; Frieren, HxH, Oshi no Ko,
   My Dress-Up Darling similar, and their S2+ files never get episode metadata (TMDB 404 per
   season). `createPlaceholders` (`service.go:722`) never checks real seasons the provider does
   not have. Fix: map by absolute number, or no placeholders past the season's real episodes.
3. **TV placeholders carry the Movies library id.** 25 seasons + 829 episodes (Rick and Morty,
   SpongeBob, Hazbin, Dexter, Teen Titans...) have library 3 while their series are library 4.
   `EnsureVirtualItem` never updates existing rows; only real rows are re-homed. Fix: derive
   from the parent or update on library change, plus a one-time repair.
4. **Filename patterns not parsed.** Tokyo Magnitude 8.0 (`_-_01_`), Berserk Memorial
   (`Episode 1 - Title`): real episodes with NULL index next to placeholders. One Pace: 413
   episodes with NULL index (Wano sorts 59..1), although each `.nfo` has `<episode>`
   (`metadata/local.go:235` never applies it). Fix in `library/naming.go:14-16` and local.go.
5. **Every restart re-matches 13 series and re-runs detection on ~100 seasons** (some 1m45s
   each). `CountUnrefreshedEpisodes` counts unfillable episodes; `SweepAll`
   (`segments/service.go:123`) re-queues any season where one episode lacks an intro or outro
   (finales, specials). Fix: persist "tried at" markers; count only real episodes.
6. **Mewtwo Strikes Back (1998) matched as the 2019 remake** (same anilist/mal ids twice).
   `pickBest` (`metadata/metadata.go:254`) penalises a 21-year gap only -0.2. "Pokemon - The
   Power of One" has no match at all.
7. **Subtitle language detection fails on karaoke-heavy ASS** (1644 text tracks undetected,
   many full English dialogue): `langdetect/langdetect.go:131` keeps fx lines. Skip non-empty
   Effect, de-duplicate text. All 61 mov_text tracks are never sniffed: no `mov_text` entry in
   the extraction map (`library/library.go:323`).
8. **Two primary sources on one movie** (Reze Arc 1080p + 2160p, both `is_primary=1`, no
   label): `library/library.go:258` always sets primary; joins on is_primary duplicate rows.
9. **`items_fts` is stale and unused** (7026 rows for 5476 items, raw filenames): drop it or
   add triggers before search uses it.

Also seen: fingerprinting put KonoSuba S1E1's intro at 105-196 s; AniSkip (a submission made
on the same 1513 s file) and the neighbouring episodes (116, 119 s) suggest about 116-205 s.

Probably fine: TMDB-based season_count disagrees for 11 series (same root as 2); Rick and Morty
carries anilist=137537 (the 2024 anime spin-off; consider no AniList outside anime libraries);
19 outros overlap the preview after them; 26 chapter outros are 2-5 s; watch state is valid.

## TV UI optimizations (static review; highest expected win first)

1. **TV NavHost uses navigation-compose's default 700 ms crossfade** (MainActivity.kt:300;
   verified). Two full pages blend for ~42 frames on every page change. Set the four
   transitions to None (the phone already overrides them).
2. **One focus move recomposes every Home row.** `lastInRow` is a SnapshotStateMap read in
   composition for every row's up/down key (TvHome.kt:144-167); `focusKey` builds a new
   modifier each time (TvUi.kt:193). Use a plain map resolved inside `focusProperties`, cache
   focus modifiers per key, remember the click lambda.
3. **Card focus is read in composition and `onFocused` runs a frame late** (TvUi.kt:213-241).
   Draw the focus frame in the draw phase; call onFocused from onFocusChanged directly; drop the
   unused pointer handler on TV.
4. **Title page fold animates height through recomposition** (TvDetail.kt:241, 249): move it to
   `graphicsLayer`/`layout`.
5. **~39 MB of fonts copied on the main thread at every cold start** (RoxyApp.kt:96-111;
   verified: all 51 fonts are Deflated, so the `openFd` size check always fails). Mark
   ttf/otf/ttc `noCompress`, compare a version stamp, copy on IO. Also move
   `DeviceReport.full` (MediaCodecList) off the main thread in MainActivity.onCreate.
6. **Every Home reload rebuilds every card:** `Item` is unstable (List/Map fields); mark the
   UI models `@Immutable` or add a stability config; skip identical emissions; resolve only
   the first hero on TV.
7. **RoxyImage composes twice per card** (a `loaded` flag flips on success): use a painter
   placeholder instead (Components.kt:108-125).
8. **Partial `alpha()` forces offscreen layers** on dimmed cards and watched thumbs: use the
   image's alpha or `CompositingStrategy.ModulateAlpha`.
9. **Rail open/close animates width through recomposition** (TvRail.kt:455-512).
10. **Browse re-sorts 1000 items and swaps the grid on every category focus** (TvBrowse.kt:131):
    debounce, and move `categories()` off composition.
11. **Images:** backdrops request 1920 px (1280 looks the same under the scrim); logos 2x their
    shown width; size Coil's memory cache for 2.4 GB and cap decode parallelism.
12. Smaller: hero overdraw (merge the two scrims), `contentType` on Home's lazy lists, parallel
    season-count fetch, remembered action/tab lists on title pages, update-state collection
    moved into the banner.
13. **Build:** add a baseline profile (no AOT for sideloaded app code otherwise) and a Compose
    stability config.

Measure on the perf build, force-compiled first (`cmd package compile -f -m speed-profile`):
gfxinfo framestats for Right along a row, Down between rows, open/Back a title, rail, fold;
`am start -W` for cold start before and after item 5.

## Edge-case bugs (second pass, each confirmed with live rows or a reproduction)

KonoSuba's S1 OVA is filed in the library as `Specials/S00E03` ("Gods Blessings On This
Wonderful Choker!"), not S01E11.

1. **AniList attaches a later season's Crunchyroll episode list to an earlier entry.** KonoSuba
   S1 has a virtual S01E11 titled with S3's finale and an S3 thumbnail; all 23 Mushoku Tensei S1
   episodes carry S2's titles and stills, plus a fake S01E24. `anilist.go:414` raises the count
   to the list's length; the duplicate-list guard only catches a list repeated on a later season.
   Fix: reject lists that do not fit the entry; prefer TMDB titles. (server)
2. **Title-page Play looks only at the selected season** (TvDetail/PhoneSeries). Fixed in the
   app: `upNextOf` over all episodes; the page opens on that season.
3. **A rewatch drops out of Continue Watching** (`store/userdata.go:132` requires played=0;
   `continue.go:90` next-up only unplayed). (server)
4. **TVDB-numbered files get TMDB's metadata** (KonoSuba S00E03/E04 named after other specials,
   JJK 0 stored as "New Year's Special", 15 SpongeBob episodes). Match by the filename's title
   within the season, or TMDB's TVDB-order episode group. (server)
5. **Placeholders from AniList's sequel chain make fake seasons** (Yu-Gi-Oh! virtual S2 = GX,
   180 eps; S3 = 5D's, 154): `createPlaceholders` uses AniList as `src`. Use TMDB only. (server)
6. **Mark season watched also marks placeholders**, so episodes arriving later are already
   watched (`store/userdata.go:90-95`: add `is_virtual = 0`). (server)
7. **Absolute-numbered files share a GUID across seasons** (`library.go:429` `abs%04d`): two
   seasons' episode 1 become one item. No live case yet. (server)
8. **Absolute mapping ignores the season folder** (InuYasha: The Final Act S7 shows S1 titles;
   `service.go:~515`). (server)
9. **Multi-episode files leave fake missing episodes** (57 files, e.g. SpongeBob S01E01-E03:
   `EpisodeEnd` parsed but unused). (server)
10. **Signs & songs not recognised** ("[FOR DUB]", "ForcedNarrative"), and untagged dual-audio
    tracks defeat the dub pairing. Fixed in the app: wider regex; language from the title.
11. **TV Watch Next row for a finished episode restarts it** (row kept until 97 %, server marks
    watched at 90 % and resets). Fixed in the app: removed at 90 %. Still open: publish the next
    episode's row, one row per series.

Unconfirmed: unaired-today shown as "Missing" (UTC date compare, `availability.go:13`);
`isAnime` from an AniList tag rather than the library's anime mode (`home.go:126`); autoplay
after a finale goes to a special; "Show - 12.5" sorts first.
