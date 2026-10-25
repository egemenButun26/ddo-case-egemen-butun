# F2 — Giriş/Çıkış Oturumu Karar Paketi

Paket: `internal/domain/session`. Saf Go paketi; veritabanı, HTTP, dosya, ağ, saat ve
rastgelelik kullanmaz. Kamera olayını ve mevcut oturumları alır, ne yapılacağına karar verir.
Veritabanı ve servis katmanı sonraki parçada.

## Olay akışı

1. Kamera `Event` üretir: `LotID`, `CameraID`, `CameraEventID`, `Direction`, `PlateRaw`, `CapturedAt`.
2. `Event.Validate` alanları denetler (plaka içeriği burada denetlenmez).
3. Yön `entry` ise `DecideEntry`, `exit` ise `DecideExit` çağrılır.
4. Karar `Decision` olarak döner: `opened`, `duplicate_entry`, `closed`, `duplicate_exit`,
   `orphan_exit` ya da `review`.
5. `review` kararında `Review` nedeni, güveni ve önerilen/ikinci oturum kimlikleri taşınır.

## Giriş karar tablosu

| Koşul | Karar |
|---|---|
| Boş plaka okuması | `review` / `empty_read` |
| Normalize plaka TR biçiminde değil | `review` / `invalid_plate` |
| Açık oturum yok | `opened` |
| Açık oturumlarda tam ya da otomatik eşleşme | `duplicate_entry` (en iyi oturum) |
| Açık oturumlarda inceleme düzeyinde eşleşme | `review` / `possible_duplicate_entry` |
| Açık oturumlarda eşleşme yok | `opened` |

## Çıkış karar tablosu

| Koşul | Karar |
|---|---|
| Boş plaka okuması | `review` / `empty_read` |
| Açık oturumda tam/otomatik eşleşme, giriş çıkıştan önce | `closed` |
| Açık oturumda tam/otomatik eşleşme, giriş çıkıştan sonra | `review` / `exit_before_entry` |
| Açık oturumda inceleme düzeyinde eşleşme | `review` / `plate.Match` nedeni |
| Son `DuplicateWindow` içinde kapanmış oturumda tam/otomatik eşleşme | `duplicate_exit` |
| Eşleşme yok, plaka TR biçiminde değil | `review` / `invalid_plate` |
| Eşleşme yok, plaka geçerli | `orphan_exit` |

## Tekrarlı ve sırasız olay kuralları

- Açık oturum varken aynı aracın yeniden girişi yeni oturum açmaz; `duplicate_entry` olur.
- Çıkıştan hemen sonra gelen aynı plaka çıkışı `duplicate_exit` sayılır. Pencere varsayılan 5 dakikadır
  ve sınır dahildir (tam 5 dk tekrar, 5 dk 1 sn bekleyen çıkış).
- Girişi görülmemiş çıkış `orphan_exit` olarak saklanır; giriş olayı sonradan gelirse
  `MatchOrphanExit` en erken uygun bekleyen çıkışı eşleştirir.
- Bekleyen çıkış seçimi giriş zamanından önce olanları atlar; eşitlikte küçük `EventID` kazanır.

## İnceleme nedenleri

`empty_read`, `invalid_plate`, `possible_duplicate_entry`, `exit_before_entry`,
`low_confidence_review`, `ambiguous`, `invalid_format`, `ambiguous_exact`, `no_tariff`.

`no_tariff` nedenini servis katmanı koyar; paket sabiti hazırdır.

## Varsayımlar

- Açık oturum varken aynı aracın yeniden girişi tekrar okumadır.
- Girişte inceleme düzeyinde benzerlik oturum açmaz; görevli karar verir.
- Geçersiz/boş plakalı girişte oturum açılmaz.
- Tekrar çıkış penceresi 5 dakikadır.
- Bekleyen çıkış, geç gelen girişle eşleşir.

## Bilinen sınırlar

