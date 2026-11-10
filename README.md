# Radio Server POC

Minimal digital radio: push voice from a browser, distribute as Icecast MP3 + HLS (CDN-ready).

```
Browser mic ──WebSocket(webm/opus)──► ingest (Go) ──► ffmpeg per channel ──┬─► Icecast  → http://host:8000/<ch>.mp3
Studio app (BUTT/OBS) ──────── could push straight to Icecast ─────────────┘└─► HLS      → http://host:8090/hls/<ch>/index.m3u8 → CDN
```

- Channels are **always on**: silence when nobody broadcasts, so stream URLs never drop.
- One broadcaster per channel; listeners never touch the ingest service.

## Run

```sh
./run.sh            # build + start, prints all URLs, token and passwords
./run.sh status     # show URLs again
./run.sh logs       # follow logs
./run.sh down       # stop
```

On first run it creates `.env` with random passwords and a broadcast token. Edit `.env` (e.g. `CHANNELS`) and run `./run.sh` again to apply changes.

| URL | What |
|---|---|
| http://localhost:8090/ | Listener page (MP3 + HLS players per channel) |
| http://localhost:8090/broadcast | Broadcaster page (mic → channel) |
| http://localhost:8090/api/channels | Channel list JSON (name, live, mp3, hls) |
| http://localhost:8000/main.mp3 | Icecast MP3 stream (any player / Radio Browser) |
| http://localhost:8000/ | Icecast status |

Push a file instead of the mic: `cd ingest && go run ./cmd/push -ch news some.mp3`

## Config (env / `.env`)

| Var | Default | |
|---|---|---|
| `CHANNELS` | `main,news,music` | comma-separated |
| `BROADCAST_TOKEN` | *(empty = no auth)* | required `token` for broadcasters |
| `SOURCE_PASSWORD` / `ADMIN_PASSWORD` | `changeme-*` | Icecast passwords — **change these** |
| `ICECAST_PUBLIC_URL` | `http://localhost:8000` | URL listeners use for MP3 |
| `HLS_PUBLIC_URL` | *(empty = same host)* | set to CDN base URL |
| `INGEST_PORT` | `8090` | host port of the ingest service |

## Notes
- Mic access needs `https://` (or `localhost`): put a TLS reverse proxy in front for real use.
- CDN: point it at `http://<ingest>/hls/` as origin. Playlists send `max-age=2`, segments `max-age=3600`.
- Scale MP3 with Icecast relays; scale HLS with the CDN.
- Latency: MP3 ~2–5 s, HLS ~10–20 s.
