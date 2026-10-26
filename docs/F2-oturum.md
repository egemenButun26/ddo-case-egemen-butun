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
| Açık oturumlardan biriyle aynı karışıklık anahtarı (`Match` eşleşme saymasa bile) | `review` / `possible_duplicate_entry` (önerilen oturum) |
| `exited_at >= giriş anı` olan kapanmış oturumda tam/otomatik eşleşme, giriş `entered_at - DuplicateWindow` içinde | `duplicate_entry` (kapalı oturum) |
| Aynı kapanmış oturumda tam/otomatik eşleşme, giriş daha eski | `review` / `late_entry` (önerilen kapalı oturum) |
| Kapanmış oturumda inceleme düzeyinde eşleşme | `review` / `possible_duplicate_entry` |
| Açık oturum sayısı kapasiteye eşit/dolu, yeni oturum açılacaktı | `review` / `capacity_full` |
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

- Aynı kamera olay kimliği aynı içerikle tekrar gelirse ilk sonuç `Replayed=true` döner; içerik
  (lot, yön, ham plaka, `captured_at`) farklıysa `ErrEventConflict` döner ve hiçbir kayıt yazılmaz.
- Açık oturum varken aynı aracın yeniden girişi yeni oturum açmaz; `duplicate_entry` olur.
- Çıkıştan hemen sonra gelen aynı plaka çıkışı `duplicate_exit` sayılır. Pencere varsayılan 5 dakikadır
  ve sınır dahildir (tam 5 dk tekrar, 5 dk 1 sn bekleyen çıkış).
- Girişi görülmemiş çıkış `orphan_exit` olarak saklanır; giriş olayı sonradan gelirse
  `MatchOrphanExit` en erken uygun bekleyen çıkışı eşleştirir.
- Bekleyen çıkış seçimi giriş zamanından önce olanları atlar; eşitlikte küçük `EventID` kazanır.
- Giriş kararında kapalı oturum adayları `exited_at >= giriş anı` olanlardır. En iyi adayla giriş
  `entered_at - DuplicateWindow` sınırında ya da sonrasındaysa `duplicate_entry`, daha eskisiyse
  `late_entry` incelemesi olur; önerilen oturum kapalı oturumdur. Kapalı oturum bittikten sonraki
  giriş (ör. 12:30) aday listesine girmez ve normal açılır.
- Açık oturumlardan biriyle aynı karışıklık anahtarına sahip giriş, `plate.Match` inceleme düzeyinde
  eşleşme bulmasa bile yeni oturum açmaz; `possible_duplicate_entry` incelemesine düşer.
- Kapasite kontrolü yalnız yeni oturum açılacakken yapılır: açık oturum sayısı kapasiteye eşit ya da
  büyükse olay `review` / `capacity_full` olur. Tekrar giriş ve diğer inceleme kararları kapasiteden
  etkilenmez.

## İnceleme nedenleri

`empty_read`, `invalid_plate`, `possible_duplicate_entry`, `exit_before_entry`,
`low_confidence_review`, `ambiguous`, `invalid_format`, `ambiguous_exact`, `no_tariff`,
`late_entry`, `capacity_full`, `close_failed`.

`no_tariff` ve `close_failed` nedenlerini servis katmanı koyar; paket sabitleri hazırdır.
`late_entry`, açık oturumdan ya da kapalı oturum adayından pencereden daha eski bir geç girişte
kullanılır. `event_conflict` bir inceleme nedeni değildir; aynı kamera olay kimliği farklı içerikle
gelirse `ErrEventConflict` hatası döner ve kayıt yazılmaz.

## Varsayımlar

- Açık oturum varken aynı aracın yeniden girişi tekrar okumadır.
- Girişte inceleme düzeyinde benzerlik oturum açmaz; görevli karar verir.
- Geçersiz/boş plakalı girişte oturum açılmaz.
- Tekrar çıkış penceresi 5 dakikadır.
- Bekleyen çıkış, geç gelen girişle eşleşir.
- Giriş anı kapalı bir oturumun `entered_at - DuplicateWindow` sınırında ya da sonrasındaysa
  kapanmış oturuma `duplicate_entry` olarak bağlanır; daha eski geç girişler `late_entry`
  incelemesine gider. Kapalı oturum bittikten sonraki girişler yeni oturum açar.
