package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"parkakil/internal/simulator"
)

func TestLoadSettingsDefaults(t *testing.T) {
	env := map[string]string{
		"CAMERA_API_KEY": "anahtar",
		"DATABASE_URL":   "postgres://ornek",
	}
	ayar, err := loadSettings(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("ayarlar yuklenemedi: %v", err)
	}
	if ayar.apiURL != defaultAPIURL {
		t.Fatalf("api url %q", ayar.apiURL)
	}
	if ayar.seed != defaultSeed || ayar.eventsPerDay != defaultEventsPerDay || ayar.speed != defaultSpeed {
		t.Fatalf("varsayilanlar yanlis: %+v", ayar)
	}
	if !ayar.start.Equal(defaultStartTime()) {
		t.Fatalf("baslangic %s", ayar.start)
	}
}

func TestLoadSettingsCustom(t *testing.T) {
	env := map[string]string{
		"CAMERA_API_KEY":     "anahtar",
		"DATABASE_URL":       "postgres://ornek",
		"SIM_API_URL":        "http://localhost:9999/",
		"SIM_SEED":           "42",
		"SIM_EVENTS_PER_DAY": "123",
		"SIM_SPEED":          "2.5",
		"SIM_START":          "2026-01-02T03:04:05Z",
	}
	ayar, err := loadSettings(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("ayarlar yuklenemedi: %v", err)
	}
	if ayar.apiURL != "http://localhost:9999" {
		t.Fatalf("api url %q", ayar.apiURL)
	}
	if ayar.seed != 42 || ayar.eventsPerDay != 123 || ayar.speed != 2.5 {
		t.Fatalf("ayarlar yanlis: %+v", ayar)
	}
	if ayar.start.Format(time.RFC3339) != "2026-01-02T03:04:05Z" {
		t.Fatalf("baslangic %s", ayar.start)
	}
}

func TestLoadSettingsErrors(t *testing.T) {
	bozuk := []map[string]string{
		{"DATABASE_URL": "postgres://ornek"},
		{"CAMERA_API_KEY": "anahtar"},
		{"CAMERA_API_KEY": "anahtar", "DATABASE_URL": "postgres://ornek", "SIM_SEED": "abc"},
		{"CAMERA_API_KEY": "anahtar", "DATABASE_URL": "postgres://ornek", "SIM_EVENTS_PER_DAY": "0"},
		{"CAMERA_API_KEY": "anahtar", "DATABASE_URL": "postgres://ornek", "SIM_SPEED": "-1"},
		{"CAMERA_API_KEY": "anahtar", "DATABASE_URL": "postgres://ornek", "SIM_START": "yarin"},
	}
	for _, env := range bozuk {
		env := env
		if _, err := loadSettings(func(k string) string { return env[k] }); err == nil {
			t.Fatalf("bozuk ayar hata vermedi: %+v", env)
		}
	}
}

func TestDumpDeterministic(t *testing.T) {
	var a, b, c bytes.Buffer
	if kod := run([]string{"dump", "--seed", "7", "--events", "50", "--out", "-"}, nil, &a, io.Discard); kod != 0 {
		t.Fatalf("ilk dump kodu %d", kod)
	}
	if kod := run([]string{"dump", "--seed", "7", "--events", "50", "--out", "-"}, nil, &b, io.Discard); kod != 0 {
		t.Fatalf("ikinci dump kodu %d", kod)
	}
	if kod := run([]string{"dump", "--seed", "8", "--events", "50", "--out", "-"}, nil, &c, io.Discard); kod != 0 {
		t.Fatalf("farkli tohum dump kodu %d", kod)
	}
	if a.String() != b.String() {
		t.Fatal("ayni tohum ayni dump ciktisini vermedi")
	}
	if a.String() == c.String() {
		t.Fatal("farkli tohum ayni dump ciktisini verdi")
	}

	satirlar := strings.Split(strings.TrimSpace(a.String()), "\n")
	if len(satirlar) != 50 {
		t.Fatalf("beklenen 50 satir, bulunan %d", len(satirlar))
	}
	var rec dumpRecord
	if err := json.Unmarshal([]byte(satirlar[0]), &rec); err != nil {
		t.Fatalf("ilk satir JSON degil: %v", err)
	}
	if rec.CameraEventID == "" || rec.CameraID == "" || rec.LotID == 0 || rec.CapturedAt.IsZero() {
		t.Fatalf("eksik alan: %+v", rec)
	}
}

