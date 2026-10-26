# ParkAkıl — Otopark Plaka Tanıma, Abonelik ve Tahsilat Sistemi

**Aday:** Egemen Bütün · **Depo:** https://github.com/egemenButun26/ddo-case-egemen-butun

ParkAkıl; kameralardan gelen hatalı/tekrarlı plaka okumalarını eşleştiren, giriş-çıkış
oturumlarını yöneten, zaman geçerlilikli tarifeyle ücret hesaplayan, abonelik ve
rezervasyonları yürüten, kiosk ödemelerini idempotent biçimde tahsil eden ve günlük
mutabakat üreten bir otopark sistemidir. Backend Go, veritabanı PostgreSQL 16,
arayüz React + TypeScript'tir. Üç katman ve yardımcı servisler tek komutla Docker
Compose ile ayağa kalkar.

## Kurulum

Gereksinimler: Docker + Docker Compose. Go ve Node kurmak gerekmez; derleme
konteyner içinde yapılır.

```sh
git clone https://github.com/egemenButun26/ddo-case-egemen-butun.git
cd ddo-case-egemen-butun
cp .env.example .env
docker compose up --build --wait
```

Windows PowerShell'de kopyalamak için: `Copy-Item .env.example .env`

| Ne | Adres |
|---|---|
| Web arayüzü | http://localhost:3000 |
| Go API | http://localhost:8080 |
| Sağlık | http://localhost:8080/health |
| Hazır olma | http://localhost:8080/_hz/45a6 |
| Prometheus | http://localhost:8080/metrics |
| OpenAPI 3.1 | http://localhost:8080/openapi.yaml |

