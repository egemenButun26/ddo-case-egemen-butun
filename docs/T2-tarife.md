# T-2 — Tarife motoru

`internal/domain/tariff` paketi saf bir domain paketidir: veritabanı, HTTP, dosya, ağ ve
`time.Now()` kullanmaz; zaman her zaman parametre olarak gelir. Para `Kurus` (int64) türündedir,
float hiçbir yerde kullanılmaz. Paket `time/tzdata` gömülü saat dilimi veritabanını içe aktarır;
bu yüzden distroless imajda da `Europe/Istanbul` ve `Europe/Berlin` çözülebilir.

## Model

| Tür | Açıklama |
|---|---|
| `Kurus` | Kuruş cinsinden para; `String()` → `1.234,50 TL`. Taşma denetimli `mulKurus`/`addKurus`. |
| `Date` | Saat dilimsiz takvim günü (`Year`, `Month`, `Day`). |
| `Band` | Dakika dilimi: `UpToMinutes` (0 = sınırsız son dilim), `UnitMinutes`, `UnitPrice`. |
| `Night` | Gece penceresi: gün başından dakika (`0..1439`), birim dakika, birim fiyat. |
| `DayRule` | `Bands`, `Night` (nil olabilir), günlük `DailyCap` (0 = tavan yok). |
| `Event` | Belirli `Date` için özel `DayRule`. |
| `Version` | Zaman geçerlilikli sürüm: `ValidFrom`, `ValidTo` (sıfır = açık uç), `Location`, `FreeMinutes`, `Weekday`, `Weekend`, `Events`. |
| `DayCharge` | Gün dökümü: tarih, kural adı, gündüz/gece saniyeleri ve tutarları, tavan bayrağı, gün tutarı. |
| `Quote` | Oturum dökümü: sürüm ID, giriş/çıkış, süre, ücretsiz bayrağı, günler, toplam. |

Sürüm doğrulama `Validate`, katalog doğrulama `ValidateCatalog`, seçim `SelectVersion`,
hesap `Calculate` ile yapılır.

## Hesap kuralları

1. `v.Validate()` hatası döner. `exit < entry` → `ErrNegativeDuration`. Süre 400 günü aşarsa `ErrTooLong`.
2. Süre birimi saniyedir; saniye altı kısım aşağı yuvarlanır. Süre ≤ `FreeMinutes*60` ise toplam 0,
   `Free = true`, gün listesi boş. Ücretsiz süre aşılırsa **tüm süre** ücretlenir (ücretsiz dakikalar düşülmez).
3. Oturum, `v.Location` saat diliminde takvim günlerine bölünür: `[entry, ertesi yerel gece yarısı), …,
   [son gece yarısı, exit)`. Gece yarısı `time.Date(y, m, d+1, 0, 0, 0, 0, loc)` ile bulunur; `+24h` kullanılmaz,
   yaz saati günleri 23/25 saat olabilir.
4. Gün kuralı: tarih bir `Event` ise o kural; değilse Cumartesi/Pazar ise `Weekend` (nil ise `Weekday`);
   değilse `Weekday`.
5. Gece penceresi: `Start > End` ise pencere gece yarısını sarar → `[00:00, End) ∪ [Start, 24:00)`;
   `Start < End` ise `[Start, End)`. Sınırlar yerel `time.Date` ile kurulur. Günün kalan parçası gece ve
   gündüz saniyelerine ayrılır.
6. Gündüz ücreti: günün gündüz saniyeleri `S` üzerinde dilimler sırayla uygulanır. Dilim `i`, `S`'nin
   `[önceki UpTo, UpTo)` dakika aralığını kapsar (son dilim sınırsız). Pay için birim sayısı
   `tavan(pay_saniye / (UnitMinutes*60))` ("her başlayan birim"), tutar `birim × UnitPrice`.
   Dilim sayacı her takvim günü sıfırdan başlar.
7. Gece ücreti: gece saniyeleri `N` için birim `tavan(N / (Night.UnitMinutes*60))`, tutar
   `birim × Night.UnitPrice`. `Night` yoksa gece saniyeleri de gündüz sayılır.
8. Gün tutarı = gündüz + gece. `DailyCap > 0` ve tutar tavandan büyükse tutar tavana çekilir, `Capped = true`.
9. Toplam = gün tutarlarının toplamı. Tüm çarpma ve toplamalar taşma denetimlidir (`ErrOverflow`).

## Sınırlar ve doğrulama

Dakika alanlarının saniyeye çevrilmesi taşma denetimlidir. Üst sınır
`MaxTariffMinutes = 400 * 24 * 60 = 576000` dakikadır; en uzun oturum süresi olan 400 günle
aynı gerekçeyle seçilmiştir. Sınırlar **dahildir**: `== MaxTariffMinutes` geçerli,
`MaxTariffMinutes + 1` geçersizdir.

`Validate` aşağıdaki alanları üst sınıra karşı denetler:

- `FreeMinutes` ≤ `MaxTariffMinutes` (0 geçerli, negatif geçersiz),
- her `Band.UnitMinutes` ≤ `MaxTariffMinutes` (0 ve negatif geçersiz),
- son olmayan her `Band.UpToMinutes` ≤ `MaxTariffMinutes` (0 ve negatif geçersiz; son dilim 0 olmalı),
- `Night.UnitMinutes` ≤ `MaxTariffMinutes` (0 ve negatif geçersiz).

