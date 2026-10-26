# Mimari Karar Kayıtları (ADR)

Her kayıt: **Bağlam → Karar → Sonuçlar (artı/eksi) → Kanıt**. Kayıtlar, kodun bugünkü hâlini anlatır. Bir karar değişirse yeni bir kayıt eklenir.

---

## ADR-01 — Go + chi + pgx + goose; ORM yok

- **Bağlam:** proje.md backend için Go ve PostgreSQL 16'yı zorunlu kılıyor. İş kuralları kilit, kısmi indeks, `EXCLUDE` ve `SKIP LOCKED` gibi PostgreSQL özelliklerine dayanıyor.
- **Karar:**
  - HTTP için `go-chi/chi`, sürücü için `jackc/pgx/v5` (havuz: `pgxpool`), migrasyon için `pressly/goose` (SQL dosyaları gömülü) kullanılıyor.
  - SQL elle yazılıyor; ORM yok.
  - Paket düzeni:
    - saf iş kuralları: `internal/domain/*`;
    - veri erişimi: `internal/store` (transaction açmaz, `Querier` alır);
    - transaction sınırları: `internal/service/*`;
    - HTTP: `internal/httpx`.
- **Sonuçlar:**
  - (+) Kilit ve izolasyon davranışı görünür ve test edilebilir.
  - (+) Domain paketleri veritabanı olmadan birim test ediliyor.
  - (−) SQL ve tip eşlemesi elle bakım istiyor.

## ADR-02 — Aynı araç için tek açık oturum: lot satırı kilidi + READ COMMITTED + kısmi UNIQUE indeksler

- **Bağlam:** Kameralar aynı aracı aynı anda birden çok kez okuyabilir. Ayrıca proje.md'nin şartı: "Aynı araç için çift giriş oturumu oluşmamalı".
- **Karar:**
  - Her kamera olayı tek bir transaction'da işleniyor. Transaction önce otopark satırını `SELECT … FOR NO KEY UPDATE` ile kilitliyor.
  - İzolasyon READ COMMITTED: kilidi bekleyen transaction, kilit açıldıktan sonra her sorguda yeni bir snapshot alır ve öncekinin açtığı oturumu görür.
  - Son savunma olarak iki kısmi UNIQUE indeks var:
    - `(lot_id, plate) WHERE status='open'`;
    - OCR karışıklık sınıfı anahtarıyla aynı indeks (U/V, O/0, B/8…).
- **Neden `FOR UPDATE` değil:** `FOR NO KEY UPDATE`, FK kontrollerinin aldığı `FOR KEY SHARE` kilidiyle çakışmıyor.
- **Neden REPEATABLE READ / SERIALIZABLE değil:** REPEATABLE READ'de snapshot kilit sorgusunda alınır; bekleyen transaction açılan oturumu göremezdi. SERIALIZABLE ise yeniden deneme döngüsü isterdi.
- **Sonuçlar:**
  - (+) Basit ve deterministik.
  - (−) Bir otoparkın olayları sırayla işleniyor. Ölçüm: kapı kararı p95 birkaç ms (`faz15`).
- **Kanıt:** `TestEsZamanli50GirisAyniPlaka` ve `TestEsZamanli50GirisKarisikOkuma` (50 eşzamanlı istek → 1 oturum). Kilit kaldırılınca bu testler kırılıyor (mutasyonla doğrulandı).

## ADR-03 — Bulanık plaka eşleştirme: ağırlıklı Levenshtein + OCR karışıklık tablosu + inceleme kuyruğu

- **Bağlam:** OCR O/0, B/8 gibi karakterleri karıştırıyor, karakter atlıyor ya da boş okuyor.
- **Karar:**
  - Plaka normalize ediliyor. Ardından ağırlıklı düzenleme mesafesi hesaplanıyor; karışıklık çiftleri tam değişimden daha ucuz sayılıyor.
  - Güven puanı 0–1000 arası. Eşikler: `Auto ≥ 900`, `Review ≥ 700`, iki aday arasında en az 100 puan fark.
  - Güven düşükse ya da sonuç belirsizse oturum açılmıyor/kapatılmıyor; olay `review_items` kuyruğuna gidiyor.
- **Sonuçlar:**
  - (+) Yanlış eşleşme yerine insan kararı.
  - (−) Karışıklık anahtarıyla aynı görünen iki gerçek farklı araç aynı anda içeride olamaz; ikincisi incelemeye düşer.
