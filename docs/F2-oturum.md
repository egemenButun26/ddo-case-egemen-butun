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
