# T1 — Hatalı Plaka Okumalarıyla Eşleştirme

Paket: `internal/domain/plate`. Saf Go paketi; veritabanı, HTTP, dosya, ağ, saat ve
rastgelelik kullanmaz. Karar üretir, yan etkisi yoktur.

## Normalize

`Normalize(raw string) string`

- `i`, `ı`, `İ` harfleri `I` olur; diğer harfler `unicode.ToUpper` ile büyütülür.
- `A-Z` ve `0-9` dışındaki her karakter atılır (boşluk, `-`, `.`, `_`, sekme,
  `Ş`, `Ç`, `Ğ`, `Ö`, `Ü`, emoji ve diğer unicode).
- Örnek: `" 34 abc-123 "` → `"34ABC123"`, `"06 ışk 01"` → `"06IK01"`, `""` → `""`.

`ValidTR(p string) bool`

- İl kodu 2 rakam, `01..81`; ardından 1-3 harf; ardından 2-4 rakam.
- `"34ABC123"`, `"06A1234"`, `"01ABC12"`, `"81ABC1234"` geçerli;
  `"00ABC12"`, `"82AB12"`, `"34ABCD12"`, `"34AB1"`, `"3OABC12"` geçersiz.

## Karışıklık tablosu ve mesafe

`ConfusionCost(x, y rune) int` tek bir tablodan okur (simetrik); tabloda olmayan
çift 100'dür. Tam işlem birimi 100'dür (ekleme/silme/değişim).

| Çift | Maliyet | Çift | Maliyet | Çift | Maliyet | Çift | Maliyet |
|---|---|---|---|---|---|---|---|
| O-0 | 20 | D-0 | 40 | Q-0 | 40 | I-1 | 20 |
| L-1 | 50 | T-1 | 60 | B-8 | 20 | S-5 | 30 |
| Z-2 | 30 | G-6 | 40 | A-4 | 60 | E-3 | 60 |
| T-7 | 50 | U-V | 50 | M-N | 60 | H-N | 70 |

`Distance(a, b string) int` normalize edilmiş girdiler üzerinde ağırlıklı
Levenshtein (DP) hesaplar. Ekleme/silme 100, eşit karakter 0, farklı karakter
tabloya göre 20..100.

## Güven

```
maxLen = max(len(a), len(b));  maxLen == 0 -> 0
conf   = 1000 - (Distance(a,b) * 10) / maxLen      (tam sayı bölmesi, alt sınır 0)
```

Girdiler normalize edilmiş olmalıdır. Örnekler: `"34ABC123"/"34A8C123"` → 975,
`"34ABC12"/"34ABC123"` → 875, eşit → 1000.

## Eşikler

`Thresholds{Auto, Review, Margin}`, varsayılan `{900, 700, 100}`.
`Validate`: `0 <= Review <= Auto <= 1000` ve `0 <= Margin <= 1000`; aksi hâlde hata.

## Karar tablosu

`Match(read, candidates, thresholds) (Result, error)` — sıra:

| # | Koşul | Karar | Neden |
|---|---|---|---|
| a | Eşikler geçersiz | — | hata |
| b | Normalize okuma boş | `none` | `empty_read` |
| c | Aday yok | `none` | `no_candidates` |
| d | Adaylar güvene göre azalan, eşitlikte ID artan sıralanır | | |
| e | Tek tam eşleşme | `exact` | `exact` |
| e | Birden fazla tam eşleşme | `review` | `ambiguous_exact` |
| f | En iyi < Review | `none` | `low_confidence` |
| g | En iyi >= Auto, fark >= Margin, okuma TR biçiminde | `auto` | `auto` |
| h | Diğer | `review` | `ambiguous` > `invalid_format` > `low_confidence_review` |

`Best` ve `RunnerUp` aday varsa her zaman doldurulur; inceleme kuyruğu öneriyi görür.

## Varsayımlar

- Eksik karakter tek başına otomatik eşleşmeye yetmez (güven 875 < Auto 900).
- Biçimsiz okuma en fazla incelemeye düşer; yüksek güven `auto` için yetmez.
- Eşikler ayarlanabilir; `Margin` ikinci adayla ayrımı belirler.
- Yabancı plakalar `ValidTR`'den geçmez, bu yüzden en fazla `review` olur.
- B/8 gibi harf-rakam karışıklıkları okumanın biçimini bozarsa (ör. `34A8C123`)
  karar `review/invalid_format` olur; `auto` için okuma da TR biçiminde olmalıdır.

## Bilinen sınırlar

- Transpozisyon (iki karakterin yer değiştirmesi) ayrı ele alınmaz; iki değişim
  sayılır = 200.
- Karışıklık ağırlıkları gerçek kamera verisiyle kalibre edilmedi.
