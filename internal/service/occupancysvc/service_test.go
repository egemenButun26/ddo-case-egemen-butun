package occupancysvc_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"parkakil/internal/domain/occupancy"
	"parkakil/internal/domain/session"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/service/occupancysvc"
	"parkakil/internal/service/sessionsvc"
	"parkakil/internal/store"
	"parkakil/internal/testdb"
)

var sayac atomic.Int64

func benzersiz(onek string) string {
	return fmt.Sprintf("%s-%d-%d", onek, time.Now().UnixNano(), sayac.Add(1))
}

func standartTarife() tariff.Version {
	return tariff.Version{
		ID:        benzersiz("tarife"),
		ValidFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Location:  time.UTC,
		Weekday: tariff.DayRule{
			Name:  "hafta_ici",
			Bands: []tariff.Band{{UnitMinutes: 60, UnitPrice: 500}},
		},
	}
}

func hazirla(t *testing.T, pool *pgxpool.Pool, capacity int) (int64, *sessionsvc.Service) {
	t.Helper()
	lotID := testdb.NewLot(t, pool, capacity)
	if err := store.InsertTariffVersion(context.Background(), pool, lotID, standartTarife()); err != nil {
		t.Fatalf("tarife eklenemedi: %v", err)
	}
	svc, err := sessionsvc.New(pool, session.DefaultConfig)
	if err != nil {
		t.Fatalf("sessionsvc olusturulamadi: %v", err)
	}
	return lotID, svc
}

func giris(t *testing.T, svc *sessionsvc.Service, lotID int64, plaka string, at time.Time) sessionsvc.Result {
	t.Helper()
	res, err := svc.HandleEvent(context.Background(), session.Event{
		LotID:         lotID,
		CameraID:      benzersiz("cam-giris"),
		CameraEventID: benzersiz("olay"),
		Direction:     session.Entry,
		PlateRaw:      plaka,
		CapturedAt:    at,
	})
	if err != nil {
		t.Fatalf("%s girisi islenemedi: %v", plaka, err)
	}
	if res.Outcome != session.OutcomeOpened {
		t.Fatalf("%s girisi = %q, beklenen opened", plaka, res.Outcome)
	}
	return res
}

func cikis(t *testing.T, svc *sessionsvc.Service, lotID int64, plaka string, at time.Time) sessionsvc.Result {
	t.Helper()
	res, err := svc.HandleEvent(context.Background(), session.Event{
		LotID:         lotID,
		CameraID:      benzersiz("cam-cikis"),
		CameraEventID: benzersiz("olay"),
		Direction:     session.Exit,
		PlateRaw:      plaka,
		CapturedAt:    at,
	})
	if err != nil {
		t.Fatalf("%s cikisi islenemedi: %v", plaka, err)
	}
	if res.Outcome != session.OutcomeClosed {
		t.Fatalf("%s cikisi = %q, beklenen closed", plaka, res.Outcome)
	}
	return res
}

func plakalar(n int) []string {
	harfler := []byte("CFKPRWXY")
	rakamlar := []byte("02345689")
	out := make([]string, 0, n)
	for a := 0; a < 8 && len(out) < n; a++ {
		for b := 0; b < 8 && len(out) < n; b++ {
			out = append(out, fmt.Sprintf("34%c%c%c%c%c%c",
				harfler[a], harfler[b], harfler[(a+b)%8],
				rakamlar[a], rakamlar[b], rakamlar[(a+2*b)%8]))
		}
	}
	return out
}

