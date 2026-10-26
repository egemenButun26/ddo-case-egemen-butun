#!/bin/sh
# scripts/load/run.sh — k6 yuk testini compose aginda calistirir.
#   make load
#   VUS=10 DURATION=30s sh scripts/load/run.sh
# Sonuc: scripts/load/sonuc.json ve scripts/load/SONUC.md (k6 handleSummary yazar).
set -u

KOK=$(CDPATH= cd "$(dirname "$0")/../.." && pwd) || exit 1
KOK_MOUNT=$(cd "$KOK" && { pwd -W 2>/dev/null || pwd; })

env_degeri() {
  if [ -f "$KOK/.env" ]; then
    sed -n "s/^[[:space:]]*$1[[:space:]]*=[[:space:]]*//p" "$KOK/.env" \
      | tail -n 1 \
      | tr -d '\r' \
      | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"
  fi
}

CAMERA_API_KEY=${CAMERA_API_KEY:-$(env_degeri CAMERA_API_KEY)}
API_URL=${API_URL:-http://api:8080}
VUS=${VUS:-20}
DURATION=${DURATION:-60s}
LOT_ID=${LOT_ID:-1}
SEED=${SEED:-42}
NETWORK=${COMPOSE_NETWORK:-parkakil_default}
K6_IMAGE=${K6_IMAGE:-grafana/k6:0.54.0}

if [ -z "$CAMERA_API_KEY" ]; then
  echo "HATA: .env icinde CAMERA_API_KEY bulunamadi." >&2
  exit 1
fi

if ! docker network inspect "$NETWORK" >/dev/null 2>&1; then
  echo "HATA: '$NETWORK' docker agi yok. Once 'make up' calistirin." >&2
  exit 1
fi

rm -f "$KOK/scripts/load/sonuc.json" "$KOK/scripts/load/SONUC.md"

echo "k6 yuk testi: $API_URL (VUS=$VUS, sure=$DURATION, lot=$LOT_ID)"

docker run --rm \
  --network "$NETWORK" \
  -v "$KOK_MOUNT/scripts/load:/scripts/load" \
  -w /scripts/load \
  -e "API_URL=$API_URL" \
  -e "CAMERA_API_KEY=$CAMERA_API_KEY" \
  -e "VUS=$VUS" \
  -e "DURATION=$DURATION" \
  -e "LOT_ID=$LOT_ID" \
  -e "SEED=$SEED" \
  "$K6_IMAGE" run k6-gate.js
kod=$?

if [ -f "$KOK/scripts/load/SONUC.md" ]; then
  echo "sonuc tablosu: scripts/load/SONUC.md"
else
  echo "uyari: scripts/load/SONUC.md olusmadi" >&2
fi
if [ -f "$KOK/scripts/load/sonuc.json" ]; then
  echo "ham sonuc:     scripts/load/sonuc.json"
fi

exit "$kod"
