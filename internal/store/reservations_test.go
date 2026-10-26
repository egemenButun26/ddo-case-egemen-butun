package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"parkakil/internal/domain/occupancy"
	"parkakil/internal/domain/session"
	"parkakil/internal/store"
	"parkakil/internal/testdb"
)

func TestReservationSchema(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID := testdb.NewLot(t, pool, 10)
	at := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

	r, err := store.InsertReservation(ctx, pool, lotID, "34ABC123", at, at.Add(30*time.Minute))
	if err != nil {
		t.Fatalf("rezervasyon eklenemedi: %v", err)
	}
	if r.ID == 0 || r.LotID != lotID || r.Plate != "34ABC123" || r.Status != occupancy.StatusActive {
		t.Fatalf("rezervasyon alanlari yanlis: %+v", r)
	}
	if !r.ReservedAt.Equal(at) || !r.ExpiresAt.Equal(at.Add(30*time.Minute)) {
		t.Fatalf("rezervasyon zamani yanlis: %+v", r)
	}

	if _, err := store.InsertReservation(ctx, pool, lotID, "34ABC123", at, at.Add(30*time.Minute)); !errors.Is(err, store.ErrReservationExists) {
		t.Fatalf("ikinci aktif rezervasyon hatasi = %v, beklenen ErrReservationExists", err)
	}

	if _, err := store.InsertReservation(ctx, pool, lotID, "06ZZ9999", at, at.Add(29*time.Minute)); pgKodu(err) != "23514" {
		t.Fatalf("29 dakikalik rezervasyon hata = %v (kod %q), beklenen 23514", err, pgKodu(err))
	}

	if _, err := store.InsertReservation(ctx, pool, lotID, "06ZZ9999", at, at.Add(30*time.Minute)); err != nil {
		t.Fatalf("ikinci plaka rezervasyonu eklenemedi: %v", err)
	}

	if err := store.CancelReservation(ctx, pool, r.ID, at.Add(time.Minute)); err != nil {
		t.Fatalf("rezervasyon iptal edilemedi: %v", err)
	}
	if err := store.CancelReservation(ctx, pool, r.ID, at.Add(2*time.Minute)); !errors.Is(err, store.ErrReservationNotActive) {
		t.Fatalf("ikinci iptal hata = %v, beklenen ErrReservationNotActive", err)
	}

	if _, err := store.InsertReservation(ctx, pool, lotID, "34ABC123", at.Add(3*time.Minute), at.Add(33*time.Minute)); err != nil {
		t.Fatalf("iptal sonrasi yeniden rezervasyon eklenemedi: %v", err)
	}

	aktif, err := store.ListActiveReservations(ctx, pool, lotID)
	if err != nil {
		t.Fatalf("aktif rezervasyonlar okunamadi: %v", err)
	}
	if len(aktif) != 2 {
		t.Fatalf("aktif rezervasyon sayisi = %d, beklenen 2", len(aktif))
	}
	n, err := store.CountActiveReservations(ctx, pool, lotID)
	if err != nil || n != 2 {
		t.Fatalf("aktif rezervasyon sayimi = %d, hata %v", n, err)
	}

	if err := store.CancelReservation(ctx, pool, 1<<60, at); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("olmayan rezervasyon iptali hata = %v, beklenen ErrNotFound", err)
	}
}

func TestMarkReservationArrived(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID, tarifeID := lotHazirla(t, pool)
	at := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

	r, err := store.InsertReservation(ctx, pool, lotID, "34ABC123", at, at.Add(30*time.Minute))
	if err != nil {
		t.Fatalf("rezervasyon eklenemedi: %v", err)
	}

	giris := olayEkle(t, pool, lotID, session.Entry, at)
	sessionID, err := store.InsertSession(ctx, pool, lotID, "34ABC123", giris, at, tarifeID)
	if err != nil {
		t.Fatalf("oturum acilamadi: %v", err)
	}

	okundu, err := store.MarkReservationArrivedByPlate(ctx, pool, lotID, "34ABC123", sessionID, at)
	if err != nil {
		t.Fatalf("rezervasyon arrived yapilamadi: %v", err)
	}
	if !okundu {
		t.Fatalf("aktif rezervasyon bulunamadi")
	}

	var (
		status string
		sess   *int64
		arrive *time.Time
	)
	if err := pool.QueryRow(ctx,
		`SELECT status, session_id, arrived_at FROM "ya-reservations" WHERE id = $1`, r.ID,
	).Scan(&status, &sess, &arrive); err != nil {
		t.Fatalf("rezervasyon okunamadi: %v", err)
	}
	if status != string(occupancy.StatusArrived) || sess == nil || *sess != sessionID {
		t.Fatalf("rezervasyon arrived alanlari yanlis: %s %v", status, sess)
	}
	if arrive == nil || !arrive.UTC().Equal(at) {
		t.Fatalf("arrived_at yanlis: %v", arrive)
	}

	tekrar, err := store.MarkReservationArrivedByPlate(ctx, pool, lotID, "34ABC123", sessionID, at)
	if err != nil || tekrar {
		t.Fatalf("ikinci arrived = %v, hata %v; beklenen false", tekrar, err)
	}
}

