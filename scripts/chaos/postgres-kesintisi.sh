#!/bin/sh
# scripts/chaos/postgres-kesintisi.sh — PostgreSQL'i durdurup API'nin toparlandigini gosterir.
#   make chaos
# Gereksinim: compose ayakta (make up). Kamera anahtari .env'den okunur, yazdirilmaz.
set -u

KOK=$(CDPATH= cd "$(dirname "$0")/../.." && pwd) || exit 1
cd "$KOK" || exit 1

env_degeri() {
  if [ -f "$KOK/.env" ]; then
    sed -n "s/^[[:space:]]*$1[[:space:]]*=[[:space:]]*//p" "$KOK/.env" \
      | tail -n 1 \
      | tr -d '\r' \
      | sed -e 's/^"//' -e 's/"$//' -e "s/^'//" -e "s/'$//"
  fi
}

CAMERA_API_KEY=${CAMERA_API_KEY:-$(env_degeri CAMERA_API_KEY)}
API_PORT=$(env_degeri API_HOST_PORT)
API_PORT=${API_PORT:-8080}
API_URL=${API_URL:-http://127.0.0.1:$API_PORT}
LOT_ID=${LOT_ID:-1}

GECICI=$(mktemp -d 2>/dev/null || echo "/tmp/parkakil-chaos-$$")
mkdir -p "$GECICI" || exit 1

POSTGRES_DURDURULDU=0
PASS=0
FAIL=0
HEALTH_KOD=000
KAMERA_KOD=000

pass() {
  PASS=$((PASS + 1))
  printf 'PASS  %s\n' "$1"
}

fail() {
  FAIL=$((FAIL + 1))
  printf 'FAIL  %s (%s)\n' "$1" "${2:-}"
}

temizlik() {
  rm -rf "$GECICI"
  if [ "$POSTGRES_DURDURULDU" = "1" ]; then
    echo "temizlik: postgres yeniden baslatiliyor" >&2
    docker compose start postgres >/dev/null 2>&1 || true
  fi
}
trap temizlik EXIT

health_dene() {
  HEALTH_KOD=$(curl -s -o "$GECICI/health.json" -w '%{http_code}' --max-time 5 "$API_URL/health" 2>/dev/null)
  [ -n "$HEALTH_KOD" ] || HEALTH_KOD=000
}

wait_health() {
  hedef=$1
  limit=$2
  baslangic=$(date +%s)
  while :; do
    health_dene
    if [ "$HEALTH_KOD" = "$hedef" ]; then
      if [ "$hedef" != "503" ] || grep -q '"code":"unavailable"' "$GECICI/health.json" 2>/dev/null; then
        return 0
      fi
    fi
    [ $(( $(date +%s) - baslangic )) -lt "$limit" ] || return 1
    sleep 1
  done
}

api_calisiyor() {
  docker compose ps --services --status running 2>/dev/null | grep -qx api
}

kamera_post() {
  olay_id=$1
  govde=$(printf '{"lot_id":%s,"camera_id":"kaos-testi","camera_event_id":"%s","direction":"entry","plate_raw":"34 KZ 001","captured_at":"%s"}' \
    "$LOT_ID" "$olay_id" "$(date -u +%Y-%m-%dT%H:%M:%SZ)")
  KAMERA_KOD=$(curl -s -o "$GECICI/kamera.json" -w '%{http_code}' --max-time 15 \
    -X POST \
    -H 'Content-Type: application/json' \
    -H "X-Camera-Key: $CAMERA_API_KEY" \
    --data "$govde" \
    "$API_URL/camera-events" 2>/dev/null)
  [ -n "$KAMERA_KOD" ] || KAMERA_KOD=000
}

BASLANGIC=$(date +%s)

echo "== kara kutu: postgres kesintisi ($API_URL)"

if [ -z "$CAMERA_API_KEY" ]; then
  echo "HATA: .env icinde CAMERA_API_KEY bulunamadi." >&2
  exit 1
fi

calisanlar=$(docker compose ps --services --status running 2>/dev/null || true)
if ! printf '%s\n' "$calisanlar" | grep -qx api || ! printf '%s\n' "$calisanlar" | grep -qx postgres; then
  echo "HATA: compose ayakta degil (api ve postgres calismali). Once 'make up'." >&2
  exit 1
fi

health_dene
if [ "$HEALTH_KOD" = "200" ]; then
  pass "1 /health baslangicta 200"
else
  fail "1 /health baslangicta 200" "durum=$HEALTH_KOD"
fi

if docker compose stop postgres >/dev/null 2>&1; then
  POSTGRES_DURDURULDU=1
  pass "2 postgres durduruldu"
else
  fail "2 postgres durduruldu" "docker compose stop basarisiz"
fi

if wait_health 503 30; then
  pass "3 /health 503 unavailable (en fazla 30 sn)"
else
  fail "3 /health 503 unavailable (en fazla 30 sn)" "son durum=$HEALTH_KOD"
fi

kamera_post "kaos-kesinti-$(date +%s)-$$"
if [ "$KAMERA_KOD" -ge 500 ] 2>/dev/null && [ "$KAMERA_KOD" -lt 600 ] 2>/dev/null; then
  if api_calisiyor; then
    pass "4 kesinti sirasinda POST /camera-events 5xx ($KAMERA_KOD), api ayakta"
  else
    fail "4 kesinti sirasinda POST /camera-events 5xx, api ayakta" "api calismiyor"
  fi
else
  fail "4 kesinti sirasinda POST /camera-events 5xx" "durum=$KAMERA_KOD"
fi

if docker compose start postgres >/dev/null 2>&1; then
  POSTGRES_DURDURULDU=0
  pass "5 postgres yeniden baslatildi"
else
  fail "5 postgres yeniden baslatildi" "docker compose start basarisiz"
fi

if wait_health 200 60; then
  pass "6 /health 200'e dondu (en fazla 60 sn)"
else
  fail "6 /health 200'e dondu (en fazla 60 sn)" "son durum=$HEALTH_KOD"
fi

kamera_post "kaos-toparlanma-$(date +%s)-$$"
if [ "$KAMERA_KOD" = "201" ]; then
  pass "7 toparlanma sonrasi POST /camera-events 201"
else
  fail "7 toparlanma sonrasi POST /camera-events 201" "durum=$KAMERA_KOD"
fi

BITIS=$(date +%s)
TOPLAM=$((BITIS - BASLANGIC))

echo
echo "Toplam sure: ${TOPLAM} sn"
echo "PASS=$PASS FAIL=$FAIL"

[ "$FAIL" -eq 0 ] || exit 1
exit 0
