# F2 — Giriş/Çıkış Oturumu Karar Paketi

Paket: `internal/domain/session`. Saf Go paketi; veritabanı, HTTP, dosya, ağ, saat ve
rastgelelik kullanmaz. Kamera olayını ve mevcut oturumları alır, ne yapılacağına karar verir.
Veritabanı katmanı `internal/store`, servis katmanı `internal/service/sessionsvc` paketindedir.

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
| Açık oturumlarda tam/otomatik eşleşme, giriş zamanı pencereden çok önce | `review` / `late_entry` (önerilen oturum) |
| Açık oturumlarda tam/otomatik eşleşme | `duplicate_entry` (en iyi oturum) |
| Açık oturumlarda inceleme düzeyinde eşleşme | `review` / `possible_duplicate_entry` |
| Giriş anını (pencere payıyla) kapsayan kapanmış oturumda tam/otomatik eşleşme | `duplicate_entry` (kapalı oturum) |
| Kapsayan kapanmış oturumda inceleme düzeyinde eşleşme | `review` / `possible_duplicate_entry` |
| Hiçbiri | `opened` |

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
`low_confidence_review`, `ambiguous`, `invalid_format`, `ambiguous_exact`, `no_tariff`,
`late_entry`, `event_conflict`, `close_failed`.

`no_tariff`, `close_failed` ve `event_conflict` nedenlerini servis katmanı koyar; paket sabitleri hazırdır.
`late_entry` kapalı oturuma bağlanamayacak kadar eski bir geç girişte kullanılır.

## Varsayımlar

- Açık oturum varken aynı aracın yeniden girişi tekrar okumadır.
- Girişte inceleme düzeyinde benzerlik oturum açmaz; görevli karar verir.
- Geçersiz/boş plakalı girişte oturum açılmaz.
- Tekrar çıkış penceresi 5 dakikadır.
- Bekleyen çıkış, geç gelen girişle eşleşir.
- Kapalı bir oturumun aralığını kapsayan geç giriş yeni oturum açmaz; kapanmış oturuma
  `duplicate_entry` olarak bağlanır. Bu oturuma bağlanamayacak kadar eski geç girişler
  `late_entry` incelemesine gider.

## Bilinen sınırlar

- Bekleyen çıkışların zaman aşımı/temizliği yok.
- İnceleme kuyruğunu çözme akışı yok.
- B/8 gibi biçimi bozan okumalar `plate.Match` gereği otomatik eşleşmez; girişte `invalid_plate`,
  çıkışta `invalid_format` incelemesine düşer.
- `MatchOrphanExit` tek adayla çalıştığı için U/V gibi karışıklıkta yanlış araca bağlanabilir.
- Tarife sürümü satırlarının değişmezliği veritabanında zorlanmıyor (UPDATE fonksiyonu yok).
- HTTP ucu yok (auth aşamasında).

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
- `InsertSession`, `GetSession`, `ListOpenSessions`, `ListRecentClosed`, `ListClosedCovering`,
  `CloseSession`, `CountOpenSessions`: oturumlar; ikinci açık oturum `ErrOpenSessionExists`,
  kapalı/yok oturumu kapatma `ErrSessionNotOpen`. `ListClosedCovering` verilen anı (pencere payıyla)
  kapsayan kapalı oturumları döner.
- `InsertReview`: inceleme kaydı; 0 öneri kimlikleri SQL NULL olarak yazılır.

Bilinen PostgreSQL kısıt hataları sentinel hataya eşlenir: `23505` + `sessions_one_open_per_plate` →
`ErrOpenSessionExists`, `23505` + `plate_events_camera_event_uniq` → `ErrEventConflict`,
`23P01` + `tariff_versions_no_overlap` → `ErrTariffOverlap`. Diğer hatalar
olduğu gibi döner (ör. `23514` CHECK ihlali). SQL'lerin tamamı parametrelidir.

### Test düzeni

`internal/testdb` yalnız testler içindir: `TEST_DATABASE_URL` yoksa `PARKAKIL_REQUIRE_DB=1` ile
hata verir, aksi halde testi atlar. Migrasyonlar paket düzeyinde `sync.Once` ile bir kez uygulanır;
her test kendi benzersiz lot'uyla yalıtılır. `internal/store/store_test.go` gerçek PostgreSQL ister;
`test/faz6.js` geçici bir Docker ağında `postgres:16-alpine` başlatır, port açmaz, `go test -p 1`
koşturur (atlama yasak) ve her durumda konteyner ile ağı siler.

## Servis akışı

Paket: `internal/service/sessionsvc`. `Service.HandleEvent` bir kamera olayını tek bir
`pgx.ReadCommitted` transaction içinde işler; context zaman aşımına uyar.

1. `Event.Validate` başarısızsa hiçbir satır yazılmadan hata döner.
2. Transaction açılır ve `LockLot` ile otopark satırı kilitlenir; otopark yoksa
   `ErrLotNotFound` ile geri alınır. Aynı otoparkın olayları bu kilit sayesinde sırayla işlenir.
