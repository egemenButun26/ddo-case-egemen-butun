// scripts/load/k6-gate.js — ParkAkıl k6 yuk testi.
//   Senaryolar: health (GET /health), camera_events (POST /camera-events).
//   Plakalar tohumlu (SEED) uretilir; camera_event_id her istekte benzersizdir.
//   Kapı kararı ucu paralel dalda yazılıyor; geldiğinde buraya yeni bir senaryo
//   olarak eklenecek (gate_decision: POST /gate-decisions, X-Camera-Key ile).
//
//   Ortam degiskenleri:
//     API_URL          (varsayilan http://api:8080)
//     CAMERA_API_KEY   (zorunlu; .env'den run.sh okur)
//     VUS              (varsayilan 20)
//     DURATION         (varsayilan 60s)
//     LOT_ID           (varsayilan 1)
//     SEED             (varsayilan 42)
import http from 'k6/http';
import { check } from 'k6';

const API_URL = (__ENV.API_URL || 'http://api:8080').replace(/\/+$/, '');
const CAMERA_KEY = __ENV.CAMERA_API_KEY || '';
const VUS = Number(__ENV.VUS || '20');
const DURATION = __ENV.DURATION || '60s';
const LOT_ID = Number(__ENV.LOT_ID || '1');
const SEED = Number(__ENV.SEED || '42');

export const options = {
  scenarios: {
    health: {
      executor: 'constant-vus',
      vus: VUS,
      duration: DURATION,
      exec: 'healthScenario',
      tags: { scenario: 'health' },
    },
    camera_events: {
      executor: 'constant-vus',
      vus: VUS,
      duration: DURATION,
      exec: 'cameraEventsScenario',
      tags: { scenario: 'camera_events' },
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    'http_req_duration{scenario:health}': ['p(95)<200'],
    'http_req_duration{scenario:camera_events}': ['p(95)<200'],
  },
  summaryTrendStats: ['avg', 'p(50)', 'p(95)', 'p(99)', 'max'],
};

function lcg(tohum) {
  let durum = Math.floor(Math.abs(tohum)) % 4294967296;
  return function () {
    durum = (1664525 * durum + 1013904223) % 4294967296;
    return durum / 4294967296;
  };
}

function sifirla(sayi, genislik) {
  let metin = String(sayi);
  while (metin.length < genislik) metin = '0' + metin;
  return metin;
}

const HARFLER = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ';
let sayac = 0;

function plakaUret(rnd) {
  const il = sifirla(1 + Math.floor(rnd() * 81), 2);
  const harf1 = HARFLER[Math.floor(rnd() * HARFLER.length)];
  const harf2 = HARFLER[Math.floor(rnd() * HARFLER.length)];
  const rakam = sifirla(Math.floor(rnd() * 10000), 4);
  return `${il}${harf1}${harf2}${rakam}`;
}

function kameraEventID() {
  sayac += 1;
  return `k6-${Date.now()}-${__VU}-${__ITER}-${sayac}`;
}

export function healthScenario() {
  const res = http.get(`${API_URL}/health`, { tags: { scenario: 'health' } });
  check(res, {
    'health 200': (r) => r.status === 200,
  });
}

export function cameraEventsScenario() {
  const rnd = lcg(SEED + __VU * 7919 + __ITER);
  const govde = {
    lot_id: LOT_ID,
    camera_id: `k6-kamera-${__VU}`,
    camera_event_id: kameraEventID(),
    direction: rnd() < 0.5 ? 'entry' : 'exit',
    plate_raw: plakaUret(rnd),
    captured_at: new Date().toISOString(),
  };
  const res = http.post(`${API_URL}/camera-events`, JSON.stringify(govde), {
    headers: {
      'Content-Type': 'application/json',
      'X-Camera-Key': CAMERA_KEY,
    },
    tags: { scenario: 'camera_events' },
  });
  check(res, {
    'kamera olayi 2xx': (r) => r.status >= 200 && r.status < 300,
  });
}

function ms(deger) {
  return deger === undefined || deger === null ? 'n/a' : Number(deger).toFixed(2);
}

function yuzde(deger) {
  return deger === undefined || deger === null ? 'n/a' : (Number(deger) * 100).toFixed(2) + '%';
}

function markdownTablo(data) {
  const sure = (data.metrics.http_req_duration && data.metrics.http_req_duration.values) || {};
  const hata = data.metrics.http_req_failed && data.metrics.http_req_failed.values;
  return [
    '| Ölçüm | Değer |',
    '|---|---|',
    `| Toplam istek | ${data.metrics.iterations ? data.metrics.iterations.values.count : 'n/a'} |`,
    `| p50 | ${ms(sure['p(50)'])} ms |`,
    `| p95 | ${ms(sure['p(95)'])} ms |`,
    `| p99 | ${ms(sure['p(99)'])} ms |`,
    `| Hata oranı | ${yuzde(hata && hata.rate)} |`,
    `| Eşikler | http_req_failed < 1%, p95 < 200 ms |`,
    '',
  ].join('\n');
}

export function handleSummary(data) {
  const tablo = markdownTablo(data);
  return {
    'sonuc.json': JSON.stringify(data, null, 2),
    'SONUC.md': tablo,
    stdout: '\n' + tablo,
  };
}
