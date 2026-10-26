package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"parkakil/internal/domain/session"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/store"
	"parkakil/internal/testdb"
	"parkakil/migrations"
)

var sayac atomic.Int64

func benzersiz(onek string) string {
	return fmt.Sprintf("%s-%d-%d", onek, time.Now().UnixNano(), sayac.Add(1))
}

func tarifeSurumu(id string, from time.Time, to *time.Time) tariff.Version {
	v := tariff.Version{
		ID:        id,
		ValidFrom: from,
		Location:  time.UTC,
		Weekday: tariff.DayRule{
			Name:  "hafta_ici",
			Bands: []tariff.Band{{UnitMinutes: 60, UnitPrice: 500}},
		},
	}
	if to != nil {
		v.ValidTo = *to
	}
	return v
}

// lotHazirla yeni bir lot ve acik uclu tarife olusturur.
func lotHazirla(t *testing.T, pool *pgxpool.Pool) (int64, string) {
	t.Helper()
	lotID := testdb.NewLot(t, pool, 100)
	v := tarifeSurumu(benzersiz("tarife"), time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), nil)
	if err := store.InsertTariffVersion(context.Background(), pool, lotID, v); err != nil {
		t.Fatalf("tarife eklenemedi: %v", err)
	}
	return lotID, v.ID
}

func olayEkle(t *testing.T, pool *pgxpool.Pool, lotID int64, yon session.Direction, at time.Time) int64 {
	t.Helper()
	ev := session.Event{
		LotID:         lotID,
		CameraID:      "cam-giris",
		CameraEventID: benzersiz("olay"),
		Direction:     yon,
		PlateRaw:      "34ABC34",
		CapturedAt:    at,
	}
	id, err := store.InsertEvent(context.Background(), pool, ev, "34ABC34", session.OutcomeOpened, 0)
	if err != nil {
		t.Fatalf("olay eklenemedi: %v", err)
	}
	return id
}