3. `FindEventByCameraKey` ile aynı `(camera_id, camera_event_id)` aranır. Bulunan kaydın
   `LotID`, `Direction`, `PlateRaw` ve `CapturedAt` alanları gelen olayla aynı değilse hiçbir şey
   yazılmadan `ErrEventConflict` döner. Aynıysa yeni satır yazılmaz; kayıtlı `EventID`, `Outcome`,
   `SessionID` ve varsa `ReviewID` ile `Replayed=true` döner. Sonuç `closed` ise `AmountKurus`
   oturumdan doldurulur. `InsertEvent` sırasında eşzamanlı farklı lot yarışından doğan
   `plate_events_camera_event_uniq` ihlali de ham `23505` yerine `ErrEventConflict` olur.
4. Girişte `ListOpenSessions` ve `ListClosedCovering` okunur, `DecideEntry` çağrılır:
   - `opened`: tarife sürümü giriş anında (`TariffVersionAt`) seçilir, olay ve oturum yazılır,
     olayın sonucu oturuma bağlanır. Ardından `ListOrphanExits` + `MatchOrphanExit` ile bekleyen
     çıkış eşleşirse oturum o çıkışla hemen kapatılır (`ClosedByOrphanExit`).
     Tarife sürümü yoksa `no_tariff` incelemesi yazılır.
   - `duplicate_entry`: olay açık oturuma ya da girişi kapsayan kapalı oturuma bağlanır.
   - `review`: olay `review` sonucuyla ve `review_items` satırıyla yazılır (`late_entry` dahil).
5. Çıkışta `DecideExit` (açık oturumlar ve son `DuplicateWindow` içinde kapananlar):
   `closed` olayı oturumu kapatır, `duplicate_exit` ve `orphan_exit` kendi sonuçlarıyla yazılır,
   `review` inceleme satırı üretir.
6. Kapatma: oturumun **dondurulmuş** tarife sürümü `GetTariffVersion` ile okunur (çıkış anına göre
   sürüm seçilmez), `tariff.Calculate` ile tutar ve quote JSON üretilir, `CloseSession` yazar.
   `close` hesaptan önce hiçbir yazma yapmaz. Hesap hatası (`close_failed`) olayı kaybettirmez:
   - Normal çıkışta çıkış olayı `review`, oturum açık kalır; `review_items` nedeni `close_failed`,
     önerilen oturum kapatılamayan oturumdur. `HandleEvent` hatasız `review` döner.
   - Bekleyen çıkışla kapatmada giriş oturumu `opened` kalır; bekleyen çıkış olayı `review` olur ve
     `close_failed` incelemesi yeni oturumu önerir. `ClosedByOrphanExit=false` döner.
   - `tariff.Calculate` dışındaki veritabanı hataları eskisi gibi hata olarak döner.
7. Commit; herhangi bir hata rollback eder. `ErrOpenSessionExists` kilit sayesinde beklenmez;
   yine de oluşursa hata sarılarak döner.

## Kanıt

- `internal/service/sessionsvc/service_test.go`: gerçek PostgreSQL üzerinde 27 entegrasyon testi
  (önceki 19 test + geç giriş, olay çakışması ve kapatma hatası senaryoları). Testler
  `t.Parallel()` kullanmaz; her test kendi lot'unu ve tarifesini kurar.
- `test/faz6.js`: geçici Docker ağında `postgres:16-alpine` başlatır, host'a port açmaz,
  `go test -p 1` koşturur; `--- SKIP` yasaktır. Ek kontroller: beklenen testlerin `--- PASS`
  satırları, `internal/store` kapsamı >= %70, `internal/service/sessionsvc` kapsamı >= %75 ve
  iki 50 eşzamanlı testin süresi < 30 sn.
- Son ölçüm: `internal/store` kapsamı %88,2; `internal/service/sessionsvc` kapsamı %76,5;
  eşzamanlı test süreleri 0,55 sn ve 0,33 sn; faz6 39 kontrol geçti.
- Mutasyon kontrolü: `LockLot` kaldırılınca `TestEsZamanli50GirisKarisikOkuma` kırıldı
  (`ErrOpenSessionExists`); kapatmada dondurulmuş sürüm yerine `TariffVersionAt` kullanılınca
  `TestTarifeDondurma` kırıldı (90,00 TL / 4.500 kuruş); `FindEventByCameraKey` kontrolü
  kaldırılınca `TestAyniKameraOlayiTekrar` kırıldı (23505). Her mutasyon geri alındı.
- Mutasyon kontrolü (geç giriş/çakışma/kapatma hatası parçası): tekrar kontrolündeki içerik
  karşılaştırması kaldırılınca `TestOlayCakismasiFarkliOtopark` kırıldı (`ErrEventConflict`
  yerine `<nil>`); `ListClosedCovering` yerine nil verilince `TestGecGirisKapaliOturum` kırıldı
  (`opened` yerine `duplicate_entry` beklentisi); normal çıkıştaki `close_failed` yolu kaldırılıp
  hata döndürülünce `TestKapatmaHatasiIncelemeye` kırıldı (`close_failed` hatası). Hepsi geri alındı.
- Doğrulama: `gofmt -l .` boş, `go vet ./...` temiz, `go test ./...` (veritabanı yokken
  entegrasyon testleri atlanır) yeşil; `node test/faz6.js`, `faz5.js`, `faz4.js`, `faz3.js`,
  `faz1.js` yeşil.
