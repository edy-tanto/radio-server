#!/usr/bin/env bash
# Radio POC launcher.  usage: ./run.sh [up|down|restart|logs|status]
set -euo pipefail
cd "$(dirname "$0")"

cmd=${1:-up}

need() { command -v "$1" >/dev/null || { echo "error: $1 is not installed" >&2; exit 1; }; }
need docker
docker compose version >/dev/null 2>&1 || { echo "error: docker compose plugin missing" >&2; exit 1; }

# first run: create .env with random passwords (kept across runs)
if [[ ! -f .env ]]; then
  rnd() { head -c 32 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 20; }
  cat > .env <<EOF
CHANNELS=main,news,music
SOURCE_PASSWORD=$(rnd)
ADMIN_PASSWORD=$(rnd)
BROADCAST_TOKEN=$(rnd)
INGEST_PORT=8090
ICECAST_PUBLIC_URL=http://localhost:8000
HLS_PUBLIC_URL=
EOF
  chmod 600 .env
  echo "created .env with random passwords"
fi
set -a; source .env; set +a
port=${INGEST_PORT:-8090}

status() {
  echo
  echo "Listener page    http://localhost:$port/"
  echo "Broadcaster page http://localhost:$port/broadcast   (token: ${BROADCAST_TOKEN:-none})"
  echo "Channel API      http://localhost:$port/api/channels"
  echo "Icecast admin    http://localhost:8000/admin/   (admin / $ADMIN_PASSWORD)"
  echo "Streams:"
  for ch in ${CHANNELS//,/ }; do
    echo "  $ch   MP3 $ICECAST_PUBLIC_URL/$ch.mp3   HLS http://localhost:$port/hls/$ch/index.m3u8"
  done
}

case $cmd in
  up)
    docker compose up -d --build
    printf "waiting for streams"
    for _ in $(seq 30); do
      if curl -fs "http://localhost:$port/hls/${CHANNELS%%,*}/index.m3u8" >/dev/null 2>&1; then
        echo " ready"; status; exit 0
      fi
      printf "."; sleep 1
    done
    echo " timed out — check: ./run.sh logs" >&2; exit 1 ;;
  down)    docker compose down ;;
  restart) docker compose restart ;;
  logs)    docker compose logs -f --tail 50 ;;
  status)  docker compose ps; status ;;
  *) echo "usage: $0 [up|down|restart|logs|status]" >&2; exit 1 ;;
esac