func TestDumpToFile(t *testing.T) {
	yol := filepath.Join(t.TempDir(), "olaylar.jsonl")
	var stderr bytes.Buffer
	if kod := run([]string{"dump", "--seed", "3", "--events", "10", "--out", yol}, nil, io.Discard, &stderr); kod != 0 {
		t.Fatalf("dump kodu %d: %s", kod, stderr.String())
	}
	veri, err := os.ReadFile(yol)
	if err != nil {
		t.Fatalf("cikti okunamadi: %v", err)
	}
	if satir := len(strings.Split(strings.TrimSpace(string(veri)), "\n")); satir != 10 {
		t.Fatalf("beklenen 10 satir, bulunan %d", satir)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	if kod := run([]string{"yok"}, nil, io.Discard, io.Discard); kod != 2 {
		t.Fatalf("bilinmeyen komut kodu %d", kod)
	}
}

func TestHealthcheck(t *testing.T) {
	if err := writeAlive(); err != nil {
		t.Fatalf("canlilik dosyasi yazilamadi: %v", err)
	}
	defer os.Remove(alivePath)

	if kod := runHealthcheck(); kod != 0 {
		t.Fatalf("taze dosya icin kod %d", kod)
	}
	eski := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(alivePath, eski, eski); err != nil {
		t.Fatalf("mtime degistirilemedi: %v", err)
	}
	if kod := runHealthcheck(); kod != 1 {
		t.Fatalf("eski dosya icin kod %d", kod)
	}
	if err := os.Remove(alivePath); err != nil {
		t.Fatalf("dosya silinemedi: %v", err)
	}
	if kod := runHealthcheck(); kod != 1 {
		t.Fatalf("olmayan dosya icin kod %d", kod)
	}
}

func sampleEvent() simulator.Event {
	return simulator.Event{
		LotID:         1,
		CameraID:      "CAM-KADIKOY-IN",
		CameraEventID: "sim-1-00000001",
		Direction:     simulator.Entry,
		PlateRaw:      "34ABC123",
		CapturedAt:    time.Date(2026, 10, 27, 6, 0, 0, 0, time.UTC),
		SendAt:        time.Date(2026, 10, 27, 6, 0, 1, 0, time.UTC),
	}
}

func testSender(t *testing.T, url string) (*sender, *counters) {
	t.Helper()
	c := &counters{}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	s := newSender(url, "gizli-kamera-anahtari", logger, c)
	s.backoff = time.Millisecond
	return s, c
}

func TestSenderSendsHeaderAndJSON(t *testing.T) {
	var gorulenBaslik string
	var gorulen cameraEventRequest
	sunucu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gorulenBaslik = r.Header.Get("X-Camera-Key")
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content type %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&gorulen); err != nil {
			t.Errorf("govde cozulemedi: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer sunucu.Close()

	s, c := testSender(t, sunucu.URL)
	if err := s.send(context.Background(), sampleEvent()); err != nil {
		t.Fatalf("gonderim hatasi: %v", err)
	}
	if gorulenBaslik != "gizli-kamera-anahtari" {
		t.Fatalf("kamera basligi %q", gorulenBaslik)
	}
	if gorulen.LotID != 1 || gorulen.CameraEventID != "sim-1-00000001" ||
		gorulen.Direction != simulator.Entry || gorulen.PlateRaw != "34ABC123" {
		t.Fatalf("govde yanlis: %+v", gorulen)
	}
	if c.created.Load() != 1 || c.failed.Load() != 0 {
		t.Fatalf("sayaclar yanlis: %+v", c)
	}
}

func TestSenderRetriesSameIDOn503(t *testing.T) {
	var deneme atomic.Int64
	kimlikler := make(chan string, 2)
	sunucu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req cameraEventRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		kimlikler <- req.CameraEventID
		if deneme.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer sunucu.Close()

	s, c := testSender(t, sunucu.URL)
	if err := s.send(context.Background(), sampleEvent()); err != nil {
		t.Fatalf("gonderim hatasi: %v", err)
	}
	if deneme.Load() != 2 {
		t.Fatalf("deneme sayisi %d", deneme.Load())
	}
	ilkKimlik := <-kimlikler
	ikinciKimlik := <-kimlikler
	if ilkKimlik != ikinciKimlik {
		t.Fatalf("yeniden denemede kimlik degisti: %q != %q", ilkKimlik, ikinciKimlik)
	}
	if c.created.Load() != 1 || c.failed.Load() != 0 {
		t.Fatalf("sayaclar yanlis: %+v", c)
	}
}

func TestSenderNoRetryOn4xx(t *testing.T) {
	var deneme atomic.Int64
	sunucu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deneme.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer sunucu.Close()

	s, c := testSender(t, sunucu.URL)
	if err := s.send(context.Background(), sampleEvent()); err != nil {
		t.Fatalf("4xx hata dondurmemeli: %v", err)
	}
	if deneme.Load() != 1 {
		t.Fatalf("4xx yeniden denendi: %d", deneme.Load())
	}
	if c.clientError.Load() != 1 {
		t.Fatalf("4xx sayaci %d", c.clientError.Load())
	}
}

func TestSenderCountsReplayAndConflict(t *testing.T) {
	for _, durum := range []int{http.StatusOK, http.StatusConflict} {
		sunucu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(durum)
		}))
		s, c := testSender(t, sunucu.URL)
		if err := s.send(context.Background(), sampleEvent()); err != nil {
			t.Fatalf("%d gonderim hatasi: %v", durum, err)
		}
		sunucu.Close()
		if durum == http.StatusOK && c.replayed.Load() != 1 {
			t.Fatalf("replay sayaci %d", c.replayed.Load())
		}
		if durum == http.StatusConflict && c.conflict.Load() != 1 {
			t.Fatalf("cakisma sayaci %d", c.conflict.Load())
		}
	}
}