func TestRezervasyonHatalari(t *testing.T) {
	pool := testdb.Open(t)
	svc, err := occupancysvc.New(pool)
	if err != nil {
		t.Fatalf("servis olusturulamadi: %v", err)
	}
	lotID, sessions := hazirla(t, pool, 2)
	at := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Reserve(context.Background(), lotID, "ABC", at); !errors.Is(err, occupancysvc.ErrInvalidPlate) {
		t.Fatalf("gecersiz plaka hata = %v, beklenen ErrInvalidPlate", err)
	}

	if _, err := svc.Reserve(context.Background(), lotID, "34ABC123", at); err != nil {
		t.Fatalf("rezervasyon eklenemedi: %v", err)
	}
	if _, err := svc.Reserve(context.Background(), lotID, "34 ABC 123", at); !errors.Is(err, occupancysvc.ErrReservationExists) {
		t.Fatalf("ayni plaka rezervasyonu hata = %v, beklenen ErrReservationExists", err)
	}

	giris(t, sessions, lotID, "34XYZ123", at)
	if _, err := svc.Reserve(context.Background(), lotID, "34XYZ123", at); !errors.Is(err, occupancysvc.ErrOpenSessionExists) {
		t.Fatalf("acik oturumlu plaka hata = %v, beklenen ErrOpenSessionExists", err)
	}

	if _, err := svc.Reserve(context.Background(), lotID, "34HHH123", at); !errors.Is(err, occupancysvc.ErrNoCapacity) {
		t.Fatalf("dolu otopark hata = %v, beklenen ErrNoCapacity", err)
	}

	if _, err := svc.Reserve(context.Background(), 1<<60, "34ABC123", at); !errors.Is(err, store.ErrLotNotFound) {
		t.Fatalf("olmayan lot hata = %v, beklenen ErrLotNotFound", err)
	}

	if _, err := occupancysvc.New(nil); err == nil {
		t.Fatalf("nil havuz kabul edildi")
	}
}

func TestEsZamanliRezervasyon(t *testing.T) {
	pool := testdb.Open(t)
	svc, err := occupancysvc.New(pool)
	if err != nil {
		t.Fatalf("servis olusturulamadi: %v", err)
	}
	lotID := testdb.NewLot(t, pool, 3)
	at := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	liste := plakalar(10)

	hatalar := make([]error, len(liste))
	var wg sync.WaitGroup
	bas := make(chan struct{})
	wg.Add(len(liste))
	for i := range liste {
		go func(i int) {
			defer wg.Done()
			<-bas
			_, hatalar[i] = svc.Reserve(context.Background(), lotID, liste[i], at)
		}(i)
	}
	close(bas)
	wg.Wait()

	basarili, kapasiteDolu := 0, 0
	for _, err := range hatalar {
		switch {
		case err == nil:
			basarili++
		case errors.Is(err, occupancysvc.ErrNoCapacity):
			kapasiteDolu++
		default:
			t.Fatalf("beklenmeyen hata: %v", err)
		}
	}
	if basarili != 3 || kapasiteDolu != 7 {
		t.Fatalf("basarili=%d kapasiteDolu=%d, beklenen 3/7", basarili, kapasiteDolu)
	}

	aktif, err := store.CountActiveReservations(context.Background(), pool, lotID)
	if err != nil {
		t.Fatalf("aktif rezervasyonlar sayilamadi: %v", err)
	}
	if aktif != 3 {
		t.Fatalf("aktif rezervasyon = %d, beklenen 3", aktif)
	}
}

func TestReleaseExpiredSinir(t *testing.T) {
	pool := testdb.Open(t)
	svc, err := occupancysvc.New(pool)
	if err != nil {
		t.Fatalf("servis olusturulamadi: %v", err)
	}
	lotID := testdb.NewLot(t, pool, 1)
	// ReleaseExpired tum lotlarda calisir; diger testlerin 2026 tarihli
	// rezervasyonlariyla karismamak icin bu test gecmis bir an kullanir.
	at := time.Date(2020, 1, 1, 10, 0, 0, 0, time.UTC)

	if _, err := svc.Reserve(context.Background(), lotID, "34ABC123", at); err != nil {
		t.Fatalf("rezervasyon eklenemedi: %v", err)
	}

	n, err := svc.ReleaseExpired(context.Background(), at.Add(30*time.Minute-time.Millisecond))
	if err != nil || n != 0 {
		t.Fatalf("29:59.999 serbest = %d, hata %v; beklenen 0", n, err)
	}
	if _, err := svc.Reserve(context.Background(), lotID, "34XYZ123", at); !errors.Is(err, occupancysvc.ErrNoCapacity) {
		t.Fatalf("serbest birakilmadan yeni rezervasyon hata = %v, beklenen ErrNoCapacity", err)
	}

	n, err = svc.ReleaseExpired(context.Background(), at.Add(30*time.Minute))
	if err != nil || n != 1 {
		t.Fatalf("30:00 serbest = %d, hata %v; beklenen 1", n, err)
	}

	if _, err := svc.Reserve(context.Background(), lotID, "34XYZ123", at.Add(30*time.Minute)); err != nil {
		t.Fatalf("serbest kalan yer icin rezervasyon eklenemedi: %v", err)
	}

	sonuclar := make([]int, 2)
	hatalar := make([]error, 2)
	var wg sync.WaitGroup
	bas := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-bas
			sonuclar[i], hatalar[i] = svc.ReleaseExpired(context.Background(), at.Add(2*time.Hour))
		}(i)
	}
	close(bas)
	wg.Wait()

	toplam := 0
	for i := range hatalar {
		if hatalar[i] != nil {
			t.Fatalf("eszamanli serbest birakma hatasi: %v", hatalar[i])
		}
		toplam += sonuclar[i]
	}
	if toplam != 1 {
		t.Fatalf("eszamanli serbest birakma toplami = %d, beklenen 1", toplam)
	}
}

