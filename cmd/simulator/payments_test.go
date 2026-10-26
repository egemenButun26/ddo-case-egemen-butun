package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testLoop(srvURL string) *kioskPaymentLoop {
	return &kioskPaymentLoop{
		apiURL:    srvURL,
		cameraKey: "test-kamera-anahtari",
		client:    &http.Client{Timeout: 5 * time.Second},
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		cooldown:  make(map[int64]time.Time),
	}
}

func TestKioskCooldown(t *testing.T) {
	l := testLoop("http://ornek")
	if l.beklemede(1) {
		t.Fatal("bos cooldown bekliyor gormedi")
	}
	l.isaretle(1)
	if !l.beklemede(1) {
		t.Fatal("isaretlenen oturum bekliyor degil")
	}
	if l.beklemede(2) {
		t.Fatal("baska oturum yanlislikla bekliyor")
	}
	l.cooldown[99] = time.Now().Add(-2 * time.Minute)
	l.isaretle(3)
	if l.beklemede(99) {
		t.Fatal("eski kayit temizlenmedi")
	}
}

func kioskSunucu(t *testing.T) (*httptest.Server, *int, *string) {
	t.Helper()
	var payDurum int
	var sonKey string
	var payCagri int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/kiosk/quote":
			if r.URL.Query().Get("lot_id") == "" || r.URL.Query().Get("plate") == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"session_id":42,"due_kurus":1500}`)
		case r.Method == http.MethodPost && r.URL.Path == "/kiosk/payments":
			payCagri++
			sonKey = r.Header.Get("Idempotency-Key")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(payDurum)
			fmt.Fprint(w, `{"payment_id":1,"session_id":42,"status":"pending"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return srv, &payDurum, &sonKey
}

func TestKioskOdeBasari(t *testing.T) {
	srv, durum, key := kioskSunucu(t)
	defer srv.Close()

	*durum = http.StatusCreated
	l := testLoop(srv.URL)
	if !l.ode(context.Background(), kioskCandidate{sessionID: 42, lotID: 1, plate: "34ABC123"}) {
		t.Fatal("201 yaniti basarili sayilmadi")
	}
	if *key == "" {
		t.Fatal("Idempotency-Key gonderilmedi")
	}

	*durum = http.StatusOK
	if !l.ode(context.Background(), kioskCandidate{sessionID: 42, lotID: 1, plate: "34ABC123"}) {
		t.Fatal("200 yaniti basarili sayilmadi")
	}
}

func TestKioskOdeHatalar(t *testing.T) {
	srv, durum, _ := kioskSunucu(t)
	defer srv.Close()
	l := testLoop(srv.URL)

	*durum = http.StatusConflict
	if l.ode(context.Background(), kioskCandidate{sessionID: 42, lotID: 1, plate: "34ABC123"}) {
		t.Fatal("409 yaniti basarili sayildi")
	}

	*durum = http.StatusInternalServerError
	if l.ode(context.Background(), kioskCandidate{sessionID: 42, lotID: 1, plate: "34ABC123"}) {
		t.Fatal("500 yaniti basarili sayildi")
	}
}

func TestKioskOdeTeklifYok(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/kiosk/quote":
			fmt.Fprint(w, `{"session_id":42,"due_kurus":0}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	l := testLoop(srv.URL)

	// due_kurus 0 -> odeme denenmemeli.
	if l.ode(context.Background(), kioskCandidate{sessionID: 42, lotID: 1, plate: "34ABC123"}) {
		t.Fatal("sifir tutar icin basari dondu")
	}
}

func TestKioskOdeIstekHatasi(t *testing.T) {
	l := testLoop("http://127.0.0.1:1")
	if l.ode(context.Background(), kioskCandidate{sessionID: 42, lotID: 1, plate: "34ABC123"}) {
		t.Fatal("ulasilamayan adres icin basari dondu")
	}
}

func TestRunKioskPaymentsKapanis(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	bitti := make(chan struct{})
	go func() {
		runKioskPayments(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)),
			"http://127.0.0.1:1", "anahtar", "postgres://127.0.0.1:1/db?sslmode=disable")
		close(bitti)
	}()
	select {
	case <-bitti:
	case <-time.After(10 * time.Second):
		t.Fatal("runKioskPayments kapanis sinyaline ragmen donmedi")
	}
}

func TestDrainAndClose(t *testing.T) {
	r := &countingReader{}
	drainAndClose(r)
	if r.n == 0 {
		t.Fatal("govde okunmadi")
	}
}

type countingReader struct {
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	if c.n > 0 {
		return 0, io.EOF
	}
	c.n = len(p)
	return len(p), nil
}

func (c *countingReader) Close() error { return nil }
