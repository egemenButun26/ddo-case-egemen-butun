# ParkAkıl

ParkAkıl'ın Go API iskeleti, PostgreSQL ve statik durum sayfası için Docker Compose altyapısı.

## Kurulum

```sh
cp .env.example .env
docker compose up --build
```

Windows PowerShell'de kopyalamak için: `Copy-Item .env.example .env`

## Adresler

| Ne | Adres |
|---|---|
| Web arayüzü (sistem durumu) | http://localhost:3000 |
| API | http://localhost:8080 |
| API sağlık kontrolü | http://localhost:8080/health |

## Kamera olay ucu

Kamera olayları web üzerinden `POST /api/camera-events`, API portu üzerinden `POST /camera-events` ile gönderilir.
İstekte `X-Camera-Key` başlığı zorunludur; eksik ya da yanlışsa `401` döner.
Anahtar `.env` dosyasındaki `CAMERA_API_KEY` değeridir; gerçek anahtarı bu dosyaya yazmayın.

## Port çakışması

Host'ta 8080 ya da 3000 doluysa `.env` içinde portları değiştirin:

```sh
API_HOST_PORT=18080
WEB_HOST_PORT=13000
```

## Testler

```sh
node test/faz1.js
node test/faz2.js
npm test
```

`test/faz2.js` Docker gerektirir; uçtan uca kabul testini çalıştırır.

## Make (varsa)

```sh
make up
make down
make logs
make test
make smoke
```

Make olmadan eşdeğerleri:

| Make | Komut |
|---|---|
| `make up` | `docker compose up -d --build --wait` |
| `make down` | `docker compose down -v --remove-orphans` |
| `make logs` | `docker compose logs -f --tail=100` |
| `make test` | `docker run --rm -v "$(pwd):/src" -v parkakil-gomod:/go/pkg/mod -w /src -e GOFLAGS=-buildvcs=false golang:1.23-alpine sh -c "go vet ./... && go test ./..."` |
| `make smoke` | `node test/faz2.js` |

Durum: yalnızca iskelet (aşama 0); iş özellikleri henüz yok.