Servisler: `postgres` (host'a port açmaz), `migrate` (goose), `seed` (demo verisi),
`api`, `web`, `simulator` (kamera olayları + kiosk ödemeleri) ve `providersim`
(ödeme sağlayıcı gecikmeleri/zaman aşımları/geç onayları). Tüm servislerde
healthcheck ve `depends_on` koşulları vardır. İmajlar çok aşamalı ve root olmayan
kullanıcıyla çalışır.

## Demo hesapları

Parola `.env` içindeki `SEED_DEMO_PASSWORD` değeridir; `.env.example` ile kurulumda
`demo-parola-degistir` olur.

| Rol | E-posta | Not |
|---|---|---|
| Sistem yöneticisi | `seed.ardic@ddo-case.invalid` | İlk seed kullanıcısı |
| Sürücü | `surucu@parkakil.test` | Plaka `34ABC123`, KADIKOY aboneliği |
| Otopark görevlisi | `gorevli@parkakil.test` | İnceleme kuyruğu, doluluk, borç sorgu |
| Muhasebe | `muhasebe@parkakil.test` | Ödeme, fatura, iade, mutabakat |
| İşletme müdürü | `mudur@parkakil.test` | Rapor, kara liste, rezervasyon |

Ayrıntılı deneme adımları ve elle senaryolar: [`TESTING.md`](TESTING.md).
Hazır HTTP istekleri: [`requests.http`](requests.http).

## Mimari özet

```
kamera / kiosk / kapı          sürücü, görevli, muhasebe, müdür, yönetici
        │                                      │
        │ X-Camera-Key                         │ Bearer oturum (sunucu taraflı)
        ▼                                      ▼
   ┌──────────────────────── Go API (chi) ────────────────────────┐
   │ httpx: auth, kamera, ops, ödeme, faturalama, metrik, OpenAPI  │
   │ service: sessionsvc, occupancysvc, paymentsvc, billingsvc     │
   │ domain: plate, session, tariff, payment, occupancy, billing   │
   │ store: pgx/v5, elle SQL, transaction + SELECT ... FOR UPDATE  │
   └──────────────────────────────┬───────────────────────────────┘
                                  ▼
                        PostgreSQL 16 (ya- önekli tablolar)
```

- **Backend:** Go 1.23, `chi` yönlendirici, `pgx/v5`, `goose` migrasyonları. ORM yok;
  kilit, kısmi indeks, `EXCLUDE` ve `SKIP LOCKED` doğrudan SQL'de.
- **Veritabanı:** 22 uygulama tablosu; tümü `ya-` önekiyle, kullanıcı tablosu tam
  olarak `ya-qo-users`. Para her yerde tam sayı kuruş (`bigint`). Yabancı anahtar,
  `UNIQUE`, `CHECK`, kısmi indeksler ve append-only denetim tetikleyicileri var.
- **Kimlik:** bcrypt parola özeti, 32 baytlık rastgele sunucu taraflı oturum token'ı
  (veritabanında yalnız SHA-256 özeti). `requireAuth` 401, `requireRole` 403.
  Kamera/kiosk/kapı cihazları `X-Camera-Key` kullanır.
- **Dayanıklılık:** `context` zaman aşımları, graceful shutdown, JSON log (parola/token
  loglanmaz), tüm yapılandırma ortam değişkenlerinden.
- **Gözlemlenebilirlik:** Prometheus `/metrics`, istek kimliği, yapılandırılmış log.
- **Yardımcı servisler:** `simulator` tohumlu plaka olayları üretir ve açık oturumlar
  için kiosk ödemesi tetikler; `providersim` gecikme, zaman aşımı, çift bildirim ve
  geç onay üretir.
- **Karar kayıtları:** 12 ADR, [`DECISIONS.md`](DECISIONS.md) içinde.

### Frontend seçimi ve gerekçesi

React 18 + Vite + TypeScript + React Router kullanıldı; hazır UI kütüphanesi yok,
stil düz CSS. Gerekçe: beş rol için ayrı ekranlar ve sunucu taraflı RBAC ile birebir
eşleşen istemci yönlendirmesi gerekiyordu; küçük bağımlılık kümesi imajı küçük tutuyor
ve root olmayan nginx imajında çalışıyor. Her ekranda yükleniyor/boş/hata durumu,
`fetch` sarmalayıcısında 10 sn zaman aşımı ve mobil genişlikte kullanılabilir düzen var.
Testler Vitest + Testing Library ile yazıldı.

## Çalıştırma ve doğrulama

```sh
make verify        # F-1..F-7, T-1..T-5, health ve auth için PASS/FAIL tablosu
make reset-demo    # verileri ilk seed haline döndürür
make load          # k6 yük testi (ayrı; 15 dk verify sınırına girmez)
make chaos         # PostgreSQL'i durdurup toparlanmayı gösterir
node test/faz3.js  # T-1 plaka eşleştirme kanıtı
```

`make` yoksa: `node scripts/verify/verify.js`, `docker compose down -v --remove-orphans
&& docker compose up -d --build --wait`, `sh scripts/load/run.sh`,
`sh scripts/chaos/postgres-kesintisi.sh`.

`make verify` çalışan ve seed edilmiş sistemi varsayar; FAIL varsa sıfırdan farklı
çıkış kodu döner. Ölçülen son koşu: **14/14 PASS** (F-1..F-7, T-1..T-5, HEALTH,
AUTH). Demo verisini değiştirdiği için tekrar temiz doğrulamada `make reset-demo`
kullanılmalıdır.

## Testler, CI ve kanıtlar

- 369 Go test fonksiyonu (116 alt test) + 53 Vitest testi.
- Domain kapsamı eşikleri test betiklerinde denetlenir; CI kapsam raporunu
  `$GITHUB_STEP_SUMMARY` içine basar.
- GitHub Actions: `gofmt`, `go vet`, `golangci-lint`, `go test -race`, frontend
  `npm ci` + `typecheck` + `vitest` + `vite build`, API ve web imaj derleme,
  compose smoke.
- Her iş `test/fazN.js` ile tek başına çalışır. T-1: `faz3`/`faz6`, T-2: `faz4`,
  T-3: `faz16`, T-4: `faz11`/`faz15`/`faz20`, T-5: `faz18`/`faz22`,
  arayüz: `faz23`, gözlemlenebilirlik: `faz19`, OpenAPI: `faz27`,
  geliştirici notları: `faz26`.

### k6 yük testi sonuçları

`make load` (20 VU, 60 sn, health + camera_events) sonucu:

| Ölçüm | Değer |
|---|---|
| Toplam istek | 43.744 |
| p50 | 49,20 ms |
| p95 | 78,56 ms |
| p99 | 95,36 ms |
| Hata oranı | %0,00 |
| Eşikler | `http_req_failed < %1`, p95 < 200 ms |

Kapı kararı ayrı ölçülür: `TestDecideGateP95` ve `make verify` T-4 adımı 100 istekte
p95 < 50 ms eşiğini denetler. Kapı kararı k6 senaryosuna dahil değildir; k6 tablosu
genel API yükünü gösterir.

## Veritabanı indeksleri ve `EXPLAIN (ANALYZE)` ölçümleri

Ölçüm, demo veritabanına 60.000 giriş + 30.000 çıkış + 30.000 yetim çıkış olayı ve
30.000 kapalı oturum eklenerek alındı (137.918 `ya-plate_events`, 31.810
`ya-sessions`). İki kritik indeks:

```sql
CREATE INDEX sessions_closed_recent
  ON "ya-sessions" (lot_id, exited_at) WHERE status = 'closed';

CREATE INDEX plate_events_orphan_exits
  ON "ya-plate_events" (lot_id, captured_at) WHERE outcome = 'orphan_exit';
```

| Sorgu | İndeksli | İndekssiz (`enable_indexscan/bitmapscan=off`) | Fark |
|---|---|---|---|
| Son 20 kapalı oturum: `WHERE lot_id=1 AND status='closed' ORDER BY exited_at DESC LIMIT 20` | 0,184 ms (Index Scan Backward, 23 buffer) | 11,686 ms (Seq Scan + top-N Sort, 693 buffer) | ~63× |
| Son 20 yetim çıkış: `WHERE lot_id=1 AND outcome='orphan_exit' ORDER BY captured_at DESC LIMIT 20` | 0,052 ms (Index Scan Backward, 6 buffer) | 20,921 ms (Parallel Seq Scan + Sort, 2.429 buffer) | ~402× |

Her iki sorgu da `LIMIT 20` ile kullanılan gerçek uçların sorgularıdır; indeksler
doluluk/olay listelerinde sıralamayı ve filtreyi tek taramada karşılar.

## OpenAPI, metrik ve kaos

- OpenAPI 3.1: `docs/openapi.yaml` ve `internal/httpx/openapi.yaml` birebir aynı;
  API `GET /openapi.yaml` ile servis eder. Test: `node test/faz27.js`.
- Prometheus: `GET /metrics`; `METRICS_TOKEN` doluysa Bearer token zorunludur.
- Kaos: `make chaos` PostgreSQL'i durdurur; `/health` 503 döner, API süreçten
  düşmez; PostgreSQL geri gelince sistem toparlanır.

## Bilinen sınırlar

- Aynı otoparkta yalnızca OCR karışıklık sınıfıyla ayrılan iki gerçek araç (ör.
  `O`/`0`) aynı anda açık olamaz; ikinci okuma insan inceleme kuyruğuna düşer.
- Simülatörün plaka olayları tohumla bayt bayt deterministiktir; kiosk ödemeleri
  çalışan sistemin açık oturumlarından türetildiği için zamanlamaya bağlıdır.
- 30 dakikalık rezervasyon serbest bırakma süresi `make verify` içinde beklenmez;
  bu davranış `node test/faz16.js` ile kanıtlanır.
- `make verify` demo verisini değiştirir; tekrar için `make reset-demo`.

## Doğrulama Soruları

### 1. En zorlandığın görev hangisiydi? Hangi alternatif çözümleri düşündün ve neden eledin?

En zor görev T-1, hatalı plaka okumalarıyla oturum eşleştirmeydi. Alternatifler:
tam plaka eşleşmesi (OCR hatalarında oturum kapatmıyor), yalnızca Levenshtein
(OCR karışıklığını ve eksik karakteri aynı ağırlıkta cezalandırıyor), trigram/Jaro
benzerliği (Türk plaka yapısını ve karışıklık tablosunu bilmiyor) ve OCR modeli
(kapsam dışı, açıklanabilirlik ve gecikme maliyeti). Ağırlıklı Levenshtein +
OCR karışıklık tablosu + güven eşiği + insan inceleme kuyruğunu seçtim. Ölçüt:
`Auto ≥ 900`, `Review ≥ 700`, en iyi ve ikinci aday arasında en az 100 puan fark.
Karar kaydı: ADR-03.

### 2. Çözümünün kırıldığı ya da yetersiz kaldığı bir senaryo bul ve nedenini açıkla.

Aynı otoparkta, plakaları yalnızca OCR karışıklık sınıfıyla ayrılan iki gerçek araç
(örneğin `34O...` ve `340...`) aynı anda içeride olamaz: karışıklık anahtarıyla aynı
görünen plakalar için kısmi `UNIQUE` indeks ikinci girişi engeller ve olay inceleme
kuyruğuna düşer. Bu bilinçli bir güvenlik tercihidir; yanlış aracı otomatik eşleştirmek
yerine insan kararına bırakır. Diğer sınır: simülatörün kiosk ödemeleri açık oturumlara
bağlı olduğu için plaka olayları kadar bayt-deterministik değildir.

### 3. Veritabanına eklediğin dizinlerden ikisini seç: hangi sorgu için, dizinsiz/dizinli ölçüm farkı nedir?

- `sessions_closed_recent` — son 20 kapalı oturum sorgusu. İndeksli 0,184 ms,
  indekssiz 11,686 ms (yaklaşık 63 kat).
- `plate_events_orphan_exits` — son 20 yetim çıkış sorgusu. İndeksli 0,052 ms,
  indekssiz 20,921 ms (yaklaşık 402 kat).

Ölçüm verisi ve `EXPLAIN (ANALYZE, BUFFERS)` ayrıntıları yukarıdaki tabloda.

### 4. Yapay zekâ kullandıysan, reddettiğin ya da düzelttiğin bir çıktıyı (önce/sonra) göster ve neden yanlış olduğunu açıkla.

İlk üretimde yapay zekâ, `Idempotency-Key` desteğini yalnızca ödeme ve iade uçlarına
eklemişti. Rezervasyon `POST`'u anahtarı yok sayıyordu; aynı anahtarla gelen tekrar
istek yeni rezervasyon açmaya çalışıyor veya `reservation_exists` dönüyordu. Bu,
proje.md'nin “rezervasyon gibi kritik POST'lar aynı anahtarla aynı sonucu döndürmeli”
şartına aykırıydı. Düzeltme: `ya-reservations` tablosuna `idempotency_key` +
`request_hash`, `UNIQUE` kısıtı ve kanonik istek özeti eklendi; aynı anahtar ve
aynı gövde 200 + aynı kayıt, farklı gövde 409 `idempotency_conflict` döndürüyor.
Kanıt: `TestOpsReservationIdempotency`, `TestRezervasyonIdempotency`.

### 5. “Hatalı plaka okumalarıyla oturum eşleştirme” görevindeki çözümünün doğru çalıştığını nasıl kanıtladın? Komutu ve çıktısını yaz.

```sh
$ node test/faz3.js
  ok  1 go test plaka paketi (Docker) geciyor
  ok  2 toplam kapsam >= %85
     importlar: errors sort strings unicode
  ok  3 import listesi temiz (DB/HTTP/os/time/rand yok)

faz3: 3 kontrol gecti (kapsam %96.3)
```

Ek olarak `node test/faz6.js` gerçek PostgreSQL üzerinde eşzamanlı 50 girişte tek
oturum oluştuğunu, `make verify` ise U/V OCR karışıklığının aynı oturuma bağlandığını
ve geçersiz okumanın inceleme kuyruğuna düştüğünü uçtan uca gösterir.

DDO Case Kodu: ARDIC-45a6ab