- Karışıklık sınıfı anahtarı (`plate.ConfusionKey`) harf/rakam varyantlarını (U/V, H/M/N, O/0/D/Q vb.)
  aynı sınıfa koyar; aynı sınıftaki ikinci araç açık oturum alamaz, incelemeye düşer.
- Otopark kapasitesi açık oturum sayısıyla ölçülür; rezervasyon ve sayaç yoktur.

## Bilinen sınırlar

- Bekleyen çıkışların zaman aşımı/temizliği yok.
- İnceleme kuyruğunu çözme akışı yok.
- B/8 gibi biçimi bozan okumalar `plate.Match` gereği otomatik eşleşmez; girişte `invalid_plate`,
  çıkışta `invalid_format` incelemesine düşer.
- `MatchOrphanExit` tek adayla çalıştığı için U/V gibi karışıklıkta yanlış araca bağlanabilir.
- Tarife sürümü satırlarının değişmezliği veritabanında zorlanmıyor (UPDATE fonksiyonu yok).
- Kapasite yalnız açık oturum sayısıdır; rezervasyon ve sayaç (T-3'ün kalanı) yoktur. Bekleyen çıkışı
  olan (aslında çıkmış) aracın geç girişi de otopark doluyken `capacity_full` incelemesine düşer.
- Karışıklık indeksi yalnız aynı uzunluktaki sınıf varyantlarını korur; eksik/fazla karakterli okumalar
  (`34ABC12` gibi) hâlâ lot kilidi + `plate.Match` ile ayrılır. Bedel: aynı karışıklık anahtarına sahip
  iki gerçek farklı araç aynı anda açık oturum alamaz, ikincisi incelemeye düşer.
- HTTP ucu yok (auth aşamasında).

## Veritabanı katmanı

Paket: `internal/store`. pgx v5 üzerinde çalışır. Fonksiyonlar `pgx.Tx` ya da `pgxpool.Pool`'un
karşıladığı küçük `store.Querier` arayüzünü alır; transaction başlatmak ve bitirmek çağıranın işidir.

### Tablolar

- `migrations/00002_oturum.sql`: `lots`, `tariff_versions`, `plate_events`, `sessions`, `review_items`.
- `migrations/00003_karisiklik_anahtari.sql`: `sessions_one_open_per_plate_key` kısmi UNIQUE indeksi.
- `sessions_one_open_per_plate`: aynı lot ve plakada tek açık oturum.
- `sessions_one_open_per_plate_key`: aynı lot ve `translate(plate, 'ODQILT7BSZGAEVHN', '0001111852643UMM')`
  değerinde tek açık oturum; karışıklık sınıfı varyantlarını (U/V, H/M/N, O/0/D/Q vb.) veritabanında korur.
  `plate.ConfusionKey` aynı dönüşümü Go tarafında üretir.
- `tariff_versions_no_overlap`: lot başına tarife geçerlilik aralıkları çakışamaz (`[valid_from, valid_to)`).
- `sessions_state_chk`: açık oturumda çıkış/tutar yok; kapalı oturumda ikisi de dolu ve `exited_at >= entered_at`.

### Tarife tanımı (JSON)

`EncodeTariffDefinition` / `DecodeTariffDefinition` tarifenin yalnız tanımını JSON olarak taşır;
kimlik ve geçerlilik tarihleri tablo sütunlarındadır. Alanlar snake_case (`location`, `free_minutes`,
`weekday`, `weekend`, `events`, `bands`, `night`, `daily_cap_kurus`). Çözme sırasında bilinmeyen alan,
bozuk JSON, bozuk tarih, `Local`/boş/geçersiz saat dilimi reddedilir; sonuç `Version.Validate` ile
denetlenir ve `tariff.ErrInvalidTariff` korunur.

### Fonksiyonlar

- `CreateLot`, `LockLot` (`SELECT capacity ... FOR NO KEY UPDATE`, kapasiteyi döner): otopark kaydı;
  yoksa `ErrLotNotFound`.
- `InsertTariffVersion`, `TariffVersionAt`, `GetTariffVersion`: tarife yazma/okuma; an için sürüm yoksa
  `ErrNoTariff`, çakışma varsa `ErrTariffOverlap`, kayıt yoksa `ErrNotFound`.
- `InsertEvent`, `FindEventByCameraKey`, `SetEventOutcome`, `ListOrphanExits`: kamera olayları.
- `InsertSession`, `GetSession`, `ListOpenSessions`, `ListRecentClosed`, `ListClosedEndingAfter`,
  `CloseSession`, `CountOpenSessions`: oturumlar; ikinci açık oturum `ErrOpenSessionExists`,
  kapalı/yok oturumu kapatma `ErrSessionNotOpen`. `ListClosedEndingAfter` `exited_at >= at` olan
  kapalı oturumları `id` sırasıyla döner.
- `InsertReview`: inceleme kaydı; 0 öneri kimlikleri SQL NULL olarak yazılır.

Bilinen PostgreSQL kısıt hataları sentinel hataya eşlenir: `23505` + `sessions_one_open_per_plate` ya da
`sessions_one_open_per_plate_key` → `ErrOpenSessionExists`, `23505` + `plate_events_camera_event_uniq` →
`ErrEventConflict`, `23P01` + `tariff_versions_no_overlap` → `ErrTariffOverlap`. Diğer hatalar
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
2. Transaction açılır ve `LockLot` ile otopark satırı kilitlenir; kapasite okunur, otopark yoksa
   `ErrLotNotFound` ile geri alınır. Aynı otoparkın olayları bu kilit sayesinde sırayla işlenir ve
   kapasite kontrolü kilit altında yapıldığı için eşzamanlı girişler kapasiteyi aşamaz.
3. `FindEventByCameraKey` ile aynı `(camera_id, camera_event_id)` aranır. Bulunan kaydın
   `LotID`, `Direction`, `PlateRaw` ve `CapturedAt` alanları gelen olayla aynı değilse hiçbir şey
   yazılmadan `ErrEventConflict` döner. Aynıysa yeni satır yazılmaz; kayıtlı `EventID`, `Outcome`,
   `SessionID` ve varsa `ReviewID` ile `Replayed=true` döner. Sonuç `closed` ise `AmountKurus`
   oturumdan doldurulur. `InsertEvent` sırasında eşzamanlı farklı lot yarışından doğan
   `plate_events_camera_event_uniq` ihlali de ham `23505` yerine `ErrEventConflict` olur.
4. Girişte `ListOpenSessions` ve `ListClosedEndingAfter` okunur, `DecideEntry(cfg, ev, kapasite, open, closed)`
   çağrılır. Karar sırası: açık oturum eşleşmesi, kapalı oturum eşleşmesi, açık oturumlarla karışıklık
   anahtarı, kapasite.
   - `opened`: tarife sürümü giriş anında (`TariffVersionAt`) seçilir, olay ve oturum yazılır,
     olayın sonucu oturuma bağlanır. Ardından `ListOrphanExits` + `MatchOrphanExit` ile bekleyen
     çıkış eşleşirse oturum o çıkışla hemen kapatılır (`ClosedByOrphanExit`).
     Tarife sürümü yoksa `no_tariff` incelemesi yazılır.
   - `duplicate_entry`: olay açık oturuma ya da kapalı oturum adayına bağlanır.
   - `review`: olay `review` sonucuyla ve `review_items` satırıyla yazılır (`late_entry`,
     `possible_duplicate_entry`, `capacity_full` dahil).
   - `sessions_one_open_per_plate_key` indeksine normal akışta çarpılmaz; karışıklık anahtarı kontrolü
     bunu önler, indeks yalnız son savunma hattıdır.
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

- `internal/service/sessionsvc/service_test.go`: gerçek PostgreSQL üzerinde 31 entegrasyon testi
  (önceki 27 test + kapasite, eşzamanlı kapasite, kapalı oturum öncesi geç giriş ve karışıklık
  anahtarı senaryoları). Testler `t.Parallel()` kullanmaz; her test kendi lot'unu ve tarifesini kurar.
- `internal/store/store_test.go`, `internal/domain/session/decide_test.go` ve
  `internal/domain/plate/confusionkey_test.go`: kapasite, `ConfusionKey`, SQL/Go anahtar eşitliği,
  `ListClosedEndingAfter` sınırları ve karışıklık indeksi testleri.
- `test/faz7.js`: geçici Docker ağında `postgres:16-alpine` başlatır, host'a port açmaz,
  `go test -p 1` koşturur; `--- SKIP` yasaktır. Ek kontroller: beklenen testlerin `--- PASS`
  satırları, `internal/store` kapsamı >= %70, `internal/service/sessionsvc` kapsamı >= %75 ve
  üç 50 eşzamanlı testin süresi < 30 sn. `test/faz6.js` değişmeden geçer.
- Son ölçüm: `internal/store` kapsamı %88,2; `internal/service/sessionsvc` kapsamı %76,7;
  eşzamanlı test süreleri 0,23 sn, 0,16 sn ve 0,26 sn; faz7 21 kontrol, faz6 39 kontrol geçti.
- Mutasyon kontrolü: `LockLot` kaldırılınca `TestEsZamanli50GirisKarisikOkuma` kırıldı
  (`ErrOpenSessionExists`); kapatmada dondurulmuş sürüm yerine `TariffVersionAt` kullanılınca
  `TestTarifeDondurma` kırıldı (90,00 TL / 4.500 kuruş); `FindEventByCameraKey` kontrolü
  kaldırılınca `TestAyniKameraOlayiTekrar` kırıldı (23505). Her mutasyon geri alındı.
- Mutasyon kontrolü (geç giriş/çakışma/kapatma hatası parçası): tekrar kontrolündeki içerik
  karşılaştırması kaldırılınca `TestOlayCakismasiFarkliOtopark` kırıldı (`ErrEventConflict`
  yerine `<nil>`); `ListClosedEndingAfter` yerine nil verilince `TestGecGirisKapaliOturum` kırıldı
  (`opened` yerine `duplicate_entry` beklentisi); normal çıkıştaki `close_failed` yolu kaldırılıp
  hata döndürülünce `TestKapatmaHatasiIncelemeye` kırıldı (`close_failed` hatası). Hepsi geri alındı.
- Mutasyon kontrolü (faz7): `DecideEntry` kapasite kontrolü silinince `TestKapasiteDolu`
  ("opened" yerine "review" beklentisi) ve `TestEsZamanli50GirisKapasite` (50 `opened`, 10 beklenen)
  kırıldı; karışıklık anahtarı kuralı silinince `TestKarisiklikAnahtariGiris`
  (`ErrOpenSessionExists`) ve `TestDecideEntryKarisiklikAnahtari` (`opened` yerine `review`) kırıldı;
  `00003` Up boş bırakılınca `TestOpenSessionUniqueConfusionKey` (`<nil>` yerine
  `ErrOpenSessionExists`) kırıldı; geç giriş `late_entry` dalı eski `duplicate_entry` davranışına
  döndürülünce `TestGecGirisKapaliOturumOncesi` ve `TestDecideEntryKapaliOturum` kırıldı.
  Mutasyonlar geçici dosya kopyasıyla geri alındı (değişiklikler commit edilmediği için
  `git checkout --` kullanılmadı).
- Doğrulama: `gofmt -l .` boş, `go vet ./...` temiz, `go test ./...` (veritabanı yokken
  entegrasyon testleri atlanır) yeşil; `node test/faz7.js`, `faz6.js`, `faz5.js`, `faz4.js`,
  `faz3.js` yeşil.
