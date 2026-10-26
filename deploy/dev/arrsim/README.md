# arr test bench

Real Sonarr and Radarr in Docker, wired to **arrsim** (`server/cmd/arrsim`): a fake
Newznab indexer and SABnzbd download client. It lets Roxy's request and download
tracking be tested end to end on a laptop: nothing is fetched from the internet, and
every "download" is a small locally generated test video.

```
sudo MEDIA=/path/to/roxy/testdata/out deploy/dev/arrsim/up.sh
```

`up.sh` starts the containers (host networking: Sonarr :8989, Radarr :7878), builds and
starts arrsim on :8099, and runs `setup.py`, which adds the `/media` root folder,
arrsim as indexer and download client, and a webhook to Roxy on :8096 (an API key is
created with roxy/roxy). The keys are printed at the end; enter them in the app under
Account → Server → Sonarr & Radarr (URL `http://localhost:8989` / `:7878`).

Control what the next grabs do:

```
curl -X POST 'http://localhost:8099/sim/mode?value=ok'        # 90 s download, imports
curl -X POST 'http://localhost:8099/sim/mode?value=slow'      # four times slower
curl -X POST 'http://localhost:8099/sim/mode?value=stall'     # stops at 37 %, paused
curl -X POST 'http://localhost:8099/sim/mode?value=fail'      # fails at the end
curl -X POST 'http://localhost:8099/sim/mode?value=badimport' # completes with no video
curl -X POST 'http://localhost:8099/sim/mode?value=none'      # the indexer finds nothing
curl http://localhost:8099/sim/state                          # current downloads
```

Every release comes in two versions from different "groups", so blocklisting one
(Try another release) finds the other. Anime is searched by absolute episode number.

Notes: arrsim keeps its downloads in memory, so restarting it drops what was in flight.
Imported files land in the library folder (`MEDIA`) as real items; delete them from
Sonarr / Radarr with their files to clean up. Config and downloads live in
`/srv/arrsim` (override with `ARRSIM_HOME`).
