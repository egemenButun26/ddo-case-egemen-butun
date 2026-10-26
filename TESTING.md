# ParkAkıl — Test ve Doğrulama Kılavuzu

Bu kılavuz, teslim edilen sistemi kısa sürede denemek içindir. Tüm komutlar repo
kökünde çalıştırılır. Önce `.env` oluşturun:

```sh
cp .env.example .env
docker compose up --build --wait
```

`make` yoksa komutların eşdeğerleri tablolarda verilmiştir.

## 1. Demo hesapları

Parola, `.env` içindeki `SEED_DEMO_PASSWORD` değeridir. `.env.example` ile
kurulumda bu değer `demo-parola-degistir` olur.

| Rol | E-posta | Parola | Not |
|---|---|---|---|
| Sistem yöneticisi | `seed.ardic@ddo-case.invalid` | `demo-parola-degistir` | İlk seed kullanıcısı |
| Sürücü | `surucu@parkakil.test` | `demo-parola-degistir` | Plaka: `34ABC123`, KADIKOY aboneliği |
| Otopark görevlisi | `gorevli@parkakil.test` | `demo-parola-degistir` | İnceleme kuyruğu, doluluk, borç sorgu |
| Muhasebe | `muhasebe@parkakil.test` | `demo-parola-degistir` | Ödeme, fatura, iade, mutabakat |
| İşletme müdürü | `mudur@parkakil.test` | `demo-parola-degistir` | Rapor, kara liste, rezervasyon |

Parolayı değiştirdiyseniz tablodaki değer yerine kendi `.env` değerinizi kullanın.

## 2. Tek komutla doğrulama

```sh
make verify
```

`make` yoksa:

```sh
node scripts/verify/verify.js
```

Betik, çalışan ve seed edilmiş sistemde aşağıdaki kimliklerin her birini API
üzerinden dener; sonunda `PASS/FAIL` tablosu basar ve FAIL varsa sıfırdan farklı
çıkış kodu döner. Süre normalde 2–4 dakikadır (15 dakika sınırı vardır).
Doğrulama demo verisini değiştirebilir; temiz başlangıç için:

```sh
make reset-demo
```

`make` yoksa:

```sh
docker compose down -v --remove-orphans
docker compose up -d --build --wait
```

## 3. Kimlik başına nasıl denenir / beklenen sonuç

| Kimlik | Nasıl denenir | Beklenen sonuç |
|---|---|---|
| `F-1` | `make verify` → F-1; arayüzde Görevli ekranı | En az 5 otopark, canlı olay akışı dolu; `POST /camera-events` 201 ve `opened` döner. |
| `F-2` | `POST /camera-events` ile aynı plakaya giriş, sonra çıkış | Çıkış 201, `outcome=closed`, aynı `session_id`, `amount_kurus` dolu. |
| `F-3` | 10 dk ve 2 saatlik iki oturum | 10 dk → 0 kuruş; 2 saat → 7000 kuruş (seed tarifesi). |
| `F-4` | Sürücü/abonelik ekranı; `POST /reservations`, `POST /subscriptions` | Rezervasyon 30 dk, iptal edilebilir; abonelik oluşturma/iptal çalışır. |
| `F-5` | Kiosk ekranı; `GET /kiosk/quote`, `POST /kiosk/payments` | Teklif doğru tutarı verir; aynı `Idempotency-Key` aynı `payment_id` döner. |
| `F-6` | Görevli ekranı plaka borç sorgusu; müdür kara liste | Borç toplamı görünür; kara listeye ekleme/silme 201/204. |
| `F-7` | Görevli doluluk; muhasebe günlük rapor; `GET /metrics` | Doluluk 5 otopark; `/reports/daily` giriş sayısı > 0; web 3000 yanıt verir. |
| `T-1` | `make verify` → T-1; `node test/faz3.js` | U/V OCR karışıklığı aynı oturuma bağlanır; geçersiz okuma `review` kuyruğuna düşer ve çözülür. |
| `T-2` | `make verify` → T-2; `node test/faz4.js` | İlk 15 dk ücretsiz; dilim, gün tavanı, gece, hafta sonu ve etkinlik kuralları testli. |
| `T-3` | `make verify` → T-3; `node test/faz16.js` | Rezervasyon kapasiteye yansır; 30 dk sonra serbest kalır; recount/kayma düzeltmesi çalışır. |
| `T-4` | `make verify` → T-4; `node test/faz15.js`, `faz20.js` | Idempotent ödeme, sağlayıcı gecikmesi, 15 dk çıkış izni, kapı kararı p95 < 50 ms, iade. |
| `T-5` | `make verify` → T-5; `node test/faz18.js`, `faz22.js` | Prorated iptal, dunning, dönem sonu idempotency, günlük mutabakat farkı. |
| `HEALTH` | `curl -i localhost:8080/health` ve `/_hz/45a6` | İkisi de 200; `X-DDO-Node: 45a6ab`; DB kontrolü `ok`. |
| `AUTH` | Yanlış parola; sürücüyle `/admin/users` | 401; sürücü için 403. |