func TestCancelVeOccupancy(t *testing.T) {
	pool := testdb.Open(t)
	svc, err := occupancysvc.New(pool)
	if err != nil {
		t.Fatalf("servis olusturulamadi: %v", err)
	}
	lotID, sessions := hazirla(t, pool, 5)
	at := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	liste := plakalar(3)

	r1, err := svc.Reserve(context.Background(), lotID, liste[0], at)
	if err != nil {
		t.Fatalf("ilk rezervasyon eklenemedi: %v", err)
	}
	if _, err := svc.Reserve(context.Background(), lotID, liste[1], at); err != nil {
		t.Fatalf("ikinci rezervasyon eklenemedi: %v", err)
	}
	giris(t, sessions, lotID, liste[2], at)

	snap, err := svc.Occupancy(context.Background(), lotID)
	if err != nil {
		t.Fatalf("doluluk okunamadi: %v", err)
	}
	beklenen := occupancysvc.Snapshot{Capacity: 5, Occupied: 1, ActiveReservations: 2, Available: 2}
	if snap != beklenen {
		t.Fatalf("doluluk = %+v, beklenen %+v", snap, beklenen)
	}

	if err := svc.Cancel(context.Background(), r1.ID, at.Add(time.Minute)); err != nil {
		t.Fatalf("rezervasyon iptal edilemedi: %v", err)
	}
	snap, err = svc.Occupancy(context.Background(), lotID)
	if err != nil {
		t.Fatalf("doluluk okunamadi: %v", err)
	}
	if snap.ActiveReservations != 1 || snap.Available != 3 {
		t.Fatalf("iptal sonrasi doluluk = %+v", snap)
	}

	if err := svc.Cancel(context.Background(), r1.ID, at.Add(2*time.Minute)); !errors.Is(err, store.ErrReservationNotActive) {
		t.Fatalf("ikinci iptal hata = %v, beklenen ErrReservationNotActive", err)
	}
	if err := svc.Cancel(context.Background(), 1<<60, at); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("olmayan rezervasyon iptali hata = %v, beklenen ErrNotFound", err)
	}
}

