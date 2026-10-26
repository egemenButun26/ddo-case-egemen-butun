package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"parkakil/internal/store"
	"parkakil/internal/testdb"
)

func TestOpsBlacklistStore(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()

	entry, err := store.InsertBlacklist(ctx, pool, "34 abc 123", "  giris engeli  ", nil)
	if err != nil {
		t.Fatalf("kara liste kaydi eklenemedi: %v", err)
	}
	if entry.Plate != "34ABC123" {
		t.Fatalf("plaka = %q, beklenen 34ABC123", entry.Plate)
	}
	if entry.Reason != "giris engeli" {
		t.Fatalf("neden = %q, beklenen trimmed metin", entry.Reason)
	}
	if !entry.Active || entry.RemovedAt != nil || entry.CreatedBy != nil {
		t.Fatalf("yeni kayit alanlari yanlis: %+v", entry)
	}

	if _, err := store.InsertBlacklist(ctx, pool, "34 ABC 123", "ikinci", nil); !errors.Is(err, store.ErrBlacklistExists) {
		t.Fatalf("aktif tekrar hata = %v, beklenen ErrBlacklistExists", err)
	}
	if _, err := store.InsertBlacklist(ctx, pool, "---", "neden", nil); !errors.Is(err, store.ErrInvalidBlacklist) {
		t.Fatalf("bos plaka hata = %v, beklenen ErrInvalidBlacklist", err)
	}
	if _, err := store.InsertBlacklist(ctx, pool, "34ABC123", "   ", nil); !errors.Is(err, store.ErrInvalidBlacklist) {
		t.Fatalf("bos neden hata = %v, beklenen ErrInvalidBlacklist", err)
	}

	blacklisted, err := store.IsPlateBlacklisted(ctx, pool, "34ABC123")
	if err != nil || !blacklisted {
		t.Fatalf("aktif kara liste = %v, hata %v; beklenen true", blacklisted, err)
	}

	liste, err := store.ListBlacklist(ctx, pool, 0, 100)
	if err != nil {
		t.Fatalf("kara liste okunamadi: %v", err)
	}
	bulundu := false
	for _, e := range liste {
		if e.ID == entry.ID {
			bulundu = true
		}
	}
	if !bulundu {
		t.Fatalf("eklenen kayit listede yok")
	}

	if _, err := pool.Exec(ctx,
		`UPDATE blacklist SET active = false WHERE id = $1`, entry.ID); pgKodu(err) != "23514" {
		t.Fatalf("removed_at olmadan pasif hata = %v (kod %q), beklenen 23514", err, pgKodu(err))
	}

	removedAt := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	if err := store.DeactivateBlacklist(ctx, pool, entry.ID, removedAt); err != nil {
		t.Fatalf("kara liste kaydi kaldirilamadi: %v", err)
	}
	blacklisted, err = store.IsPlateBlacklisted(ctx, pool, "34ABC123")
	if err != nil || blacklisted {
		t.Fatalf("kaldirilmis kara liste = %v, hata %v; beklenen false", blacklisted, err)
	}
	if err := store.DeactivateBlacklist(ctx, pool, entry.ID, removedAt); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ikinci kaldirma hata = %v, beklenen ErrNotFound", err)
	}
	if err := store.DeactivateBlacklist(ctx, pool, 1<<60, removedAt); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("olmayan kayit kaldirma hata = %v, beklenen ErrNotFound", err)
	}

	yeni, err := store.InsertBlacklist(ctx, pool, "34abc123", "yeniden", nil)
	if err != nil {
		t.Fatalf("kaldirma sonrasi yeniden eklenemedi: %v", err)
	}
	if yeni.ID == entry.ID {
		t.Fatalf("yeni kayit eski kimligi kullandi: %d", yeni.ID)
	}

	var active, removed int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE active), count(*) FILTER (WHERE NOT active)
		 FROM blacklist WHERE plate = '34ABC123'`,
	).Scan(&active, &removed); err != nil {
		t.Fatalf("kara liste sayimi okunamadi: %v", err)
	}
	if active != 1 || removed != 1 {
		t.Fatalf("aktif/pasif = %d/%d, beklenen 1/1", active, removed)
	}
}
