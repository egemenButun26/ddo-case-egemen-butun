// Package testdb yalnizca testlerin kullandigi veritabani yardimcisidir.
// Uretim kodu bu paketi import etmemelidir.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"

	"parkakil/internal/store"
	"parkakil/migrations"
)

var (
	migrateOnce sync.Once
	migrateErr  error
	lotSayac    atomic.Int64
)

// Open, TEST_DATABASE_URL ile havuz acar ve migrasyonlari bir kez uygular.
func Open(t testing.TB) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		if os.Getenv("PARKAKIL_REQUIRE_DB") == "1" {
			t.Fatalf("TEST_DATABASE_URL yok")
		}
		t.Skip("TEST_DATABASE_URL yok")
	}

	migrateOnce.Do(func() {
		migrateErr = migrate(url)
	})
	if migrateErr != nil {
		t.Fatalf("migrasyon uygulanamadi: %v", migrateErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("veritabani havuzu acilamadi: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("veritabani ping basarisiz: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// NewLot, benzersiz kodla yeni otopark olusturur.
func NewLot(t testing.TB, pool *pgxpool.Pool, capacity int) int64 {
	t.Helper()

	n := lotSayac.Add(1)
	kod := fmt.Sprintf("%s-%d-%d", sanitize(t.Name()), time.Now().UnixNano(), n)
	id, err := store.CreateLot(context.Background(), pool, kod, "test otoparki", capacity)
	if err != nil {
		t.Fatalf("lot olusturulamadi: %v", err)
	}
	return id
}

func migrate(url string) error {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return err
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}
	if err := goose.Up(db, "."); err != nil {
		return err
	}
	return nil
}

func sanitize(ad string) string {
	var b strings.Builder
	for _, r := range ad {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	s := b.String()
	if len(s) > 40 {
		s = s[:40]
	}
	if s == "" {
		s = "lot"
	}
	return s
}