func TestRecomputeVeDrift(t *testing.T) {
	pool := testdb.Open(t)
	svc, err := occupancysvc.New(pool)
	if err != nil {
		t.Fatalf("servis olusturulamadi: %v", err)
	}
	lotID, sessions := hazirla(t, pool, 10)
	at := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	liste := plakalar(2)

	giris(t, sessions, lotID, liste[0], at)
	giris(t, sessions, lotID, liste[1], at.Add(time.Minute))
	cikis(t, sessions, lotID, liste[0], at.Add(2*time.Hour))

	stored, fromEvents, fromSessions, err := svc.Recompute(context.Background(), lotID)
	if err != nil {
		t.Fatalf("yeniden hesaplama basarisiz: %v", err)
	}
	if stored != 1 || fromEvents != 1 || fromSessions != 1 {
		t.Fatalf("normal akis recompute = %d/%d/%d, beklenen 1/1/1", stored, fromEvents, fromSessions)
	}

	if _, err := pool.Exec(context.Background(),
		`UPDATE lot_counters SET occupied = occupied + 7 WHERE lot_id = $1`, lotID); err != nil {
		t.Fatalf("sayac elle bozulamadi: %v", err)
	}

	d, err := svc.DetectAndFixDrift(context.Background(), lotID, at.Add(3*time.Hour))
	if err != nil {
		t.Fatalf("kayma tespiti basarisiz: %v", err)
	}
	if !d.Drifted || d.Stored != 8 || d.Recomputed != 1 || d.Delta != 7 {
		t.Fatalf("kayma sonucu yanlis: %+v", d)
	}
	if n, err := store.GetCounter(context.Background(), pool, lotID); err != nil || n != 1 {
		t.Fatalf("duzeltilmis sayac = %d, hata %v; beklenen 1", n, err)
	}

	var kayitSayisi int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM counter_drifts WHERE lot_id = $1`, lotID).Scan(&kayitSayisi); err != nil {
		t.Fatalf("kayma kayitlari okunamadi: %v", err)
	}
	if kayitSayisi != 1 {
		t.Fatalf("kayma kaydi = %d, beklenen 1", kayitSayisi)
	}
	var corrected bool
	if err := pool.QueryRow(context.Background(),
		`SELECT corrected FROM counter_drifts WHERE lot_id = $1`, lotID).Scan(&corrected); err != nil {
		t.Fatalf("corrected okunamadi: %v", err)
	}
	if !corrected {
		t.Fatalf("corrected = false, beklenen true")
	}

	d, err = svc.DetectAndFixDrift(context.Background(), lotID, at.Add(4*time.Hour))
	if err != nil {
		t.Fatalf("ikinci kayma tespiti basarisiz: %v", err)
	}
	if d.Drifted {
		t.Fatalf("ikinci tespit kayma buldu: %+v", d)
	}
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM counter_drifts WHERE lot_id = $1`, lotID).Scan(&kayitSayisi); err != nil {
		t.Fatalf("kayma kayitlari okunamadi: %v", err)
	}
	if kayitSayisi != 1 {
		t.Fatalf("ikinci cagri kayma kaydi yazdi: %d", kayitSayisi)
	}

	if _, err := svc.DetectAndFixDrift(context.Background(), 1<<60, at); !errors.Is(err, store.ErrLotNotFound) {
		t.Fatalf("olmayan lot kayma tespiti hata = %v, beklenen ErrLotNotFound", err)
	}
}

func TestRezervasyonGelinceArrived(t *testing.T) {
	pool := testdb.Open(t)
	svc, err := occupancysvc.New(pool)
	if err != nil {
		t.Fatalf("servis olusturulamadi: %v", err)
	}
	lotID, sessions := hazirla(t, pool, 2)
	at := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

	r, err := svc.Reserve(context.Background(), lotID, "34ABC123", at)
	if err != nil {
		t.Fatalf("rezervasyon eklenemedi: %v", err)
	}

	res := giris(t, sessions, lotID, "34ABC123", at.Add(5*time.Minute))

	var (
		status string
		sess   *int64
		arri   *time.Time
	)
	if err := pool.QueryRow(context.Background(),
		`SELECT status, session_id, arrived_at FROM reservations WHERE id = $1`, r.ID,
	).Scan(&status, &sess, &arri); err != nil {
		t.Fatalf("rezervasyon okunamadi: %v", err)
	}
	if status != string(occupancy.StatusArrived) || sess == nil || *sess != res.SessionID {
		t.Fatalf("rezervasyon arrived alanlari yanlis: %s %v", status, sess)
	}
	if arri == nil || !arri.UTC().Equal(at.Add(5*time.Minute)) {
		t.Fatalf("arrived_at yanlis: %v", arri)
	}

	snap, err := svc.Occupancy(context.Background(), lotID)
	if err != nil {
		t.Fatalf("doluluk okunamadi: %v", err)
	}
	if snap.Occupied != 1 || snap.ActiveReservations != 0 || snap.Available != 1 {
		t.Fatalf("giris sonrasi doluluk = %+v", snap)
	}
}