- **Kanıt:** `docs/T1-plaka.md`, `internal/domain/plate`.

## ADR-04 — Para tam sayı kuruş; tarife sürümü girişte dondurulur

- **Bağlam:** proje.md'de float yasak. Tarifeler zamanla değişiyor, ama giriş anındaki sürüm oturuma dondurulmalı.
- **Karar:**
  - Para: Go'da `tariff.Kurus` (`int64`), veritabanında `bigint`. Bütün aritmetik taşma denetimli.
  - Tarife sürümleri `tariff_versions` tablosunda; aynı otoparkta geçerlilik aralıkları `EXCLUDE USING gist` ile çakışamaz.
  - `sessions.tariff_version_id` giriş anında yazılıyor; çıkıştaki hesap bu sürümle yapılıyor. Gece yarısını aşan oturumlar yerel takvim günlerine bölünüyor.
- **Kanıt:** `docs/T2-tarife.md`, `TestBeklenenDegerler`, `TestTarifeDondurma`.

## ADR-05 — İdempotency: kamera olay kimliği, `Idempotency-Key` + istek özeti, sağlayıcı referansı

- **Bağlam:** Ağ kopuyor; kamera, kiosk ve sağlayıcı aynı isteği tekrar gönderiyor.
- **Karar:**
  - **Kamera:** `(camera_id, camera_event_id)` UNIQUE. Aynı içerik tekrar gelirse ilk sonuç döner (200). Farklı içerikle gelirse 409.
  - **Kamera zamanı:** `CapturedAt` mikrosaniyeye kesiliyor; PostgreSQL `timestamptz` hassasiyetiyle aynı.
  - **Ödeme ve iade:** `Idempotency-Key` başlığı zorunlu; anahtar UNIQUE. İstek gövdesinin SHA-256 özeti saklanıyor. Aynı anahtar + aynı özet → aynı sonuç; farklı özet → 409.
  - **Sağlayıcı bildirimi:** `provider_ref` UNIQUE. Yinelenen bildirim durumu değiştirmiyor.
  - **Dönem sonu faturalama:** `invoices UNIQUE (subscription_id, period_start, kind)` + `billing_runs` ile veritabanı düzeyinde idempotent.
- **Kanıt:**
  - `TestOlayCakismasiFarkliIcerik`, `TestTekrarOlayNanosaniye`;
  - `faz15` ve `faz18` eşzamanlı idempotency testleri.

## ADR-06 — Çıkış izni sağlayıcının onay anından 15 dk; geç onay ve iade

- **Bağlam:** Kiosk sağlayıcısında zaman aşımı, çift bildirim ve geç onay var.
- **Karar:**
  - Ödemenin durum makinesi saf domain kodunda: `pending → succeeded | failed | timed_out`; geç onay da `timed_out → succeeded` geçişi.
  - Çıkış izni `[PaidAt, PaidAt+15dk)`. `PaidAt` sağlayıcının onay anı; bildirimin alınma anı `ConfirmedAt` olarak ayrıca tutuluyor.
  - Geç onay borcu iki kez kapatırsa fazla ödeme hesaplanıyor ve iade gerektiği bildiriliyor.
  - Sağlayıcı bildirimi HMAC-SHA256 ile imzalı.
- **Sonuçlar:**
  - (+) Kurallar saf kodda; durum tablosu ve sınırlar testle sabit.
  - (−) Onaydan 15 dk sonra gelen bildirimde izin zaten dolmuş olur. Bu bilinçli bir karar.
- **Kanıt:** `internal/domain/payment` (coverage %100), `faz11`, `faz15`.

## ADR-07 — Sunucu taraflı oturum (JWT yerine) ve bcrypt

- **Bağlam:** proje.md "JWT (access + refresh) ya da sunucu taraflı oturum" diyor. Her uçta yetki kontrolü ve iptal edilebilir oturum gerekiyor.
- **Karar:**
  - Token: 32 bayt `crypto/rand` değer; istemciye base64url olarak veriliyor. Veritabanında yalnız SHA-256 özeti saklanıyor (`auth_sessions`).
  - Sürelidir; çıkışta iptal ediliyor.
  - Parola: bcrypt.
  - Rol tabanlı yetki: `requireAuth` 401, `requireRole` 403.
  - Saha cihazları (kamera, kapı, kiosk) kullanıcı oturumu değil, `X-Camera-Key` cihaz anahtarı kullanıyor. Kontrol: SHA-256 + sabit zamanlı karşılaştırma; anahtar tanımlı değilse istek reddediliyor.