- Bekleyen çıkışların zaman aşımı/temizliği bu aşamada yok.
- İnceleme kuyruğunu çözme akışı bu aşamada yok.
- B/8 gibi biçimi bozan okumalar `plate.Match` gereği otomatik eşleşmez; girişte `invalid_plate`,
  çıkışta `invalid_format` incelemesine düşer.
- Veritabanı ve servis katmanı sonraki parçada.

## Veritabanı katmanı

Paket: `internal/store`. pgx v5 üzerinde çalışır. Fonksiyonlar `pgx.Tx` ya da `pgxpool.Pool`'un
karşıladığı küçük `store.Querier` arayüzünü alır; transaction başlatmak ve bitirmek çağıranın işidir.

### Tablolar

- `migrations/00002_oturum.sql`: `lots`, `tariff_versions`, `plate_events`, `sessions`, `review_items`.
- `sessions_one_open_per_plate`: aynı lot ve plakada tek açık oturum.
- `tariff_versions_no_overlap`: lot başına tarife geçerlilik aralıkları çakışamaz (`[valid_from, valid_to)`).
- `sessions_state_chk`: açık oturumda çıkış/tutar yok; kapalı oturumda ikisi de dolu ve `exited_at >= entered_at`.

### Tarife tanımı (JSON)

`EncodeTariffDefinition` / `DecodeTariffDefinition` tarifenin yalnız tanımını JSON olarak taşır;
kimlik ve geçerlilik tarihleri tablo sütunlarındadır. Alanlar snake_case (`location`, `free_minutes`,
`weekday`, `weekend`, `events`, `bands`, `night`, `daily_cap_kurus`). Çözme sırasında bilinmeyen alan,
bozuk JSON, bozuk tarih, `Local`/boş/geçersiz saat dilimi reddedilir; sonuç `Version.Validate` ile
denetlenir ve `tariff.ErrInvalidTariff` korunur.

### Fonksiyonlar

- `CreateLot`, `LockLot` (`SELECT ... FOR UPDATE`): otopark kaydı; yoksa `ErrLotNotFound`.
- `InsertTariffVersion`, `TariffVersionAt`, `GetTariffVersion`: tarife yazma/okuma; an için sürüm yoksa
  `ErrNoTariff`, çakışma varsa `ErrTariffOverlap`, kayıt yoksa `ErrNotFound`.
- `InsertEvent`, `FindEventByCameraKey`, `SetEventOutcome`, `ListOrphanExits`: kamera olayları.
- `InsertSession`, `GetSession`, `ListOpenSessions`, `ListRecentClosed`, `CloseSession`, `CountOpenSessions`:
  oturumlar; ikinci açık oturum `ErrOpenSessionExists`, kapalı/yok oturumu kapatma `ErrSessionNotOpen`.
- `InsertReview`: inceleme kaydı; 0 öneri kimlikleri SQL NULL olarak yazılır.

Bilinen PostgreSQL kısıt hataları sentinel hataya eşlenir: `23505` + `sessions_one_open_per_plate` →
`ErrOpenSessionExists`, `23P01` + `tariff_versions_no_overlap` → `ErrTariffOverlap`. Diğer hatalar
olduğu gibi döner (ör. `23514` CHECK ihlali). SQL'lerin tamamı parametrelidir.

### Test düzeni

`internal/testdb` yalnız testler içindir: `TEST_DATABASE_URL` yoksa `PARKAKIL_REQUIRE_DB=1` ile
hata verir, aksi halde testi atlar. Migrasyonlar paket düzeyinde `sync.Once` ile bir kez uygulanır;
her test kendi benzersiz lot'uyla yalıtılır. `internal/store/store_test.go` gerçek PostgreSQL ister;
`test/faz6.js` geçici bir Docker ağında `postgres:16-alpine` başlatır, port açmaz, `go test -p 1`
koşturur (atlama yasak) ve her durumda konteyner ile ağı siler.