`Validate`'in döndürdüğü her hata `ErrInvalidTariff` ile sarılır; çağıran
`errors.Is(err, ErrInvalidTariff)` ile ayırt edebilir. Hata mesajı sorunlu alanı ve değeri söyler.
Hafta içi, hafta sonu ve etkinlik kurallarının hepsi aynı denetimden geçer.

`Calculate` ayrıca savunma derinliği olarak dört dakika→saniye çevrimini `dakikaSaniye` üzerinden
yapar. `Validate` atlansa bile taşma sessizce sarmaz:

- `ErrInvalidTariff`: negatif dakika ya da doğrulamayı geçmeyen tarife,
- `ErrOverflow`: `dakika > math.MaxInt64/60`; çarpım int64 sınırını aşacak,
- `ErrNegativeAmount`: `mulKurus` negatif birim sayısı ya da negatif fiyat alır.

Sözleşme: **hata varsa tutar yoktur.** `Calculate` hata döndürdüğünde `Quote{}` (`Total == 0`)
döner; "hata nil + yanlış tutar" durumu oluşmaz.

## Standart sürüm

Saat dilimi `Europe/Istanbul`, `FreeMinutes = 15`.

- Hafta içi `hafta_ici`: dilimler `{60 dk, 15 dk birim, 1000}` ve `{sınırsız, 60 dk birim, 3000}`;
  gece `22:00–07:00`, 60 dk birim, 1500; günlük tavan 25000.
- Hafta sonu `hafta_sonu`: `{60 dk, 15 dk birim, 1500}`, `{sınırsız, 60 dk birim, 4000}`;
  gece `22:00–07:00`, 60 dk birim, 2000; günlük tavan 30000.
- Etkinlik `2026-11-01 derbi`: tek dilim `{sınırsız, 60 dk birim, 10000}`, gece yok, tavan yok.

### Beklenen değerler

| # | Oturum | Sonuç |
|---|---|---|
| a | Salı 27.10.2026 10:00 → 10:15:00 | 0, ücretsiz |
| b | Salı 10:00 → 10:15:01 | 2000 (ilk dilimde 2 başlayan 15 dk) |
| c | Salı 10:00 → 11:10 | 7000 (4×1000 + 1×3000) |
| d | Salı 08:00 → 21:00 | 25000, tavanlı (tavansız 40000) |
| e | Salı 21:00 → Çarşamba 01:00 | 8500 (4000 + 3000 + 1500) |
| f | Salı 23:55 → Çarşamba 00:05 | 0, ücretsiz (10 dk) |
| g | Salı 23:50 → Çarşamba 00:10 | 3000 (her güne ayrı başlayan birim: 1500 + 1500) |
| h | Cumartesi 31.10.2026 10:00 → 11:00 | 6000 (4×1500) |
| i | Pazar 01.11.2026 10:00 → 12:30 (etkinlik) | 30000 (3×10000; tavan yok) |
| j | Cuma 30.10.2026 23:00 → Cumartesi 08:00 | 21500 (1500 + 14000 + 6000) |
| k | Salı 27.10.2026 08:00 → Perşembe 29.10.2026 08:00 | 64500 (25000 + 25000 + 14500), 3 gün |
| l | Berlin 25.10.2026 00:00 → 26.10.2026 00:00 (yerel) | 2500, tek gün, gerçek süre 25 saat |

## Dondurma (sürüm sabitleme)

Oturum girişte `SelectVersion(vs, entry)` ile seçilen sürümün ID'sini saklar; ücret hesabına o sürüm
verilir. `Calculate` çıkış anına göre sürüm seçmez. Böylece oturum sırasında sürüm değişse de girişteki
tarife uygulanır.

Sınırlar yarı açıktır: `[ValidFrom, ValidTo)`. Tam `ValidFrom` yeni sürümü, tam `ValidTo` sonraki sürümü
verir. Bitişik aralıklar çakışma değildir; boşlukta `ErrNoVersion`, çakışmada `ErrAmbiguousVersion` döner.

## Varsayımlar

- 15 dakika aşılırsa tüm süre ücretlenir; ücretsiz dakikalar düşülmez.
- Günlük tavan takvim günü başına uygulanır.
- Dilim sayacı her takvim günü sıfırlanır.
- Kural önceliği: etkinlik > hafta sonu > hafta içi.
- Saniye altı süre yok sayılır (aşağı yuvarlanır).
- Saat dilimi sürümde tanımlıdır; giriş/çıkış başka dilimde verilse de sonuç aynıdır.
- Gün dökümündeki `DayMinutes` bilgi amaçlıdır ve yukarı yuvarlanır.

## Bilinen sınırlar

- Gece yarısını aşan oturumda her gün için ayrı "başlayan birim" hesaplandığından, örneğin 23:50 → 00:10
  oturumu 1500 + 1500 = 3000 tutar; tek parça hesaplansa 1 birim (1500) olurdu.
- Sürümler arası geçişte tek sürüm uygulanır; oturumun bir kısmı eski, bir kısmı yeni tarifeyle
  hesaplanmaz.
- `UpToMinutes`, `UnitMinutes`, `FreeMinutes` ve `Night.UnitMinutes` için üst sınır
  `MaxTariffMinutes` (576000 dakika) olduğundan int64 taşmasına yol açan aşırı değerler artık
  `Validate` aşamasında `ErrInvalidTariff` ile reddedilir; `dakikaSaniye` ikinci savunma katmanıdır.