func TestRunSenderMissingSettings(t *testing.T) {
	if kod := run(nil, func(string) string { return "" }, io.Discard, io.Discard); kod != 1 {
		t.Fatalf("eksik ayar kodu %d", kod)
	}
}

func TestRunSenderDatabaseError(t *testing.T) {
	env := map[string]string{
		"CAMERA_API_KEY": "anahtar",
		"DATABASE_URL":   "postgres://kullanici:parola@127.0.0.1:1/ornek?sslmode=disable&connect_timeout=1",
	}
	var cikti bytes.Buffer
	kod := run(nil, func(k string) string { return env[k] }, &cikti, io.Discard)
	if kod != 1 {
		t.Fatalf("veritabani hatasi kodu %d", kod)
	}
	if !strings.Contains(cikti.String(), "otopark listesi okunamadi") {
		t.Fatalf("hata logu yok: %s", cikti.String())
	}
}

func TestRunHealthcheckDispatch(t *testing.T) {
	if err := writeAlive(); err != nil {
		t.Fatalf("canlilik dosyasi yazilamadi: %v", err)
	}
	defer os.Remove(alivePath)
	if kod := run([]string{"healthcheck"}, nil, io.Discard, io.Discard); kod != 0 {
		t.Fatalf("healthcheck kodu %d", kod)
	}
}

func TestDumpErrors(t *testing.T) {
	if kod := run([]string{"dump", "--events", "-1"}, nil, io.Discard, io.Discard); kod != 2 {
		t.Fatalf("negatif events kodu %d", kod)
	}
	dizin := t.TempDir()
	if kod := run([]string{"dump", "--events", "1", "--out", dizin}, nil, io.Discard, io.Discard); kod != 1 {
		t.Fatalf("gecersiz cikis yolu kodu %d", kod)
	}
	var bos bytes.Buffer
	if kod := run([]string{"dump", "--events", "0"}, nil, &bos, io.Discard); kod != 0 {
		t.Fatalf("sifir olay kodu %d", kod)
	}
	if bos.Len() != 0 {
		t.Fatalf("sifir olay cikti uretti: %q", bos.String())
	}
}

func TestWaitUntil(t *testing.T) {
	if !waitUntil(context.Background(), time.Now().Add(-time.Second)) {
		t.Fatal("gecmis hedef true donmeli")
	}
	if !waitUntil(context.Background(), time.Now().Add(5*time.Millisecond)) {
		t.Fatal("kisa bekleme true donmeli")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitUntil(ctx, time.Now().Add(time.Hour)) {
		t.Fatal("iptal edilen baglam false donmeli")
	}
}

func TestBackgroundWorkersStopOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	bitir := make(chan struct{}, 2)
	go func() { keepAlive(ctx); bitir <- struct{}{} }()
	go func() {
		logSummary(ctx, slog.New(slog.NewJSONHandler(io.Discard, nil)), &counters{})
		bitir <- struct{}{}
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-bitir:
		case <-time.After(2 * time.Second):
			t.Fatal("arka plan isi iptal edilince durmadi")
		}
	}
}

func TestLogSummaryNow(t *testing.T) {
	var cikti bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&cikti, nil))
	c := &counters{}
	c.sent.Add(3)
	c.created.Add(2)
	logSummaryNow(logger, c)
	if !strings.Contains(cikti.String(), "simulator ozet") {
		t.Fatalf("ozet logu yok: %s", cikti.String())
	}
}

func TestSenderBadURL(t *testing.T) {
	s, c := testSender(t, "http://[::1")
	if err := s.send(context.Background(), sampleEvent()); err == nil {
		t.Fatal("gecersiz URL hata donmeli")
	}
	if c.failed.Load() != 1 {
		t.Fatalf("hata sayaci %d", c.failed.Load())
	}
}

func TestSenderGivesUpAfterMaxAttempts(t *testing.T) {
	var deneme atomic.Int64
	sunucu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deneme.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer sunucu.Close()

	s, c := testSender(t, sunucu.URL)
	s.maxAttempts = 3
	if err := s.send(context.Background(), sampleEvent()); err == nil {
		t.Fatal("tum denemeler basarisizsa hata donmeli")
	}
	if deneme.Load() != 3 {
		t.Fatalf("deneme sayisi %d", deneme.Load())
	}
	if c.failed.Load() != 1 {
		t.Fatalf("hata sayaci %d", c.failed.Load())
	}
}