func pgKodu(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func TestSchemaObjects(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()

	rows, err := pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public'`)
	if err != nil {
		t.Fatalf("tablolar okunamadi: %v", err)
	}
	defer rows.Close()

	tablolar := map[string]bool{}
	for rows.Next() {
		var ad string
		if err := rows.Scan(&ad); err != nil {
			t.Fatalf("tablo adi okunamadi: %v", err)
		}
		tablolar[ad] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("tablo listesi: %v", err)
	}

	for _, ad := range []string{"lots", "tariff_versions", "plate_events", "sessions", "review_items"} {
		if !tablolar[ad] {
			t.Fatalf("%s tablosu yok", ad)
		}
	}

	var indexSayisi int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'sessions_one_open_per_plate'`,
	).Scan(&indexSayisi); err != nil {
		t.Fatalf("dizin okunamadi: %v", err)
	}
	if indexSayisi != 1 {
		t.Fatalf("sessions_one_open_per_plate dizini yok (%d)", indexSayisi)
	}
}

func TestMigrationDownUp(t *testing.T) {
	testdb.Open(t)
	url := os.Getenv("TEST_DATABASE_URL")

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("baglanti acilamadi: %v", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose ayari: %v", err)
	}

	if err := goose.DownTo(db, ".", 1); err != nil {
		t.Fatalf("00002 down basarisiz: %v", err)
	}

	var varMi bool
	if err := db.QueryRow(`SELECT to_regclass('public.sessions') IS NOT NULL`).Scan(&varMi); err != nil {
		t.Fatalf("sessions kontrolu: %v", err)
	}
	if varMi {
		t.Fatal("down sonrasi sessions tablosu hala var")
	}

	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("00002 up basarisiz: %v", err)
	}

	if err := db.QueryRow(`SELECT to_regclass('public.sessions') IS NOT NULL`).Scan(&varMi); err != nil {
		t.Fatalf("sessions kontrolu: %v", err)
	}
	if !varMi {
		t.Fatal("up sonrasi sessions tablosu yok")
	}
	if err := db.QueryRow(`SELECT to_regclass('public.review_items') IS NOT NULL`).Scan(&varMi); err != nil {
		t.Fatalf("review_items kontrolu: %v", err)
	}
	if !varMi {
		t.Fatal("up sonrasi review_items tablosu yok")
	}
	var indexSayisi int
	if err := db.QueryRow(
		`SELECT count(*) FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'sessions_one_open_per_plate'`,
	).Scan(&indexSayisi); err != nil {
		t.Fatalf("dizin kontrolu: %v", err)
	}
	if indexSayisi != 1 {
		t.Fatalf("up sonrasi sessions_one_open_per_plate dizini yok (%d)", indexSayisi)
	}
}

func TestLockLotNotFound(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()

	if err := store.LockLot(ctx, pool, 1<<60); !errors.Is(err, store.ErrLotNotFound) {
		t.Fatalf("olmayan lot icin hata = %v, beklenen ErrLotNotFound", err)
	}

	lotID := testdb.NewLot(t, pool, 10)
	if err := store.LockLot(ctx, pool, lotID); err != nil {
		t.Fatalf("var olan lot kilitlenemedi: %v", err)
	}
}

func TestLockLotKarsilikliDislar(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID := testdb.NewLot(t, pool, 10)

	tx1, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("ilk islem acilamadi: %v", err)
	}
	defer func() { _ = tx1.Rollback(ctx) }()
	if err := store.LockLot(ctx, tx1, lotID); err != nil {
		t.Fatalf("ilk kilit alinamadi: %v", err)
	}

	kazanildi := make(chan error, 1)
	basladi := make(chan struct{})
	go func() {
		tx2, err := pool.Begin(ctx)
		if err != nil {
			kazanildi <- err
			return
		}
		defer func() { _ = tx2.Rollback(ctx) }()
		close(basladi)
		kazanildi <- store.LockLot(ctx, tx2, lotID)
	}()

	<-basladi
	select {
	case err := <-kazanildi:
		t.Fatalf("ikinci kilit birinci commit etmeden alindi: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	if err := tx1.Commit(ctx); err != nil {
		t.Fatalf("ilk islem commit edilemedi: %v", err)
	}
	select {
	case err := <-kazanildi:
		if err != nil {
			t.Fatalf("commit sonrasi kilit alinamadi: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("ikinci kilit commit sonrasi 5 sn icinde alinamadi")
	}
}

func TestTariffVersionAtBoundaries(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID := testdb.NewLot(t, pool, 100)

	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(30 * 24 * time.Hour)
	v := tarifeSurumu(benzersiz("tarife"), t0, &t1)
	if err := store.InsertTariffVersion(ctx, pool, lotID, v); err != nil {
		t.Fatalf("tarife eklenemedi: %v", err)
	}

	got, err := store.TariffVersionAt(ctx, pool, lotID, t0)
	if err != nil {
		t.Fatalf("valid_from aninda tarife bulunamadi: %v", err)
	}
	if got.ID != v.ID {
		t.Fatalf("tarife ID = %q, beklenen %q", got.ID, v.ID)
	}

	if _, err := store.TariffVersionAt(ctx, pool, lotID, t1); !errors.Is(err, store.ErrNoTariff) {
		t.Fatalf("valid_to aninda hata = %v, beklenen ErrNoTariff", err)
	}
	if _, err := store.TariffVersionAt(ctx, pool, lotID, t0.Add(-time.Second)); !errors.Is(err, store.ErrNoTariff) {
		t.Fatalf("valid_from oncesinde hata = %v, beklenen ErrNoTariff", err)
	}

	okunan, err := store.GetTariffVersion(ctx, pool, v.ID)
	if err != nil {
		t.Fatalf("GetTariffVersion: %v", err)
	}
	if !okunan.ValidFrom.Equal(v.ValidFrom) || !okunan.ValidTo.Equal(v.ValidTo) {
		t.Fatalf("gecerlilik okunamadi: %v - %v", okunan.ValidFrom, okunan.ValidTo)
	}
	giris := t0.Add(50 * time.Hour)
	cikis := giris.Add(3 * time.Hour)
	q1, err := tariff.Calculate(v, giris, cikis)
	if err != nil {
		t.Fatalf("hesap: %v", err)
	}
	q2, err := tariff.Calculate(okunan, giris, cikis)
	if err != nil {
		t.Fatalf("hesap: %v", err)
	}
	if q1.Total != q2.Total {
		t.Fatalf("gidis-donus toplam %d, beklenen %d", q2.Total, q1.Total)
	}

	if _, err := store.GetTariffVersion(ctx, pool, benzersiz("yok")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("olmayan tarife hatasi = %v, beklenen ErrNotFound", err)
	}

	gap := tarifeSurumu(benzersiz("tarife"), t1.Add(time.Hour), nil)
	if err := store.InsertTariffVersion(ctx, pool, lotID, gap); err != nil {
		t.Fatalf("bosluk sonrasi tarife eklenemedi: %v", err)
	}
	if _, err := store.TariffVersionAt(ctx, pool, lotID, t1.Add(30*time.Minute)); !errors.Is(err, store.ErrNoTariff) {
		t.Fatalf("boslukta hata = %v, beklenen ErrNoTariff", err)
	}

	bozuk := tarifeSurumu(benzersiz("tarife"), t0, nil)
	bozuk.Weekday.Bands = nil
	if err := store.InsertTariffVersion(ctx, pool, lotID, bozuk); !errors.Is(err, tariff.ErrInvalidTariff) {
		t.Fatalf("gecersiz tarife hatasi = %v, beklenen ErrInvalidTariff", err)
	}
}

func TestTariffOverlap(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID := testdb.NewLot(t, pool, 100)

	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	v1 := tarifeSurumu(benzersiz("tarife"), t0, nil)
	if err := store.InsertTariffVersion(ctx, pool, lotID, v1); err != nil {
		t.Fatalf("ilk tarife eklenemedi: %v", err)
	}

	v2 := tarifeSurumu(benzersiz("tarife"), t0.Add(time.Hour), nil)
	if err := store.InsertTariffVersion(ctx, pool, lotID, v2); !errors.Is(err, store.ErrTariffOverlap) {
		t.Fatalf("cakisan tarife hatasi = %v, beklenen ErrTariffOverlap", err)
	}

	digerLot := testdb.NewLot(t, pool, 100)
	if err := store.InsertTariffVersion(ctx, pool, digerLot, v2); err != nil {
		t.Fatalf("farkli lot'ta ayni aralik reddedildi: %v", err)
	}
}

func TestEventCameraKeyUnique(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID := testdb.NewLot(t, pool, 100)

	kamera := benzersiz("cam")
	kameraOlay := benzersiz("kam-olay")
	at := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	ev := session.Event{
		LotID:         lotID,
		CameraID:      kamera,
		CameraEventID: kameraOlay,
		Direction:     session.Entry,
		PlateRaw:      "34ABC34",
		CapturedAt:    at,
	}
	id, err := store.InsertEvent(ctx, pool, ev, "34ABC34", session.OutcomeOpened, 0)
	if err != nil {
		t.Fatalf("olay eklenemedi: %v", err)
	}

	okunan, bulundu, err := store.FindEventByCameraKey(ctx, pool, kamera, kameraOlay)
	if err != nil {
		t.Fatalf("olay aranamadi: %v", err)
	}
	if !bulundu {
		t.Fatal("olay bulunamadi")
	}
	if okunan.ID != id || okunan.PlateNorm != "34ABC34" || okunan.Direction != session.Entry ||
		okunan.Outcome != session.OutcomeOpened || okunan.SessionID != 0 || !okunan.CapturedAt.Equal(at) {
		t.Fatalf("olay alanlari yanlis: %+v", okunan)
	}

	if _, bulundu, err := store.FindEventByCameraKey(ctx, pool, kamera, benzersiz("yok")); err != nil || bulundu {
		t.Fatalf("olmayan olay: bulundu=%v hata=%v", bulundu, err)
	}

	if _, err := store.InsertEvent(ctx, pool, ev, "34ABC34", session.OutcomeOpened, 0); !errors.Is(err, store.ErrEventConflict) {
		t.Fatalf("ayni kamera olayi ikinci kez eklendi: %v", err)
	}

	if err := store.SetEventOutcome(ctx, pool, id, session.OutcomeReview, 0); err != nil {
		t.Fatalf("olay sonucu guncellenemedi: %v", err)
	}
	okunan, _, err = store.FindEventByCameraKey(ctx, pool, kamera, kameraOlay)
	if err != nil {
		t.Fatalf("olay aranamadi: %v", err)
	}
	if okunan.Outcome != session.OutcomeReview {
		t.Fatalf("sonuc = %q, beklenen %q", okunan.Outcome, session.OutcomeReview)
	}
	if err := store.SetEventOutcome(ctx, pool, 1<<60, session.OutcomeReview, 0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("olmayan olay guncellemesi hata = %v, beklenen ErrNotFound", err)
	}
}

func TestOpenSessionUniquePerPlate(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID, tarifeID := lotHazirla(t, pool)
	lot2, tarife2 := lotHazirla(t, pool)

	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	giris1 := olayEkle(t, pool, lotID, session.Entry, t0)
	id1, err := store.InsertSession(ctx, pool, lotID, "34ABC34", giris1, t0, tarifeID)
	if err != nil {
		t.Fatalf("oturum acilamadi: %v", err)
	}

	giris2 := olayEkle(t, pool, lotID, session.Entry, t0.Add(time.Minute))
	if _, err := store.InsertSession(ctx, pool, lotID, "34ABC34", giris2, t0.Add(time.Minute), tarifeID); !errors.Is(err, store.ErrOpenSessionExists) {
		t.Fatalf("ikinci acik oturum hatasi = %v, beklenen ErrOpenSessionExists", err)
	}

	giris3 := olayEkle(t, pool, lot2, session.Entry, t0)
	if _, err := store.InsertSession(ctx, pool, lot2, "34ABC34", giris3, t0, tarife2); err != nil {
		t.Fatalf("farkli lot'ta ayni plaka reddedildi: %v", err)
	}

	cikis1 := olayEkle(t, pool, lotID, session.Exit, t0.Add(2*time.Hour))
	if err := store.CloseSession(ctx, pool, id1, cikis1, t0.Add(2*time.Hour), 1000, nil); err != nil {
		t.Fatalf("oturum kapatilamadi: %v", err)
	}

	giris4 := olayEkle(t, pool, lotID, session.Entry, t0.Add(3*time.Hour))
	if _, err := store.InsertSession(ctx, pool, lotID, "34ABC34", giris4, t0.Add(3*time.Hour), tarifeID); err != nil {
		t.Fatalf("kapanistan sonra yeni oturum acilamadi: %v", err)
	}

	n, err := store.CountOpenSessions(ctx, pool, lotID, "34ABC34")
	if err != nil {
		t.Fatalf("acik oturum sayilamadi: %v", err)
	}
	if n != 1 {
		t.Fatalf("acik oturum sayisi = %d, beklenen 1", n)
	}
}

func TestCloseSession(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID, tarifeID := lotHazirla(t, pool)

	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	giris := olayEkle(t, pool, lotID, session.Entry, t0)
	id, err := store.InsertSession(ctx, pool, lotID, "06XYZ06", giris, t0, tarifeID)
	if err != nil {
		t.Fatalf("oturum acilamadi: %v", err)
	}

	cikis := olayEkle(t, pool, lotID, session.Exit, t0.Add(90*time.Minute))
	quote := []byte(`{"total_kurus":1500}`)
	if err := store.CloseSession(ctx, pool, id, cikis, t0.Add(90*time.Minute), 1500, quote); err != nil {
		t.Fatalf("oturum kapatilamadi: %v", err)
	}

	s, err := store.GetSession(ctx, pool, id)
	if err != nil {
		t.Fatalf("oturum okunamadi: %v", err)
	}
	if s.Status != "closed" {
		t.Fatalf("durum = %q, beklenen closed", s.Status)
	}
	if s.ExitEventID != cikis {
		t.Fatalf("cikis olayi = %d, beklenen %d", s.ExitEventID, cikis)
	}
	if s.ExitedAt == nil || !s.ExitedAt.Equal(t0.Add(90*time.Minute)) {
		t.Fatalf("exited_at yanlis: %v", s.ExitedAt)
	}
	if s.AmountKurus == nil || *s.AmountKurus != tariff.Kurus(1500) {
		t.Fatalf("tutar yanlis: %v", s.AmountKurus)
	}

	if err := store.CloseSession(ctx, pool, id, cikis, t0.Add(90*time.Minute), 1500, nil); !errors.Is(err, store.ErrSessionNotOpen) {
		t.Fatalf("ikinci kapatma hatasi = %v, beklenen ErrSessionNotOpen", err)
	}

	giris2 := olayEkle(t, pool, lotID, session.Entry, t0.Add(2*time.Hour))
	id2, err := store.InsertSession(ctx, pool, lotID, "06XYZ07", giris2, t0.Add(2*time.Hour), tarifeID)
	if err != nil {
		t.Fatalf("ikinci oturum acilamadi: %v", err)
	}
	cikis2 := olayEkle(t, pool, lotID, session.Exit, t0.Add(2*time.Hour))
	if err := store.CloseSession(ctx, pool, id2, cikis2, t0.Add(time.Hour), 500, nil); pgKodu(err) != "23514" {
		t.Fatalf("cikis girişten once hata = %v (kod %q), beklenen 23514", err, pgKodu(err))
	}
}

func TestListOpenAndRecentClosed(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID, tarifeID := lotHazirla(t, pool)

	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	plakalar := []string{"34AAA01", "34AAA02", "34AAA03"}
	idler := make([]int64, len(plakalar))
	for i, p := range plakalar {
		giris := olayEkle(t, pool, lotID, session.Entry, t0)
		id, err := store.InsertSession(ctx, pool, lotID, p, giris, t0, tarifeID)
		if err != nil {
			t.Fatalf("%s oturumu acilamadi: %v", p, err)
		}
		idler[i] = id
	}

	acik, err := store.ListOpenSessions(ctx, pool, lotID)
	if err != nil {
		t.Fatalf("acik oturumlar okunamadi: %v", err)
	}
	if len(acik) != len(plakalar) {
		t.Fatalf("acik oturum sayisi = %d, beklenen %d", len(acik), len(plakalar))
	}
	for i, s := range acik {
		if s.ID != idler[i] || s.Plate != plakalar[i] {
			t.Fatalf("acik oturum %d = %d/%s, beklenen %d/%s", i, s.ID, s.Plate, idler[i], plakalar[i])
		}
	}

	around := t0.Add(time.Hour)
	window := 10 * time.Minute
	cikislar := []time.Time{around, around.Add(-window), around.Add(-window - time.Second)}
	for i, cikisAt := range cikislar {
		cikis := olayEkle(t, pool, lotID, session.Exit, cikisAt)
		if err := store.CloseSession(ctx, pool, idler[i], cikis, cikisAt, 100, nil); err != nil {
			t.Fatalf("oturum %d kapatilamadi: %v", i, err)
		}
	}

	yakin, err := store.ListRecentClosed(ctx, pool, lotID, around, window)
	if err != nil {
		t.Fatalf("yakin kapananlar okunamadi: %v", err)
	}
	if len(yakin) != 2 {
		t.Fatalf("pencere ici kapanan sayisi = %d, beklenen 2", len(yakin))
	}
	if yakin[0].ID != idler[0] || yakin[1].ID != idler[1] {
		t.Fatalf("pencere sirasi yanlis: %+v", yakin)
	}
	if !yakin[1].ExitedAt.Equal(around.Add(-window)) {
		t.Fatalf("pencere siniri dahil degil: %v", yakin[1].ExitedAt)
	}

	acik, err = store.ListOpenSessions(ctx, pool, lotID)
	if err != nil {
		t.Fatalf("acik oturumlar okunamadi: %v", err)
	}
	if len(acik) != 0 {
		t.Fatalf("kapandiktan sonra acik oturum sayisi = %d", len(acik))
	}
}

func TestListClosedCovering(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID, tarifeID := lotHazirla(t, pool)

	at := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	window := 5 * time.Minute

	ekle := func(plaka string, entered, exited time.Time) int64 {
		t.Helper()
		giris := olayEkle(t, pool, lotID, session.Entry, entered)
		id, err := store.InsertSession(ctx, pool, lotID, plaka, giris, entered, tarifeID)
		if err != nil {
			t.Fatalf("%s oturumu acilamadi: %v", plaka, err)
		}
		cikis := olayEkle(t, pool, lotID, session.Exit, exited)
		if err := store.CloseSession(ctx, pool, id, cikis, exited, 100, nil); err != nil {
			t.Fatalf("%s oturumu kapatilamadi: %v", plaka, err)
		}
		return id
	}

	idA := ekle("34AAA01", at.Add(-time.Hour), at.Add(30*time.Minute))
	idB := ekle("34AAA02", at.Add(window), at.Add(30*time.Minute))
	ekle("34AAA03", at.Add(window+time.Second), at.Add(30*time.Minute))
	idD := ekle("34AAA04", at.Add(-time.Hour), at)
	ekle("34AAA05", at.Add(-time.Hour), at.Add(-time.Second))

	girisAcik := olayEkle(t, pool, lotID, session.Entry, at.Add(-time.Hour))
	if _, err := store.InsertSession(ctx, pool, lotID, "34AAA06", girisAcik, at.Add(-time.Hour), tarifeID); err != nil {
		t.Fatalf("acik oturum acilamadi: %v", err)
	}

	kapsayan, err := store.ListClosedCovering(ctx, pool, lotID, at, window)
	if err != nil {
		t.Fatalf("kapsayan kapali oturumlar okunamadi: %v", err)
	}
	if len(kapsayan) != 3 {
		t.Fatalf("kapsayan oturum sayisi = %d, beklenen 3: %+v", len(kapsayan), kapsayan)
	}
	beklenen := []int64{idA, idB, idD}
	for i, s := range kapsayan {
		if s.ID != beklenen[i] {
			t.Fatalf("kapsayan oturum %d = %d, beklenen %d", i, s.ID, beklenen[i])
		}
	}
	if !kapsayan[0].EnteredAt.Equal(at.Add(-time.Hour)) || !kapsayan[0].ExitedAt.Equal(at.Add(30*time.Minute)) {
		t.Fatalf("kapali oturum alanlari yanlis: %+v", kapsayan[0])
	}
	if !kapsayan[1].EnteredAt.Equal(at.Add(window)) || !kapsayan[2].ExitedAt.Equal(at) {
		t.Fatalf("sinir oturumlari yanlis: %+v / %+v", kapsayan[1], kapsayan[2])
	}
}

func TestListOrphanExits(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID := testdb.NewLot(t, pool, 100)

	since := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	oncesi := olayEkle(t, pool, lotID, session.Exit, since.Add(-time.Second))
	if err := store.SetEventOutcome(ctx, pool, oncesi, session.OutcomeOrphanExit, 0); err != nil {
		t.Fatalf("sonuc guncellenemedi: %v", err)
	}
	tam := olayEkle(t, pool, lotID, session.Exit, since)
	if err := store.SetEventOutcome(ctx, pool, tam, session.OutcomeOrphanExit, 0); err != nil {
		t.Fatalf("sonuc guncellenemedi: %v", err)
	}
	sonra := olayEkle(t, pool, lotID, session.Exit, since.Add(2*time.Second))
	if err := store.SetEventOutcome(ctx, pool, sonra, session.OutcomeOrphanExit, 0); err != nil {
		t.Fatalf("sonuc guncellenemedi: %v", err)
	}

	bekleyen, err := store.ListOrphanExits(ctx, pool, lotID, since)
	if err != nil {
		t.Fatalf("bekleyen cikislar okunamadi: %v", err)
	}
	if len(bekleyen) != 2 {
		t.Fatalf("bekleyen cikis sayisi = %d, beklenen 2", len(bekleyen))
	}
	if bekleyen[0].EventID != tam || bekleyen[1].EventID != sonra {
		t.Fatalf("bekleyen cikis sirasi yanlis: %+v", bekleyen)
	}
	if !bekleyen[0].CapturedAt.Equal(since) || bekleyen[0].Plate != "34ABC34" {
		t.Fatalf("bekleyen cikis alanlari yanlis: %+v", bekleyen[0])
	}

	bos, err := store.ListOrphanExits(ctx, pool, lotID, since.Add(time.Hour))
	if err != nil {
		t.Fatalf("bekleyen cikislar okunamadi: %v", err)
	}
	if len(bos) != 0 {
		t.Fatalf("since sonrasi bekleyen cikis sayisi = %d, beklenen 0", len(bos))
	}
}

func TestReviewInsert(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID, tarifeID := lotHazirla(t, pool)

	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	giris := olayEkle(t, pool, lotID, session.Entry, t0)
	sid, err := store.InsertSession(ctx, pool, lotID, "34ABC34", giris, t0, tarifeID)
	if err != nil {
		t.Fatalf("oturum acilamadi: %v", err)
	}

	olay1 := olayEkle(t, pool, lotID, session.Entry, t0.Add(time.Minute))
	id1, err := store.InsertReview(ctx, pool, olay1, lotID, "34ABC34", session.Review{Reason: session.ReasonLowConfidenceReview})
	if err != nil {
		t.Fatalf("inceleme eklenemedi: %v", err)
	}
	var suggested, runner *int64
	var confidence int
	if err := pool.QueryRow(ctx,
		`SELECT suggested_session_id, runner_up_session_id, confidence FROM review_items WHERE id = $1`, id1,
	).Scan(&suggested, &runner, &confidence); err != nil {
		t.Fatalf("inceleme okunamadi: %v", err)
	}
	if suggested != nil || runner != nil {
		t.Fatalf("bos oneriler NULL degil: %v / %v", suggested, runner)
	}
	if confidence != 0 {
		t.Fatalf("confidence = %d, beklenen 0", confidence)
	}

	olay2 := olayEkle(t, pool, lotID, session.Entry, t0.Add(2*time.Minute))
	id2, err := store.InsertReview(ctx, pool, olay2, lotID, "34ABC35", session.Review{
		Reason:             session.ReasonAmbiguous,
		Confidence:         850,
		SuggestedSessionID: sid,
		RunnerUpSessionID:  sid,
		RunnerUpConfidence: 700,
	})
	if err != nil {
		t.Fatalf("inceleme eklenemedi: %v", err)
	}
	var s2, r2 *int64
	var c2, rc2 int
	if err := pool.QueryRow(ctx,
		`SELECT suggested_session_id, runner_up_session_id, confidence, runner_up_confidence
		 FROM review_items WHERE id = $1`, id2,
	).Scan(&s2, &r2, &c2, &rc2); err != nil {
		t.Fatalf("inceleme okunamadi: %v", err)
	}
	if s2 == nil || *s2 != sid || r2 == nil || *r2 != sid || c2 != 850 || rc2 != 700 {
		t.Fatalf("inceleme alanlari yanlis: %v %v %d %d", s2, r2, c2, rc2)
	}

	olay3 := olayEkle(t, pool, lotID, session.Entry, t0.Add(3*time.Minute))
	if _, err := store.InsertReview(ctx, pool, olay3, lotID, "34ABC36", session.Review{
		Reason:     session.ReasonAmbiguous,
		Confidence: 1001,
	}); pgKodu(err) != "23514" {
		t.Fatalf("confidence 1001 hata = %v (kod %q), beklenen 23514", err, pgKodu(err))
	}
}