- **Sonuçlar:**
  - (+) Anında iptal edilebiliyor; kütüphane bağımlılığı yalnız `golang.org/x/crypto`.
  - (−) Her istekte bir veritabanı sorgusu.

## ADR-08 — Yalnız ekleme yapılabilen denetim kaydı veritabanında zorlanır

- **Bağlam:** Kritik işlemler için kim, ne zaman, neyi değiştirdi ve eski/yeni değer kaydedilmeli.
- **Karar:**
  - Kayıtlar `audit_log` tablosuna yazılıyor. `BEFORE UPDATE OR DELETE` trigger'ı istisna fırlatıyor; yani kayıtlar uygulama kodundan bağımsız olarak değiştirilemiyor.
  - Sayaç kayması kayıtları (`counter_drifts`) da aynı yöntemle korunuyor.
- **Kanıt:** `TestAuditAppendOnly`, `TestCounterDriftsAppendOnly`.

## ADR-09 — Doluluk: artımlı sayaç + olaylardan yeniden hesaplama + kayma düzeltmesi

- **Bağlam:** Doluluk sayacı olaylardan yeniden hesaplanabilmeli; sayaç kayarsa tespit edilip düzeltilmeli.
- **Karar:**
  - `lot_counters` giriş ve çıkışta aynı transaction'da güncelleniyor (artımlı sayaç).
  - Yeniden hesaplama `plate_events` üzerinden yapılıyor: açılış olayı sayısı − kapanış olayı sayısı.
  - `DetectAndFixDrift` farkı `counter_drifts` tablosuna yazıp sayacı düzeltiyor.
  - Abone rezervasyonları kapasiteden düşülüyor; 30 dakika dolunca `SKIP LOCKED` ile serbest bırakılıyor.
- **Kanıt:** `faz16` (eşzamanlı rezervasyon, kayma testi).

## ADR-10 — Testler gerçek PostgreSQL ile, Docker'da; "yeşil test kanıt değil"

- **Bağlam:** Kilit, kısıt ve migration davranışı sahte veritabanıyla ölçülemez.
- **Karar:**
  - Veritabanı testleri geçici bir `postgres:16-alpine` konteynerinde çalışıyor. `PARKAKIL_REQUIRE_DB=1` iken veritabanı yoksa testler atlanmıyor, düşüyor.
  - Her iş bir `test/fazN.js` betiğiyle doğrulanıyor.
  - Kritik düzeltmelerde **mutasyon kontrolü** yapılıyor: düzeltme geçici olarak geri alınıp testin kırıldığı gösteriliyor.
- **Sonuçlar:**
  - (+) Yarış durumları ve kısıtlar gerçek motorda kanıtlı.
  - (−) Testler daha yavaş; aynı veritabanında tekrar çalıştırmaya uygun değil, her betik temiz bir konteyner açıyor.

## ADR-11 — Frontend: React 18 + Vite + TypeScript, UI kütüphanesi yok

- **Bağlam:** Beş rol, her ekranda yükleniyor / boş / hata durumları ve mobil uyum isteniyor.
- **Karar:**
  - React Router ile role göre yönlendirme. `fetch` sarmalayıcısında 10 sn zaman aşımı var ve standart hata gövdesi ayrıştırılıyor.
  - Stil düz CSS. Build çok aşamalı Docker imajında yapılıyor (`node` → `nginx-unprivileged`).
- **Sonuçlar:**
  - (+) Bağımlılık az; imaj küçük ve root olmayan kullanıcıyla çalışıyor.
  - (−) Hazır bileşen kütüphanesi olmadığı için ekranlar sade.

## ADR-12 — Tohumlu simülatörler ayrı compose servisleri

- **Bağlam:** proje.md günde ~20.000 hatalı/tekrarlı plaka olayı ve sağlayıcı gecikmeleri istiyor; aynı tohum aynı veriyi üretmeli.
- **Karar:**
  - Kamera simülatörü (`cmd/simulator`) saf bir üreticiden (`internal/simulator`) beslenen ayrı bir servis.
  - `math/rand/v2` PCG kullanıyor; tohum ve simülasyonun başlangıç zamanı sabit, şimdiki zaman kullanılmıyor.
  - `simulator dump`, aynı tohumla aynı çıktıyı bayt bayt üretiyor.
- **Kanıt:** `faz17`.
