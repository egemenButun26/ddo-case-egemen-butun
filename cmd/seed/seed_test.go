package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"parkakil/internal/auth"
	"parkakil/internal/store"
	"parkakil/internal/testdb"
)

const seedTestParola = "seed-test-parola"

func sayim(t *testing.T, pool *pgxpool.Pool, sorgu string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sorgu, args...).Scan(&n); err != nil {
		t.Fatalf("sayim basarisiz (%s): %v", sorgu, err)
	}
	return n
}

func TestSeedIdempotent(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()

	if err := seed(ctx, pool, seedTestParola); err != nil {
		t.Fatalf("ilk seed: %v", err)
	}
	users := sayim(t, pool, `SELECT count(*) FROM users`)
	lots := sayim(t, pool, `SELECT count(*) FROM lots`)
	tariffs := sayim(t, pool, `SELECT count(*) FROM tariff_versions`)
	plans := sayim(t, pool, `SELECT count(*) FROM plans`)
	subs := sayim(t, pool, `SELECT count(*) FROM subscriptions`)

	if err := seed(ctx, pool, seedTestParola); err != nil {
		t.Fatalf("ikinci seed: %v", err)
	}
	if got := sayim(t, pool, `SELECT count(*) FROM users`); got != users {
		t.Fatalf("kullanici sayisi degisti: %d -> %d", users, got)
	}
	if got := sayim(t, pool, `SELECT count(*) FROM lots`); got != lots {
		t.Fatalf("lot sayisi degisti: %d -> %d", lots, got)
	}
	if got := sayim(t, pool, `SELECT count(*) FROM tariff_versions`); got != tariffs {
		t.Fatalf("tarife sayisi degisti: %d -> %d", tariffs, got)
	}
	if got := sayim(t, pool, `SELECT count(*) FROM plans`); got != plans {
		t.Fatalf("plan sayisi degisti: %d -> %d", plans, got)
	}
	if got := sayim(t, pool, `SELECT count(*) FROM subscriptions`); got != subs {
		t.Fatalf("abonelik sayisi degisti: %d -> %d", subs, got)
	}
}

func TestSeedRolesAndLots(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()

	if err := seed(ctx, pool, seedTestParola); err != nil {
		t.Fatalf("seed: %v", err)
	}

	roller := map[auth.Role]int{}
	rows, err := pool.Query(ctx, `SELECT role, count(*) FROM users WHERE email LIKE '%@parkakil.test' GROUP BY role`)
	if err != nil {
		t.Fatalf("roller okunamadi: %v", err)
	}
	for rows.Next() {
		var role string
		var n int
		if err := rows.Scan(&role, &n); err != nil {
			t.Fatalf("rol satiri: %v", err)
		}
		roller[auth.Role(role)] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("rol listesi: %v", err)
	}
	for _, role := range []auth.Role{auth.RoleDriver, auth.RoleAttendant, auth.RoleAccountant, auth.RoleManager, auth.RoleAdmin} {
		if roller[role] != 1 {
			t.Fatalf("%s rolu icin kullanici sayisi = %d, beklenen 1", role, roller[role])
		}
	}

	beklenenLotlar := map[string]int{
		"KADIKOY":  120,
		"BESIKTAS": 80,
		"SISLI":    150,
		"USKUDAR":  60,
		"BAKIRKOY": 100,
	}
	for code, capacity := range beklenenLotlar {
		var got int
		if err := pool.QueryRow(ctx, `SELECT capacity FROM lots WHERE code = $1`, code).Scan(&got); err != nil {
			t.Fatalf("%s otoparki yok: %v", code, err)
		}
		if got != capacity {
			t.Fatalf("%s kapasitesi = %d, beklenen %d", code, got, capacity)
		}
	}

	if n := sayim(t, pool, `SELECT count(*) FROM lots WHERE code = ANY($1)`, []string{"KADIKOY", "BESIKTAS", "SISLI", "USKUDAR", "BAKIRKOY"}); n != 5 {
		t.Fatalf("demo otopark sayisi = %d, beklenen 5", n)
	}

	for _, l := range demoLots() {
		var lotID int64
		if err := pool.QueryRow(ctx, `SELECT id FROM lots WHERE code = $1`, l.Code).Scan(&lotID); err != nil {
			t.Fatalf("lot okunamadi: %v", err)
		}
		var sayi int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM tariff_versions WHERE lot_id = $1`, lotID).Scan(&sayi); err != nil {
			t.Fatalf("tarife sayisi: %v", err)
		}
		if sayi != 1 {
			t.Fatalf("%s icin tarife sayisi = %d, beklenen 1", l.Code, sayi)
		}
	}

	user, err := store.GetUserByEmail(ctx, pool, "surucu@parkakil.test")
	if err != nil {
		t.Fatalf("surucu bulunamadi: %v", err)
	}
	if user.Plate == nil || *user.Plate != "34ABC123" {
		t.Fatalf("surucu plakasi yanlis: %v", user.Plate)
	}
	if !auth.CheckPassword(user.PasswordHash, seedTestParola) {
		t.Fatal("demo parola ozeti dogrulanamadi")
	}

	for _, l := range demoLots() {
		var price int64
		var planID int64
		if err := pool.QueryRow(ctx,
			`SELECT id, monthly_price_kurus FROM plans WHERE code = $1`, l.Code+"-AYLIK",
		).Scan(&planID, &price); err != nil {
			t.Fatalf("%s plani yok: %v", l.Code, err)
		}
		if price != 150000 {
			t.Fatalf("%s plan fiyati = %d, beklenen 150000", l.Code, price)
		}
	}

	konum, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		t.Fatalf("Europe/Istanbul yuklenemedi: %v", err)
	}
	now := time.Now().In(konum)
	ayBasi := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, konum)
	var planCode string
	var periodStart time.Time
	var status string
	var subUserID *int64
	if err := pool.QueryRow(ctx,
		`SELECT p.code, s.current_period_start, s.status, s.user_id
		   FROM subscriptions s JOIN plans p ON p.id = s.plan_id
		  WHERE s.plate = $1 AND s.status IN ('active','past_due')
		  ORDER BY s.id DESC LIMIT 1`,
		"34ABC123",
	).Scan(&planCode, &periodStart, &status, &subUserID); err != nil {
		t.Fatalf("demo abonelik yok: %v", err)
	}
	if planCode != "KADIKOY-AYLIK" || status != "active" {
		t.Fatalf("demo abonelik yanlis: plan=%s durum=%s", planCode, status)
	}
	if !periodStart.Equal(time.Date(ayBasi.Year(), ayBasi.Month(), 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("demo abonelik donemi = %v, beklenen %v", periodStart, ayBasi)
	}
	if subUserID == nil || *subUserID != user.ID {
		t.Fatalf("demo abonelik sahibi yanlis: %v", subUserID)
	}
}

func TestSeedPasswordRequired(t *testing.T) {
	pool := testdb.Open(t)
	if err := seed(context.Background(), pool, ""); err == nil {
		t.Fatal("bos parola ile seed hata vermedi")
	}
	if kod := run(func(string) string { return "" }); kod != 1 {
		t.Fatalf("run cikis kodu = %d, beklenen 1", kod)
	}
}