func TestReleaseExpiredReservations(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID := testdb.NewLot(t, pool, 10)
	// ReleaseExpiredReservations tum lotlarda calisir; diger testlerin 2026
	// tarihli rezervasyonlariyla karismamak icin bu test gecmis bir an kullanir.
	at := time.Date(2020, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := store.InsertReservation(ctx, pool, lotID, "34ABC123", at, at.Add(30*time.Minute)); err != nil {
		t.Fatalf("rezervasyon eklenemedi: %v", err)
	}

	n, err := store.ReleaseExpiredReservations(ctx, pool, at.Add(29*time.Minute+59*time.Second))
	if err != nil || n != 0 {
		t.Fatalf("29dk59s serbest birakma = %d, hata %v; beklenen 0", n, err)
	}
	n, err = store.ReleaseExpiredReservations(ctx, pool, at.Add(30*time.Minute))
	if err != nil || n != 1 {
		t.Fatalf("30dk serbest birakma = %d, hata %v; beklenen 1", n, err)
	}
	n, err = store.ReleaseExpiredReservations(ctx, pool, at.Add(31*time.Minute))
	if err != nil || n != 0 {
		t.Fatalf("tekrar serbest birakma = %d, hata %v; beklenen 0", n, err)
	}
}

func TestCounterDriftsAppendOnly(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID := testdb.NewLot(t, pool, 10)
	at := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

	if _, err := store.InsertCounterDrift(ctx, pool, lotID, 7, 2, at, true); err != nil {
		t.Fatalf("kayma kaydi eklenemedi: %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE "ya-counter_drifts" SET corrected = false WHERE lot_id = $1`, lotID); err == nil {
		t.Fatalf("counter_drifts UPDATE kabul edildi")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM "ya-counter_drifts" WHERE lot_id = $1`, lotID); err == nil {
		t.Fatalf("counter_drifts DELETE kabul edildi")
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM "ya-counter_drifts" WHERE lot_id = $1`, lotID).Scan(&n); err != nil {
		t.Fatalf("kayma sayisi okunamadi: %v", err)
	}
	if n != 1 {
		t.Fatalf("kayma kaydi sayisi = %d, beklenen 1", n)
	}
}

func TestLotCounterSchema(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID := testdb.NewLot(t, pool, 10)

	if err := store.IncrementCounter(ctx, pool, lotID); err != nil {
		t.Fatalf("sayac arttirilamadi: %v", err)
	}
	if err := store.IncrementCounter(ctx, pool, lotID); err != nil {
		t.Fatalf("sayac arttirilamadi: %v", err)
	}
	if n, err := store.GetCounter(ctx, pool, lotID); err != nil || n != 2 {
		t.Fatalf("sayac = %d, hata %v; beklenen 2", n, err)
	}
	if err := store.DecrementCounter(ctx, pool, lotID); err != nil {
		t.Fatalf("sayac azaltilamadi: %v", err)
	}
	if n, err := store.GetCounter(ctx, pool, lotID); err != nil || n != 1 {
		t.Fatalf("sayac = %d, hata %v; beklenen 1", n, err)
	}

	if _, err := pool.Exec(ctx, `UPDATE "ya-lot_counters" SET occupied = -1 WHERE lot_id = $1`, lotID); pgKodu(err) != "23514" {
		t.Fatalf("negatif sayac hata = %v (kod %q), beklenen 23514", err, pgKodu(err))
	}

	olmayan := testdb.NewLot(t, pool, 10)
	if n, err := store.GetCounter(ctx, pool, olmayan); err != nil || n != 0 {
		t.Fatalf("satirsiz lot sayaci = %d, hata %v; beklenen 0", n, err)
	}
	if err := store.SetCounter(ctx, pool, olmayan, 4); err != nil {
		t.Fatalf("sayac yazilamadi: %v", err)
	}
	if n, err := store.GetCounter(ctx, pool, olmayan); err != nil || n != 4 {
		t.Fatalf("yazilan sayac = %d, hata %v; beklenen 4", n, err)
	}
}