## 4. Uçtan uca kanıt komutları

| Görev | Komut | Kanıt |
|---|---|---|
| T-1 plaka eşleştirme | `node test/faz3.js` | Plaka paketi Go testleri + kapsam ≥ %85 (ölçülen: %96,3) |
| T-1 oturum entegrasyonu | `node test/faz6.js` | Gerçek PostgreSQL; eşzamanlı 50 giriş, tek oturum |
| T-2 tarife | `node test/faz4.js` | Tarife dilimleri, tavan, gece, hafta sonu, etkinlik |
| T-3 kapasite/sayaç | `node test/faz16.js` | Eşzamanlı rezervasyon, 30 dk serbest bırakma, drift |
| T-4 ödeme/kapı | `node test/faz11.js` + `node test/faz15.js` + `node test/faz20.js` | Domain p95 < 50 ms, idempotency, geç onay, iade |
| T-5 faturalama | `node test/faz18.js` + `node test/faz22.js` | Proration, dunning, dönem sonu, mutabakat |
| F-4/F-6/F-7 | `node test/faz21.js` | Kara liste, doluluk, rezervasyon, rapor, canlı akış |
| Frontend | `node test/faz23.js` | Typecheck + Vitest (≥ 40 test) + üretim derlemesi |
| Metrik/OpenAPI | `node test/faz19.js` + `node test/faz27.js` | `/metrics`, OpenAPI 3.1 |
| Geliştirici notları | `node test/faz26.js` | `ya-` tablolar, `ya-qo-users`, `X-DDO-Node`, `/_hz/45a6`, seed, container adı, README son satırı |
| Yük testi | `make load` | p50/p95/p99 ve hata oranı; sonuç README'de |
| Kaos | `make chaos` | PostgreSQL durur, API 503 verir, geri gelince toparlanır |

## 5. Arayüzde elle denenecek senaryolar

1. **Sürücü:** `surucu@parkakil.test` ile giriş yapın. Borç, abonelik ve faturalar
   görünmeli. Bir otopark seçip rezervasyon oluşturun; listede görünmeli.
   Rezervasyonu iptal edin. Abonelik iptalinde orantılı iade faturası oluşmalı.
2. **Görevli:** Doluluk özeti, inceleme kuyruğu ve son 10 saniyede güncellenen
   canlı olay akışı görünmeli. Bir incelemeyi “Yoksay” ile çözün. Plaka borç
   sorgusunda `34ABC123` için borç/abonelik durumu görünmeli.
3. **Muhasebe:** Ödeme listesi, faturalar, dönem sonu ve mutabakat ekranlarını
   açın. Bir faturaya başarısız ödeme denemesi işleyin; yeniden deneme zamanı
   görünmeli. Mutabakat raporunda fark alanı görünmeli.
4. **Müdür:** Doluluk, günlük rapor, kara liste ve sayaç kaymaları ekranlarını
   açın. Bir plakayı kara listeye ekleyip silin. Rapor gününü değiştirin.
5. **Sistem yöneticisi:** Kullanıcı listesi (parola özeti görünmemeli) ve
   doluluk yeniden hesaplama ekranını açın.
6. **Yetkisiz erişim:** Sürücü ile `/yonetici` adresine gidin; ekran açılmamalı.
   Doğrudan `GET /admin/users` isteği 403 dönmeli.
7. **Mobil:** Tarayıcıyı 375 px genişliğe getirin; menü, kartlar ve tablolar
   yatay taşmadan kullanılabilmeli.
8. **Hata/boş durum:** API'yi durdurun (`docker compose stop api`); ekranlarda
   hata ve “Tekrar dene” görünmeli. Seed sonrası boş liste olan ekranlarda
   “Kayıt yok” mesajı görünmeli.

## 6. Elle API koleksiyonu

`requests.http` dosyası VS Code REST Client ile açılır. Önce `login` isteğini
çalıştırıp dönen token'ı diğer isteklerde kullanın. Kamera, kiosk ve kapı
isteklerinde `X-Camera-Key` başlığı `.env` içindeki `CAMERA_API_KEY` olmalıdır.
`Idempotency-Key` gereken uçlar dosyada işaretlenmiştir.

## 7. Bilinen sınırlar

- `make verify` demo verisini değiştirir (yeni oturum, abonelik, fatura, kara
  liste kayıtları). Tekrar temiz doğrulama için `make reset-demo` kullanın.
- 30 dakikalık rezervasyon serbest bırakma süresi `make verify` içinde
  beklenmez; bu davranış `node test/faz16.js` ile kanıtlanır.
- Sağlayıcı simülatörü olasılıksaldır. `make verify` ödeme onayı için birkaç
  kez dener; nadiren 2 dakikaya kadar sürebilir.
- K6 yük testi `make load` ayrı çalışır; `make verify` 15 dakika sınırını
  aşmamak için yük testini içermez.
