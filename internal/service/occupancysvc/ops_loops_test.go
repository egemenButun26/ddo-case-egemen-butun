package occupancysvc_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"parkakil/internal/service/occupancysvc"
	"parkakil/internal/store"
	"parkakil/internal/testdb"
)

func opsBekle(t *testing.T, kosul func() bool) {
	t.Helper()
	son := time.Now().Add(3 * time.Second)
	for time.Now().Before(son) {
		if kosul() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("kosul 3 sn icinde saglanmadi")
}

func TestOpsArkaPlanDonguleri(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	svc, err := occupancysvc.New(pool)
	if err != nil {
		t.Fatalf("servis olusturulamadi: %v", err)
	}

	lotID := testdb.NewLot(t, pool, 1)
	at := time.Date(2020, 1, 1, 10, 0, 0, 0, time.UTC)
	if _, err := svc.Reserve(ctx, lotID, "34BG001", at); err != nil {
		t.Fatalf("rezervasyon eklenemedi: %v", err)
	}

	releaseCtx, releaseCancel := context.WithCancel(context.Background())
	releaseDone := make(chan struct{})
	go func() {
		svc.RunReleaseLoop(releaseCtx, 10*time.Millisecond, logger)
		close(releaseDone)
	}()
	opsBekle(t, func() bool {
		n, err := store.CountActiveReservations(ctx, pool, lotID)
		return err == nil && n == 0
	})
	releaseCancel()
	select {
	case <-releaseDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("serbest birakma dongusu context iptalinde durmadi")
	}

	driftLot, sessions := hazirla(t, pool, 10)
	driftAt := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	giris(t, sessions, driftLot, "34BG002", driftAt)
	if err := store.SetCounter(ctx, pool, driftLot, 7); err != nil {
		t.Fatalf("sayac bozulamadi: %v", err)
	}

	driftCtx, driftCancel := context.WithCancel(context.Background())
	driftDone := make(chan struct{})
	go func() {
		svc.RunDriftLoop(driftCtx, 10*time.Millisecond, logger)
		close(driftDone)
	}()
	opsBekle(t, func() bool {
		n, err := store.GetCounter(ctx, pool, driftLot)
		return err == nil && n == 1
	})
	driftCancel()
	select {
	case <-driftDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("kayma dongusu context iptalinde durmadi")
	}

	var driftSayisi int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM "ya-counter_drifts" WHERE lot_id = $1`, driftLot).Scan(&driftSayisi); err != nil {
		t.Fatalf("kayma kayitlari okunamadi: %v", err)
	}
	if driftSayisi < 1 {
		t.Fatalf("kayma kaydi yazilmadi")
	}

	iptalCtx, iptalCancel := context.WithCancel(context.Background())
	iptalCancel()
	bitti := make(chan struct{})
	go func() {
		svc.RunReleaseLoop(iptalCtx, time.Hour, logger)
		close(bitti)
	}()
	select {
	case <-bitti:
	case <-time.After(time.Second):
		t.Fatalf("iptal edilmis context ile dongu hemen donmedi")
	}
}
