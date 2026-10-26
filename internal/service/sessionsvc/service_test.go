package sessionsvc_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"parkakil/internal/domain/plate"
	"parkakil/internal/domain/session"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/service/sessionsvc"
	"parkakil/internal/store"
	"parkakil/internal/testdb"
)

var sayac atomic.Int64

func benzersiz(onek string) string {
	return fmt.Sprintf("%s-%d-%d", onek, time.Now().UnixNano(), sayac.Add(1))
}

func istanbul(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		t.Fatalf("Europe/Istanbul yuklenemedi: %v", err)
	}
	return loc
}

func an(t *testing.T, s string) time.Time {
	t.Helper()
	x, err := time.ParseInLocation("2006-01-02 15:04:05", s, istanbul(t))
	if err != nil {
		t.Fatalf("%q cozulemedi: %v", s, err)
	}
	return x
}

func tarifeSurumu(t *testing.T, id string, from time.Time, to *time.Time, carpan tariff.Kurus) tariff.Version {
	t.Helper()
	v := tariff.Version{
		ID:          id,
		ValidFrom:   from,
		Location:    istanbul(t),
		FreeMinutes: 15,
		Weekday: tariff.DayRule{
			Name: "hafta_ici",
			Bands: []tariff.Band{
				{UpToMinutes: 60, UnitMinutes: 15, UnitPrice: 1000 * carpan},
				{UpToMinutes: 0, UnitMinutes: 60, UnitPrice: 3000 * carpan},
			},
			Night:    &tariff.Night{StartMinute: 1320, EndMinute: 420, UnitMinutes: 60, UnitPrice: 1500 * carpan},
			DailyCap: 25000 * carpan,
		},
	}
	if to != nil {
		v.ValidTo = *to
	}
	return v
}

func standartTarife(t *testing.T) tariff.Version {
	t.Helper()
	return tarifeSurumu(t, benzersiz("tarife"), an(t, "2026-01-01 00:00:00"), nil, 1)
}

func erkenTarife(t *testing.T) tariff.Version {
	t.Helper()
	return tarifeSurumu(t, benzersiz("tarife"), an(t, "2024-01-01 00:00:00"), nil, 1)
}

func yeniServis(t *testing.T) (*pgxpool.Pool, *sessionsvc.Service) {
	t.Helper()
	pool := testdb.Open(t)
	svc, err := sessionsvc.New(pool, session.DefaultConfig)
	if err != nil {
		t.Fatalf("servis olusturulamadi: %v", err)
	}
	return pool, svc
}

func hazirlaStandart(t *testing.T) (*pgxpool.Pool, *sessionsvc.Service, int64, tariff.Version) {
	t.Helper()
	pool, svc := yeniServis(t)
	lotID := testdb.NewLot(t, pool, 100)
	v := standartTarife(t)
	if err := store.InsertTariffVersion(context.Background(), pool, lotID, v); err != nil {
		t.Fatalf("tarife eklenemedi: %v", err)
	}
	return pool, svc, lotID, v
}

func yeniLotTarifeli(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	lotID := testdb.NewLot(t, pool, 100)
	if err := store.InsertTariffVersion(context.Background(), pool, lotID, standartTarife(t)); err != nil {
		t.Fatalf("tarife eklenemedi: %v", err)
	}
	return lotID
}

func olay(lotID int64, kamera string, yon session.Direction, plaka string, at time.Time) session.Event {
	return session.Event{
		LotID:         lotID,
		CameraID:      kamera,
		CameraEventID: benzersiz("olay"),
		Direction:     yon,
		PlateRaw:      plaka,
		CapturedAt:    at,
	}
}

func esZamanli(t *testing.T, n int, f func(int) (sessionsvc.Result, error)) []sessionsvc.Result {
	t.Helper()
	sonuc := make([]sessionsvc.Result, n)
	hatalar := make([]error, n)
	var wg sync.WaitGroup
	bas := make(chan struct{})
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-bas
			sonuc[i], hatalar[i] = f(i)
		}(i)
	}
	close(bas)
	wg.Wait()
	for i, err := range hatalar {
		if err != nil {
			t.Fatalf("goroutine %d hata: %v", i, err)
		}
	}
	return sonuc
}

func say(t *testing.T, pool *pgxpool.Pool, sorgu string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sorgu, args...).Scan(&n); err != nil {
		t.Fatalf("sayim basarisiz: %v", err)
	}
	return n
}

type incelemeSatiri struct {
	Reason      string
	Confidence  int
	SuggestedID *int64
}

func incelemeOku(t *testing.T, pool *pgxpool.Pool, eventID int64) incelemeSatiri {
	t.Helper()
	var s incelemeSatiri
	if err := pool.QueryRow(context.Background(),
		`SELECT reason, confidence, suggested_session_id FROM "ya-review_items" WHERE event_id = $1`,
		eventID,
	).Scan(&s.Reason, &s.Confidence, &s.SuggestedID); err != nil {
		t.Fatalf("inceleme okunamadi: %v", err)
	}
	return s
}

func hazirlaKapasite(t *testing.T, capacity int) (*pgxpool.Pool, *sessionsvc.Service, int64, tariff.Version) {
	t.Helper()
	pool, svc := yeniServis(t)
	lotID := testdb.NewLot(t, pool, capacity)
	v := standartTarife(t)
	if err := store.InsertTariffVersion(context.Background(), pool, lotID, v); err != nil {
		t.Fatalf("tarife eklenemedi: %v", err)
	}
	return pool, svc, lotID, v
}

func kapasitePlakalari(n int) []string {
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

func TestKapasiteDolu(t *testing.T) {
	pool, svc, lotID, _ := hazirlaKapasite(t, 2)
	at := an(t, "2026-10-27 10:00:00")

	ilk, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34AAA001", at))
	if err != nil {
		t.Fatalf("ilk giris islenemedi: %v", err)
	}
	if ilk.Outcome != session.OutcomeOpened {
		t.Fatalf("ilk giris = %q, beklenen opened", ilk.Outcome)
	}
	ikinci, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34BBB002", at.Add(time.Minute)))
	if err != nil {
		t.Fatalf("ikinci giris islenemedi: %v", err)
	}
	if ikinci.Outcome != session.OutcomeOpened {
		t.Fatalf("ikinci giris = %q, beklenen opened", ikinci.Outcome)
	}

	ucuncu, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34CCC003", at.Add(2*time.Minute)))
	if err != nil {
		t.Fatalf("ucuncu giris islenemedi: %v", err)
	}
	if ucuncu.Outcome != session.OutcomeReview {
		t.Fatalf("ucuncu giris = %q, beklenen review", ucuncu.Outcome)
	}
	if r := incelemeOku(t, pool, ucuncu.EventID); r.Reason != session.ReasonCapacityFull {
		t.Fatalf("inceleme nedeni = %q, beklenen %q", r.Reason, session.ReasonCapacityFull)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1`, lotID); n != 2 {
		t.Fatalf("oturum sayisi = %d, beklenen 2", n)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1 AND status = 'open'`, lotID); n != 2 {
		t.Fatalf("acik oturum sayisi = %d, beklenen 2", n)
	}

	tekrar, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34AAA001", at.Add(3*time.Minute)))
	if err != nil {
		t.Fatalf("tekrar giris islenemedi: %v", err)
	}
	if tekrar.Outcome != session.OutcomeDuplicateEntry {
		t.Fatalf("dolu otoparkta tekrar giris = %q, beklenen duplicate_entry", tekrar.Outcome)
	}

	cikis, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-cikis", session.Exit, "34AAA001", at.Add(30*time.Minute)))
	if err != nil {
		t.Fatalf("cikis islenemedi: %v", err)
	}
	if cikis.Outcome != session.OutcomeClosed {
		t.Fatalf("cikis = %q, beklenen closed", cikis.Outcome)
	}

	sonra, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34CCC003", at.Add(40*time.Minute)))
	if err != nil {
		t.Fatalf("cikis sonrasi giris islenemedi: %v", err)
	}
	if sonra.Outcome != session.OutcomeOpened {
		t.Fatalf("cikis sonrasi giris = %q, beklenen opened", sonra.Outcome)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1 AND status = 'open'`, lotID); n != 2 {
		t.Fatalf("acik oturum sayisi = %d, beklenen 2", n)
	}
}

func TestEsZamanli50GirisKapasite(t *testing.T) {
	pool, svc, lotID, _ := hazirlaKapasite(t, 10)
	at := an(t, "2026-10-27 10:00:00")
	plakalar := kapasitePlakalari(50)
	if len(plakalar) != 50 {
		t.Fatalf("plaka sayisi = %d, beklenen 50", len(plakalar))
	}
	for i := 0; i < len(plakalar); i++ {
		if !plate.ValidTR(plakalar[i]) {
			t.Fatalf("%q gecerli TR plakasi degil", plakalar[i])
		}
		for j := i + 1; j < len(plakalar); j++ {
			if plate.Confidence(plakalar[i], plakalar[j]) >= plate.DefaultThresholds.Review {
				t.Fatalf("%q/%q cifti inceleme esiginin ustunde", plakalar[i], plakalar[j])
			}
			if plate.ConfusionKey(plakalar[i]) == plate.ConfusionKey(plakalar[j]) {
				t.Fatalf("%q/%q ayni karisiklik anahtarinda", plakalar[i], plakalar[j])
			}
		}
	}

	sonuclar := esZamanli(t, 50, func(i int) (sessionsvc.Result, error) {
		return svc.HandleEvent(context.Background(),
			olay(lotID, "cam-giris", session.Entry, plakalar[i], at))
	})

	acilan := 0
	inceleme := 0
	for _, r := range sonuclar {
		switch r.Outcome {
		case session.OutcomeOpened:
			acilan++
		case session.OutcomeReview:
			inceleme++
		default:
			t.Fatalf("beklenmeyen sonuc: %q", r.Outcome)
		}
	}
	if acilan != 10 {
		t.Fatalf("opened sayisi = %d, beklenen 10", acilan)
	}
	if inceleme != 40 {
		t.Fatalf("review sayisi = %d, beklenen 40", inceleme)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1 AND status = 'open'`, lotID); n != 10 {
		t.Fatalf("acik oturum sayisi = %d, beklenen 10", n)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-review_items" WHERE lot_id = $1 AND reason = $2`,
		lotID, session.ReasonCapacityFull); n != 40 {
		t.Fatalf("capacity_full inceleme sayisi = %d, beklenen 40", n)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-plate_events" WHERE lot_id = $1`, lotID); n != 50 {
		t.Fatalf("olay sayisi = %d, beklenen 50", n)
	}
}

func TestGecGirisKapaliOturumOncesi(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	giris := an(t, "2026-10-27 10:00:00")
	cikis := an(t, "2026-10-27 12:00:00")

	if _, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam1", session.Entry, "34ABC123", giris)); err != nil {
		t.Fatalf("giris islenemedi: %v", err)
	}
	kapanis, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam1", session.Exit, "34ABC123", cikis))
	if err != nil {
		t.Fatalf("cikis islenemedi: %v", err)
	}
	if kapanis.Outcome != session.OutcomeClosed {
		t.Fatalf("cikis sonucu = %q, beklenen closed", kapanis.Outcome)
	}

	gecSonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam2", session.Entry, "34ABC123", an(t, "2026-10-27 08:00:00")))
	if err != nil {
		t.Fatalf("gec giris islenemedi: %v", err)
	}
	if gecSonuc.Outcome != session.OutcomeReview {
		t.Fatalf("gec giris sonucu = %q, beklenen review", gecSonuc.Outcome)
	}
	r := incelemeOku(t, pool, gecSonuc.EventID)
	if r.Reason != session.ReasonLateEntry {
		t.Fatalf("gec giris nedeni = %q, beklenen %q", r.Reason, session.ReasonLateEntry)
	}
	if r.SuggestedID == nil || *r.SuggestedID != kapanis.SessionID {
		t.Fatalf("onerilen oturum = %v, beklenen %d", r.SuggestedID, kapanis.SessionID)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1`, lotID); n != 1 {
		t.Fatalf("oturum sayisi = %d, beklenen 1", n)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1 AND status = 'open'`, lotID); n != 0 {
		t.Fatalf("acik oturum sayisi = %d, beklenen 0", n)
	}

	sonra, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam3", session.Entry, "34ABC123", an(t, "2026-10-27 12:30:00")))
	if err != nil {
		t.Fatalf("sonraki giris islenemedi: %v", err)
	}
	if sonra.Outcome != session.OutcomeOpened {
		t.Fatalf("sonraki giris sonucu = %q, beklenen opened", sonra.Outcome)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1`, lotID); n != 2 {
		t.Fatalf("oturum sayisi = %d, beklenen 2", n)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1 AND status = 'open'`, lotID); n != 1 {
		t.Fatalf("acik oturum sayisi = %d, beklenen 1", n)
	}
}

func TestKarisiklikAnahtariGiris(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	at := an(t, "2026-10-27 10:00:00")

	ilk, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34HHH123", at))
	if err != nil {
		t.Fatalf("ilk giris islenemedi: %v", err)
	}
	if ilk.Outcome != session.OutcomeOpened {
		t.Fatalf("ilk giris = %q, beklenen opened", ilk.Outcome)
	}

	ikinci, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34MMM123", at.Add(time.Minute)))
	if err != nil {
		t.Fatalf("karisiklik giris islenemedi: %v", err)
	}
	if ikinci.Outcome != session.OutcomeReview {
		t.Fatalf("karisiklik giris = %q, beklenen review", ikinci.Outcome)
	}
	r := incelemeOku(t, pool, ikinci.EventID)
	if r.Reason != session.ReasonPossibleDuplicateEntry {
		t.Fatalf("inceleme nedeni = %q, beklenen %q", r.Reason, session.ReasonPossibleDuplicateEntry)
	}
	if r.SuggestedID == nil || *r.SuggestedID != ilk.SessionID {
		t.Fatalf("onerilen oturum = %v, beklenen %d", r.SuggestedID, ilk.SessionID)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1`, lotID); n != 1 {
		t.Fatalf("oturum sayisi = %d, beklenen 1", n)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1 AND status = 'open'`, lotID); n != 1 {
		t.Fatalf("acik oturum sayisi = %d, beklenen 1", n)
	}
}

func TestEsZamanli50GirisAyniPlaka(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	at := an(t, "2026-10-27 10:00:00")

	sonuclar := esZamanli(t, 50, func(i int) (sessionsvc.Result, error) {
		ev := olay(lotID, "cam-giris", session.Entry, "34ABC123", at)
		ev.CameraEventID = fmt.Sprintf("giris-%d", i)
		return svc.HandleEvent(context.Background(), ev)
	})

	acik, err := store.CountOpenSessions(context.Background(), pool, lotID, "34ABC123")
	if err != nil {
		t.Fatalf("acik oturum sayilamadi: %v", err)
	}
	if acik != 1 {
		t.Fatalf("acik oturum sayisi = %d, beklenen 1", acik)
	}

	acilan := 0
	var acikID int64
	for _, r := range sonuclar {
		switch r.Outcome {
		case session.OutcomeOpened:
			acilan++
			acikID = r.SessionID
		case session.OutcomeDuplicateEntry:
			if r.SessionID == 0 {
				t.Fatalf("duplicate_entry oturum kimligi bos")
			}
		default:
			t.Fatalf("beklenmeyen sonuc: %q", r.Outcome)
		}
	}
	if acilan != 1 {
		t.Fatalf("opened sayisi = %d, beklenen 1", acilan)
	}
	if len(sonuclar)-acilan != 49 {
		t.Fatalf("duplicate_entry sayisi = %d, beklenen 49", len(sonuclar)-acilan)
	}
	for _, r := range sonuclar {
		if r.Outcome == session.OutcomeDuplicateEntry && r.SessionID != acikID {
			t.Fatalf("duplicate_entry SessionID = %d, acik oturum %d", r.SessionID, acikID)
		}
	}

	if n := say(t, pool, `SELECT count(*) FROM "ya-plate_events" WHERE lot_id = $1`, lotID); n != 50 {
		t.Fatalf("plate_events satir sayisi = %d, beklenen 50", n)
	}
}

func TestEsZamanli50GirisKarisikOkuma(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	at := an(t, "2026-10-27 10:00:00")
	okumalar := []string{"34ABU123", "34ABV123"}

	esZamanli(t, 50, func(i int) (sessionsvc.Result, error) {
		ev := olay(lotID, "cam-giris", session.Entry, okumalar[i%len(okumalar)], at)
		ev.CameraEventID = fmt.Sprintf("karisik-%d", i)
		return svc.HandleEvent(context.Background(), ev)
	})

	if n := say(t, pool,
		`SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1 AND status = 'open'`, lotID); n != 1 {
		t.Fatalf("acik oturum sayisi = %d, beklenen 1", n)
	}
}

func TestAyniKameraOlayiTekrar(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	at := an(t, "2026-10-27 10:00:00")
	ev := session.Event{
		LotID:         lotID,
		CameraID:      "cam-tekrar",
		CameraEventID: "olay-tekrar",
		Direction:     session.Entry,
		PlateRaw:      "34ABC123",
		CapturedAt:    at,
	}

	sonuclar := esZamanli(t, 50, func(i int) (sessionsvc.Result, error) {
		return svc.HandleEvent(context.Background(), ev)
	})

	if n := say(t, pool, `SELECT count(*) FROM "ya-plate_events" WHERE lot_id = $1`, lotID); n != 1 {
		t.Fatalf("plate_events satir sayisi = %d, beklenen 1", n)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1`, lotID); n != 1 {
		t.Fatalf("oturum sayisi = %d, beklenen 1", n)
	}

	tekrar := 0
	for _, r := range sonuclar {
		if r.Replayed {
			tekrar++
		}
		if r.EventID != sonuclar[0].EventID || r.Outcome != sonuclar[0].Outcome || r.SessionID != sonuclar[0].SessionID {
			t.Fatalf("tekrar sonuclari farkli: %+v / %+v", r, sonuclar[0])
		}
	}
	if tekrar != 49 {
		t.Fatalf("replayed sayisi = %d, beklenen 49", tekrar)
	}
}

func TestSirasizCikisOnceGelir(t *testing.T) {
	pool, svc, lotID, v := hazirlaStandart(t)
	giris := an(t, "2026-10-27 10:00:00")
	cikis := an(t, "2026-10-27 10:40:00")

	cikisOlay := olay(lotID, "cam-cikis", session.Exit, "34ABC123", cikis)
	cikisSonuc, err := svc.HandleEvent(context.Background(), cikisOlay)
	if err != nil {
		t.Fatalf("cikis islenemedi: %v", err)
	}
	if cikisSonuc.Outcome != session.OutcomeOrphanExit {
		t.Fatalf("cikis sonucu = %q, beklenen orphan_exit", cikisSonuc.Outcome)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1`, lotID); n != 0 {
		t.Fatalf("acik oturum sayisi = %d, beklenen 0", n)
	}

	girisSonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34ABC123", giris))
	if err != nil {
		t.Fatalf("giris islenemedi: %v", err)
	}
	if girisSonuc.Outcome != session.OutcomeOpened {
		t.Fatalf("giris sonucu = %q, beklenen opened", girisSonuc.Outcome)
	}
	if !girisSonuc.ClosedByOrphanExit {
		t.Fatalf("ClosedByOrphanExit false")
	}
	beklenen, err := tariff.Calculate(v, giris, cikis)
	if err != nil {
		t.Fatalf("beklenen tutar hesaplanamadi: %v", err)
	}
	if beklenen.Total != 3000 {
		t.Fatalf("beklenen tutar = %d, beklenen 3000", beklenen.Total)
	}
	if girisSonuc.AmountKurus == nil || *girisSonuc.AmountKurus != beklenen.Total {
		t.Fatalf("sonuc tutari yanlis: %v", girisSonuc.AmountKurus)
	}

	s, err := store.GetSession(context.Background(), pool, girisSonuc.SessionID)
	if err != nil {
		t.Fatalf("oturum okunamadi: %v", err)
	}
	if s.Status != "closed" {
		t.Fatalf("oturum durumu = %q, beklenen closed", s.Status)
	}
	if s.ExitedAt == nil || !s.ExitedAt.Equal(cikis) {
		t.Fatalf("exited_at = %v, beklenen %v", s.ExitedAt, cikis)
	}
	if s.AmountKurus == nil || *s.AmountKurus != beklenen.Total {
		t.Fatalf("oturum tutari yanlis: %v", s.AmountKurus)
	}
	if s.ExitEventID != cikisSonuc.EventID {
		t.Fatalf("cikis olayi = %d, beklenen %d", s.ExitEventID, cikisSonuc.EventID)
	}

	cikisKayit, bulundu, err := store.FindEventByCameraKey(context.Background(), pool, cikisOlay.CameraID, cikisOlay.CameraEventID)
	if err != nil {
		t.Fatalf("cikis olayi aranamadi: %v", err)
	}
	if !bulundu {
		t.Fatalf("cikis olayi bulunamadi")
	}
	if cikisKayit.Outcome != session.OutcomeClosed {
		t.Fatalf("cikis olayi sonucu = %q, beklenen closed", cikisKayit.Outcome)
	}
	if cikisKayit.SessionID != girisSonuc.SessionID {
		t.Fatalf("cikis olayi oturumu = %d, beklenen %d", cikisKayit.SessionID, girisSonuc.SessionID)
	}
}

func TestCikisGiristenOnce(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	giris := an(t, "2026-10-27 10:00:00")
	cikis := an(t, "2026-10-27 09:50:00")

	if _, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34ABC123", giris)); err != nil {
		t.Fatalf("giris islenemedi: %v", err)
	}

	sonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-cikis", session.Exit, "34ABC123", cikis))
	if err != nil {
		t.Fatalf("cikis islenemedi: %v", err)
	}
	if sonuc.Outcome != session.OutcomeReview {
		t.Fatalf("sonuc = %q, beklenen review", sonuc.Outcome)
	}
	if r := incelemeOku(t, pool, sonuc.EventID); r.Reason != session.ReasonExitBeforeEntry {
		t.Fatalf("inceleme nedeni = %q, beklenen %q", r.Reason, session.ReasonExitBeforeEntry)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1 AND status = 'open'`, lotID); n != 1 {
		t.Fatalf("acik oturum sayisi = %d, beklenen 1", n)
	}
}

func TestDusukGuvenCikis(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	giris := an(t, "2026-10-27 10:00:00")

	girisSonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34ABC123", giris))
	if err != nil {
		t.Fatalf("giris islenemedi: %v", err)
	}

	sonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-cikis", session.Exit, "34ABC12", an(t, "2026-10-27 11:00:00")))
	if err != nil {
		t.Fatalf("cikis islenemedi: %v", err)
	}
	if sonuc.Outcome != session.OutcomeReview {
		t.Fatalf("sonuc = %q, beklenen review", sonuc.Outcome)
	}
	r := incelemeOku(t, pool, sonuc.EventID)
	if r.Reason != session.ReasonLowConfidenceReview {
		t.Fatalf("inceleme nedeni = %q, beklenen %q", r.Reason, session.ReasonLowConfidenceReview)
	}
	if r.Confidence != 875 {
		t.Fatalf("guven = %d, beklenen 875", r.Confidence)
	}
	if r.SuggestedID == nil || *r.SuggestedID != girisSonuc.SessionID {
		t.Fatalf("onerilen oturum = %v, beklenen %d", r.SuggestedID, girisSonuc.SessionID)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1 AND status = 'open'`, lotID); n != 1 {
		t.Fatalf("acik oturum sayisi = %d, beklenen 1", n)
	}
}

func TestBicimsizOkumaCikis(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	giris := an(t, "2026-10-27 10:00:00")

	if _, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34ABC123", giris)); err != nil {
		t.Fatalf("giris islenemedi: %v", err)
	}
	sonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-cikis", session.Exit, "34A8C123", an(t, "2026-10-27 11:00:00")))
	if err != nil {
		t.Fatalf("cikis islenemedi: %v", err)
	}
	if sonuc.Outcome != session.OutcomeReview {
		t.Fatalf("sonuc = %q, beklenen review", sonuc.Outcome)
	}
	if r := incelemeOku(t, pool, sonuc.EventID); r.Reason != session.ReasonInvalidFormat {
		t.Fatalf("inceleme nedeni = %q, beklenen %q", r.Reason, session.ReasonInvalidFormat)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1 AND status = 'open'`, lotID); n != 1 {
		t.Fatalf("acik oturum sayisi = %d, beklenen 1", n)
	}
}

func TestGecersizPlakaGiris(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	at := an(t, "2026-10-27 10:00:00")

	gecersiz, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "ABC", at))
	if err != nil {
		t.Fatalf("gecersiz plaka islenemedi: %v", err)
	}
	bos, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "", at.Add(time.Minute)))
	if err != nil {
		t.Fatalf("bos plaka islenemedi: %v", err)
	}

	if gecersiz.Outcome != session.OutcomeReview || bos.Outcome != session.OutcomeReview {
		t.Fatalf("sonuclar review degil: %q / %q", gecersiz.Outcome, bos.Outcome)
	}
	if r := incelemeOku(t, pool, gecersiz.EventID); r.Reason != session.ReasonInvalidPlate {
		t.Fatalf("gecersiz plaka nedeni = %q", r.Reason)
	}
	if r := incelemeOku(t, pool, bos.EventID); r.Reason != session.ReasonEmptyRead {
		t.Fatalf("bos plaka nedeni = %q", r.Reason)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1`, lotID); n != 0 {
		t.Fatalf("oturum sayisi = %d, beklenen 0", n)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-review_items" WHERE lot_id = $1`, lotID); n != 2 {
		t.Fatalf("inceleme sayisi = %d, beklenen 2", n)
	}
}

func TestTarifeYokGiris(t *testing.T) {
	pool, svc := yeniServis(t)
	lotID := testdb.NewLot(t, pool, 100)
	at := an(t, "2026-10-27 10:00:00")

	sonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34ABC123", at))
	if err != nil {
		t.Fatalf("giris islenemedi: %v", err)
	}
	if sonuc.Outcome != session.OutcomeReview {
		t.Fatalf("sonuc = %q, beklenen review", sonuc.Outcome)
	}
	if r := incelemeOku(t, pool, sonuc.EventID); r.Reason != session.ReasonNoTariff {
		t.Fatalf("inceleme nedeni = %q, beklenen %q", r.Reason, session.ReasonNoTariff)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1`, lotID); n != 0 {
		t.Fatalf("oturum sayisi = %d, beklenen 0", n)
	}
}

func TestTarifeDondurma(t *testing.T) {
	pool, svc := yeniServis(t)
	lotID := testdb.NewLot(t, pool, 100)

	sinir := an(t, "2026-11-01 00:00:00")
	v1 := tarifeSurumu(t, benzersiz("v1"), an(t, "2026-01-01 00:00:00"), &sinir, 1)
	v2 := tarifeSurumu(t, benzersiz("v2"), sinir, nil, 2)
	for _, v := range []tariff.Version{v1, v2} {
		if err := store.InsertTariffVersion(context.Background(), pool, lotID, v); err != nil {
			t.Fatalf("tarife %q eklenemedi: %v", v.ID, err)
		}
	}

	giris := an(t, "2026-10-31 23:00:00")
	cikis := an(t, "2026-11-01 02:00:00")

	girisSonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34ABC123", giris))
	if err != nil {
		t.Fatalf("giris islenemedi: %v", err)
	}
	if girisSonuc.Outcome != session.OutcomeOpened {
		t.Fatalf("giris sonucu = %q, beklenen opened", girisSonuc.Outcome)
	}
	s, err := store.GetSession(context.Background(), pool, girisSonuc.SessionID)
	if err != nil {
		t.Fatalf("oturum okunamadi: %v", err)
	}
	if s.TariffVersionID != v1.ID {
		t.Fatalf("giris tarifesi = %q, beklenen v1 %q", s.TariffVersionID, v1.ID)
	}

	cikisSonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-cikis", session.Exit, "34ABC123", cikis))
	if err != nil {
		t.Fatalf("cikis islenemedi: %v", err)
	}
	if cikisSonuc.Outcome != session.OutcomeClosed {
		t.Fatalf("cikis sonucu = %q, beklenen closed", cikisSonuc.Outcome)
	}
	beklenen, err := tariff.Calculate(v1, giris, cikis)
	if err != nil {
		t.Fatalf("v1 hesabi: %v", err)
	}
	yanlis, err := tariff.Calculate(v2, giris, cikis)
	if err != nil {
		t.Fatalf("v2 hesabi: %v", err)
	}
	if beklenen.Total == yanlis.Total {
		t.Fatalf("v1 ve v2 ayni tutari veriyor (%d); test ayirt edemez", beklenen.Total)
	}
	if cikisSonuc.AmountKurus == nil || *cikisSonuc.AmountKurus != beklenen.Total {
		t.Fatalf("cikis tutari = %v, beklenen v1 %d", cikisSonuc.AmountKurus, beklenen.Total)
	}
	s, err = store.GetSession(context.Background(), pool, girisSonuc.SessionID)
	if err != nil {
		t.Fatalf("oturum okunamadi: %v", err)
	}
	if s.AmountKurus == nil || *s.AmountKurus != beklenen.Total {
		t.Fatalf("oturum tutari = %v, beklenen %d", s.AmountKurus, beklenen.Total)
	}

	yeniGiris, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34ABC123", an(t, "2026-11-01 03:00:00")))
	if err != nil {
		t.Fatalf("yeni giris islenemedi: %v", err)
	}
	if yeniGiris.Outcome != session.OutcomeOpened {
		t.Fatalf("yeni giris sonucu = %q, beklenen opened", yeniGiris.Outcome)
	}
	s, err = store.GetSession(context.Background(), pool, yeniGiris.SessionID)
	if err != nil {
		t.Fatalf("yeni oturum okunamadi: %v", err)
	}
	if s.TariffVersionID != v2.ID {
		t.Fatalf("yeni giris tarifesi = %q, beklenen v2 %q", s.TariffVersionID, v2.ID)
	}
}

func TestNormalAkis(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	giris := an(t, "2026-10-27 10:00:00")
	cikis := an(t, "2026-10-27 11:10:00")

	if _, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34ABC123", giris)); err != nil {
		t.Fatalf("giris islenemedi: %v", err)
	}
	sonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-cikis", session.Exit, "34ABC123", cikis))
	if err != nil {
		t.Fatalf("cikis islenemedi: %v", err)
	}
	if sonuc.Outcome != session.OutcomeClosed {
		t.Fatalf("sonuc = %q, beklenen closed", sonuc.Outcome)
	}
	if sonuc.AmountKurus == nil || *sonuc.AmountKurus != 7000 {
		t.Fatalf("tutar = %v, beklenen 7000", sonuc.AmountKurus)
	}

	var quote []byte
	if err := pool.QueryRow(context.Background(),
		`SELECT quote FROM "ya-sessions" WHERE id = $1`, sonuc.SessionID).Scan(&quote); err != nil {
		t.Fatalf("quote okunamadi: %v", err)
	}
	if len(quote) == 0 {
		t.Fatalf("quote bos")
	}
	if !json.Valid(quote) {
		t.Fatalf("quote gecerli JSON degil: %s", quote)
	}
}

func TestTekrarCikis(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	giris := an(t, "2026-10-27 10:00:00")
	cikis := an(t, "2026-10-27 11:10:00")

	if _, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34ABC123", giris)); err != nil {
		t.Fatalf("giris islenemedi: %v", err)
	}
	kapanis, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-cikis", session.Exit, "34ABC123", cikis))
	if err != nil {
		t.Fatalf("cikis islenemedi: %v", err)
	}
	if kapanis.Outcome != session.OutcomeClosed {
		t.Fatalf("ilk cikis = %q, beklenen closed", kapanis.Outcome)
	}

	tekrar, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-cikis", session.Exit, "34ABC123", cikis.Add(2*time.Minute)))
	if err != nil {
		t.Fatalf("tekrar cikis islenemedi: %v", err)
	}
	if tekrar.Outcome != session.OutcomeDuplicateExit {
		t.Fatalf("tekrar cikis = %q, beklenen duplicate_exit", tekrar.Outcome)
	}

	s, err := store.GetSession(context.Background(), pool, kapanis.SessionID)
	if err != nil {
		t.Fatalf("oturum okunamadi: %v", err)
	}
	if s.AmountKurus == nil || *s.AmountKurus != 7000 {
		t.Fatalf("tutar degisti: %v", s.AmountKurus)
	}
	if s.Status != "closed" {
		t.Fatalf("durum = %q, beklenen closed", s.Status)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1`, lotID); n != 1 {
		t.Fatalf("oturum sayisi = %d, beklenen 1", n)
	}
}

func TestFarkliOtoparkAyniPlaka(t *testing.T) {
	pool, svc := yeniServis(t)
	at := an(t, "2026-10-27 10:00:00")

	lotlar := make([]int64, 2)
	for i := range lotlar {
		lotlar[i] = testdb.NewLot(t, pool, 100)
		v := standartTarife(t)
		if err := store.InsertTariffVersion(context.Background(), pool, lotlar[i], v); err != nil {
			t.Fatalf("tarife eklenemedi: %v", err)
		}
	}

	for _, lotID := range lotlar {
		sonuc, err := svc.HandleEvent(context.Background(),
			olay(lotID, "cam-giris", session.Entry, "34ABC123", at))
		if err != nil {
			t.Fatalf("giris islenemedi: %v", err)
		}
		if sonuc.Outcome != session.OutcomeOpened {
			t.Fatalf("sonuc = %q, beklenen opened", sonuc.Outcome)
		}
	}

	for _, lotID := range lotlar {
		n, err := store.CountOpenSessions(context.Background(), pool, lotID, "34ABC123")
		if err != nil {
			t.Fatalf("acik oturum sayilamadi: %v", err)
		}
		if n != 1 {
			t.Fatalf("lot %d acik oturum sayisi = %d, beklenen 1", lotID, n)
		}
	}
}

func TestGecersizOlay(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	at := an(t, "2026-10-27 10:00:00")

	lotSuz := olay(0, "cam-giris", session.Entry, "34ABC123", at)
	if _, err := svc.HandleEvent(context.Background(), lotSuz); !errors.Is(err, session.ErrInvalidEvent) {
		t.Fatalf("lot_id 0 hatasi = %v, beklenen ErrInvalidEvent", err)
	}

	kameraSuz := olay(lotID, "", session.Entry, "34ABC123", at)
	if _, err := svc.HandleEvent(context.Background(), kameraSuz); !errors.Is(err, session.ErrInvalidEvent) {
		t.Fatalf("bos kamera hatasi = %v, beklenen ErrInvalidEvent", err)
	}

	if n := say(t, pool, `SELECT count(*) FROM "ya-plate_events" WHERE lot_id = $1`, lotID); n != 0 {
		t.Fatalf("gecersiz olay yazildi: %d satir", n)
	}
}

func TestOlmayanOtopark(t *testing.T) {
	_, svc := yeniServis(t)
	at := an(t, "2026-10-27 10:00:00")

	_, err := svc.HandleEvent(context.Background(),
		olay(1<<60, "cam-giris", session.Entry, "34ABC123", at))
	if !errors.Is(err, store.ErrLotNotFound) {
		t.Fatalf("hata = %v, beklenen ErrLotNotFound", err)
	}
}

func TestNewHatalari(t *testing.T) {
	pool := testdb.Open(t)
	if _, err := sessionsvc.New(nil, session.DefaultConfig); err == nil {
		t.Fatalf("nil havuz kabul edildi")
	}
	if _, err := sessionsvc.New(pool, session.Config{}); err == nil {
		t.Fatalf("gecersiz ayar kabul edildi")
	}
}

func TestTekrarIncelemeOlayi(t *testing.T) {
	_, svc, lotID, _ := hazirlaStandart(t)
	giris := an(t, "2026-10-27 10:00:00")
	cikis := an(t, "2026-10-27 09:50:00")

	if _, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34ABC123", giris)); err != nil {
		t.Fatalf("giris islenemedi: %v", err)
	}

	cikisOlay := olay(lotID, "cam-cikis", session.Exit, "34ABC123", cikis)
	ilk, err := svc.HandleEvent(context.Background(), cikisOlay)
	if err != nil {
		t.Fatalf("cikis islenemedi: %v", err)
	}
	tekrar, err := svc.HandleEvent(context.Background(), cikisOlay)
	if err != nil {
		t.Fatalf("tekrar islenemedi: %v", err)
	}
	if !tekrar.Replayed {
		t.Fatalf("tekrar Replayed false")
	}
	if tekrar.EventID != ilk.EventID || tekrar.Outcome != ilk.Outcome || tekrar.ReviewID != ilk.ReviewID {
		t.Fatalf("tekrar sonucu farkli: %+v / %+v", tekrar, ilk)
	}
	if tekrar.ReviewID == 0 {
		t.Fatalf("inceleme kimligi bos")
	}
}

func TestIptalEdilenBaglam(t *testing.T) {
	_, svc, lotID, _ := hazirlaStandart(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := svc.HandleEvent(ctx,
		olay(lotID, "cam-giris", session.Entry, "34ABC123", an(t, "2026-10-27 10:00:00"))); err == nil {
		t.Fatalf("iptal edilen baglamda hata bekleniyordu")
	}
}

func TestCokUzunOturum(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	giris := an(t, "2026-10-27 10:00:00")
	cikis := giris.Add(401 * 24 * time.Hour)

	cikisOlay := olay(lotID, "cam-cikis", session.Exit, "34ABC123", cikis)
	cikisSonuc, err := svc.HandleEvent(context.Background(), cikisOlay)
	if err != nil {
		t.Fatalf("bekleyen cikis islenemedi: %v", err)
	}
	if cikisSonuc.Outcome != session.OutcomeOrphanExit {
		t.Fatalf("bekleyen cikis = %q, beklenen orphan_exit", cikisSonuc.Outcome)
	}

	girisSonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34ABC123", giris))
	if err != nil {
		t.Fatalf("giris hata verdi: %v", err)
	}
	if girisSonuc.Outcome != session.OutcomeOpened {
		t.Fatalf("giris sonucu = %q, beklenen opened", girisSonuc.Outcome)
	}
	if girisSonuc.ClosedByOrphanExit {
		t.Fatalf("ClosedByOrphanExit true, beklenen false")
	}

	s, err := store.GetSession(context.Background(), pool, girisSonuc.SessionID)
	if err != nil {
		t.Fatalf("oturum okunamadi: %v", err)
	}
	if s.Status != "open" {
		t.Fatalf("oturum durumu = %q, beklenen open", s.Status)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-plate_events" WHERE lot_id = $1`, lotID); n != 2 {
		t.Fatalf("olay sayisi = %d, beklenen 2", n)
	}
	if r := incelemeOku(t, pool, cikisSonuc.EventID); r.Reason != session.ReasonCloseFailed {
		t.Fatalf("bekleyen cikis nedeni = %q, beklenen %q", r.Reason, session.ReasonCloseFailed)
	}
}

func TestGecGirisKapaliOturum(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	giris := an(t, "2026-10-27 10:00:00")
	cikis := an(t, "2026-10-27 10:40:00")

	if _, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam1", session.Entry, "34ABC123", giris)); err != nil {
		t.Fatalf("giris islenemedi: %v", err)
	}
	kapanis, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam1", session.Exit, "34ABC123", cikis))
	if err != nil {
		t.Fatalf("cikis islenemedi: %v", err)
	}
	if kapanis.Outcome != session.OutcomeClosed {
		t.Fatalf("cikis sonucu = %q, beklenen closed", kapanis.Outcome)
	}
	if kapanis.AmountKurus == nil || *kapanis.AmountKurus != 3000 {
		t.Fatalf("cikis tutari = %v, beklenen 3000", kapanis.AmountKurus)
	}

	gec := an(t, "2026-10-27 10:00:02")
	gecSonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam2", session.Entry, "34ABC123", gec))
	if err != nil {
		t.Fatalf("gec giris islenemedi: %v", err)
	}
	if gecSonuc.Outcome != session.OutcomeDuplicateEntry {
		t.Fatalf("gec giris sonucu = %q, beklenen duplicate_entry", gecSonuc.Outcome)
	}
	if gecSonuc.SessionID != kapanis.SessionID {
		t.Fatalf("gec giris oturumu = %d, beklenen kapali oturum %d", gecSonuc.SessionID, kapanis.SessionID)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1 AND status = 'open'`, lotID); n != 0 {
		t.Fatalf("acik oturum sayisi = %d, beklenen 0", n)
	}

	sonra := gec.Add(48 * time.Hour)
	sonraSonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam3", session.Exit, "34ABC123", sonra))
	if err != nil {
		t.Fatalf("sonraki cikis islenemedi: %v", err)
	}
	if sonraSonuc.Outcome != session.OutcomeOrphanExit {
		t.Fatalf("sonraki cikis sonucu = %q, beklenen orphan_exit", sonraSonuc.Outcome)
	}

	s, err := store.GetSession(context.Background(), pool, kapanis.SessionID)
	if err != nil {
		t.Fatalf("oturum okunamadi: %v", err)
	}
	if s.Status != "closed" {
		t.Fatalf("oturum durumu = %q, beklenen closed", s.Status)
	}
	if s.AmountKurus == nil || *s.AmountKurus != 3000 {
		t.Fatalf("oturum tutari = %v, beklenen 3000", s.AmountKurus)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1`, lotID); n != 1 {
		t.Fatalf("oturum sayisi = %d, beklenen 1", n)
	}
}

func TestGecGirisAcikOturumdanOnce(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	bekleyen := an(t, "2026-10-27 09:00:00")

	if _, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-cikis", session.Exit, "34ABC123", bekleyen)); err != nil {
		t.Fatalf("bekleyen cikis islenemedi: %v", err)
	}

	giris := an(t, "2026-10-27 12:00:00")
	girisSonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34ABC123", giris))
	if err != nil {
		t.Fatalf("giris islenemedi: %v", err)
	}
	if girisSonuc.Outcome != session.OutcomeOpened {
		t.Fatalf("giris sonucu = %q, beklenen opened", girisSonuc.Outcome)
	}
	if girisSonuc.ClosedByOrphanExit {
		t.Fatalf("09:00 bekleyen cikis 12:00 girisiyle eslesmemeli")
	}

	gecSonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris-2", session.Entry, "34ABC123", an(t, "2026-10-27 08:00:00")))
	if err != nil {
		t.Fatalf("gec giris islenemedi: %v", err)
	}
	if gecSonuc.Outcome != session.OutcomeReview {
		t.Fatalf("gec giris sonucu = %q, beklenen review", gecSonuc.Outcome)
	}
	r := incelemeOku(t, pool, gecSonuc.EventID)
	if r.Reason != session.ReasonLateEntry {
		t.Fatalf("gec giris nedeni = %q, beklenen %q", r.Reason, session.ReasonLateEntry)
	}
	if r.SuggestedID == nil || *r.SuggestedID != girisSonuc.SessionID {
		t.Fatalf("onerilen oturum = %v, beklenen %d", r.SuggestedID, girisSonuc.SessionID)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1 AND status = 'open'`, lotID); n != 1 {
		t.Fatalf("acik oturum sayisi = %d, beklenen 1", n)
	}
}

func TestOlayCakismasiFarkliOtopark(t *testing.T) {
	pool, svc := yeniServis(t)
	lotA := yeniLotTarifeli(t, pool)
	lotB := yeniLotTarifeli(t, pool)
	at := an(t, "2026-10-27 10:00:00")

	ev := session.Event{
		LotID:         lotA,
		CameraID:      "cam-cakisma",
		CameraEventID: "ortak-olay",
		Direction:     session.Entry,
		PlateRaw:      "34ABC123",
		CapturedAt:    at,
	}
	if _, err := svc.HandleEvent(context.Background(), ev); err != nil {
		t.Fatalf("lot A girisi islenemedi: %v", err)
	}

	evB := ev
	evB.LotID = lotB
	evB.PlateRaw = "34ABU123"
	if _, err := svc.HandleEvent(context.Background(), evB); !errors.Is(err, sessionsvc.ErrEventConflict) {
		t.Fatalf("lot B hatasi = %v, beklenen ErrEventConflict", err)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-plate_events" WHERE lot_id = $1`, lotB); n != 0 {
		t.Fatalf("lot B olay sayisi = %d, beklenen 0", n)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1`, lotB); n != 0 {
		t.Fatalf("lot B oturum sayisi = %d, beklenen 0", n)
	}
}

func TestOlayCakismasiFarkliIcerik(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	at := an(t, "2026-10-27 10:00:00")
	ev := session.Event{
		LotID:         lotID,
		CameraID:      "cam-icerik",
		CameraEventID: "ayni-anahtar",
		Direction:     session.Entry,
		PlateRaw:      "34ABC123",
		CapturedAt:    at,
	}
	ilk, err := svc.HandleEvent(context.Background(), ev)
	if err != nil {
		t.Fatalf("ilk olay islenemedi: %v", err)
	}
	if ilk.Outcome != session.OutcomeOpened {
		t.Fatalf("ilk sonuc = %q, beklenen opened", ilk.Outcome)
	}

	yanlisYon := ev
	yanlisYon.Direction = session.Exit
	yanlisYon.CapturedAt = at.Add(30 * time.Minute)
	if _, err := svc.HandleEvent(context.Background(), yanlisYon); !errors.Is(err, sessionsvc.ErrEventConflict) {
		t.Fatalf("farkli yon hatasi = %v, beklenen ErrEventConflict", err)
	}

	yanlisPlaka := ev
	yanlisPlaka.PlateRaw = "34ABU123"
	if _, err := svc.HandleEvent(context.Background(), yanlisPlaka); !errors.Is(err, sessionsvc.ErrEventConflict) {
		t.Fatalf("farkli plaka hatasi = %v, beklenen ErrEventConflict", err)
	}

	yanlisZaman := ev
	yanlisZaman.CapturedAt = at.Add(1 * time.Minute)
	if _, err := svc.HandleEvent(context.Background(), yanlisZaman); !errors.Is(err, sessionsvc.ErrEventConflict) {
		t.Fatalf("farkli captured_at hatasi = %v, beklenen ErrEventConflict", err)
	}

	yanlisYonTek := ev
	yanlisYonTek.Direction = session.Exit
	if _, err := svc.HandleEvent(context.Background(), yanlisYonTek); !errors.Is(err, sessionsvc.ErrEventConflict) {
		t.Fatalf("farkli direction hatasi = %v, beklenen ErrEventConflict", err)
	}

	lotB := yeniLotTarifeli(t, pool)
	yanlisLot := ev
	yanlisLot.LotID = lotB
	if _, err := svc.HandleEvent(context.Background(), yanlisLot); !errors.Is(err, sessionsvc.ErrEventConflict) {
		t.Fatalf("farkli lot_id hatasi = %v, beklenen ErrEventConflict", err)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-plate_events" WHERE lot_id = $1`, lotB); n != 0 {
		t.Fatalf("lot B olay sayisi = %d, beklenen 0", n)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1`, lotB); n != 0 {
		t.Fatalf("lot B oturum sayisi = %d, beklenen 0", n)
	}

	ayni, err := svc.HandleEvent(context.Background(), ev)
	if err != nil {
		t.Fatalf("ayni icerik islenemedi: %v", err)
	}
	if !ayni.Replayed {
		t.Fatalf("ayni icerik Replayed false")
	}
	if ayni.EventID != ilk.EventID || ayni.Outcome != ilk.Outcome || ayni.SessionID != ilk.SessionID {
		t.Fatalf("ayni icerik sonucu farkli: %+v / %+v", ayni, ilk)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-plate_events" WHERE lot_id = $1`, lotID); n != 1 {
		t.Fatalf("olay sayisi = %d, beklenen 1", n)
	}
}

func TestOlayCakismasiEszamanli(t *testing.T) {
	pool, svc := yeniServis(t)
	const tur = 20

	for i := 0; i < tur; i++ {
		lotA := yeniLotTarifeli(t, pool)
		lotB := yeniLotTarifeli(t, pool)
		anahtar := fmt.Sprintf("eszamanli-%d", i)
		at := an(t, "2026-10-27 10:00:00").Add(time.Duration(i) * time.Minute)
		evA := session.Event{
			LotID: lotA, CameraID: "cam-p2", CameraEventID: anahtar,
			Direction: session.Entry, PlateRaw: "34ABU123", CapturedAt: at,
		}
		evB := evA
		evB.LotID = lotB
		evB.PlateRaw = "34ABV123"

		olaylar := []session.Event{evA, evB}
		sonuclar := make([]sessionsvc.Result, 2)
		hatalar := make([]error, 2)
		var wg sync.WaitGroup
		bas := make(chan struct{})
		for j := range olaylar {
			wg.Add(1)
			go func(j int) {
				defer wg.Done()
				<-bas
				sonuclar[j], hatalar[j] = svc.HandleEvent(context.Background(), olaylar[j])
			}(j)
		}
		close(bas)
		wg.Wait()

		basarili, cakisan := 0, 0
		for j := range hatalar {
			switch {
			case hatalar[j] == nil:
				basarili++
				if sonuclar[j].Outcome != session.OutcomeOpened {
					t.Fatalf("tur %d: basarili sonuc = %q, beklenen opened", i, sonuclar[j].Outcome)
				}
			case errors.Is(hatalar[j], sessionsvc.ErrEventConflict):
				cakisan++
			default:
				t.Fatalf("tur %d: beklenmeyen hata: %v", i, hatalar[j])
			}
		}
		if basarili != 1 || cakisan != 1 {
			t.Fatalf("tur %d: basarili=%d cakisan=%d, beklenen 1/1", i, basarili, cakisan)
		}
		if n := say(t, pool,
			`SELECT count(*) FROM "ya-plate_events" WHERE camera_id = $1 AND camera_event_id = $2`,
			"cam-p2", anahtar); n != 1 {
			t.Fatalf("tur %d: anahtar icin olay sayisi = %d, beklenen 1", i, n)
		}
	}
}

func TestTekrarSonucuTutar(t *testing.T) {
	_, svc, lotID, _ := hazirlaStandart(t)
	giris := an(t, "2026-10-27 10:00:00")
	cikis := an(t, "2026-10-27 11:10:00")

	if _, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34ABC123", giris)); err != nil {
		t.Fatalf("giris islenemedi: %v", err)
	}
	cikisOlay := olay(lotID, "cam-cikis", session.Exit, "34ABC123", cikis)
	ilk, err := svc.HandleEvent(context.Background(), cikisOlay)
	if err != nil {
		t.Fatalf("cikis islenemedi: %v", err)
	}
	if ilk.Outcome != session.OutcomeClosed {
		t.Fatalf("cikis sonucu = %q, beklenen closed", ilk.Outcome)
	}
	if ilk.AmountKurus == nil {
		t.Fatalf("ilk sonuc tutari bos")
	}

	tekrar, err := svc.HandleEvent(context.Background(), cikisOlay)
	if err != nil {
		t.Fatalf("tekrar cikis islenemedi: %v", err)
	}
	if !tekrar.Replayed {
		t.Fatalf("tekrar Replayed false")
	}
	if tekrar.EventID != ilk.EventID {
		t.Fatalf("tekrar olayi = %d, beklenen %d", tekrar.EventID, ilk.EventID)
	}
	if tekrar.AmountKurus == nil || *tekrar.AmountKurus != *ilk.AmountKurus {
		t.Fatalf("tekrar tutari = %v, beklenen %v", tekrar.AmountKurus, ilk.AmountKurus)
	}
}

func TestKapatmaHatasiIncelemeye(t *testing.T) {
	pool, svc := yeniServis(t)
	lotID := testdb.NewLot(t, pool, 100)
	if err := store.InsertTariffVersion(context.Background(), pool, lotID, erkenTarife(t)); err != nil {
		t.Fatalf("tarife eklenemedi: %v", err)
	}
	giris := an(t, "2025-01-01 10:00:00")
	cikis := an(t, "2026-03-01 10:00:00")

	girisSonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34ABC123", giris))
	if err != nil {
		t.Fatalf("giris islenemedi: %v", err)
	}
	if girisSonuc.Outcome != session.OutcomeOpened {
		t.Fatalf("giris sonucu = %q, beklenen opened", girisSonuc.Outcome)
	}

	cikisOlay := olay(lotID, "cam-cikis", session.Exit, "34ABC123", cikis)
	sonuc, err := svc.HandleEvent(context.Background(), cikisOlay)
	if err != nil {
		t.Fatalf("cikis hata verdi: %v", err)
	}
	if sonuc.Outcome != session.OutcomeReview {
		t.Fatalf("cikis sonucu = %q, beklenen review", sonuc.Outcome)
	}
	r := incelemeOku(t, pool, sonuc.EventID)
	if r.Reason != session.ReasonCloseFailed {
		t.Fatalf("inceleme nedeni = %q, beklenen %q", r.Reason, session.ReasonCloseFailed)
	}
	if r.SuggestedID == nil || *r.SuggestedID != girisSonuc.SessionID {
		t.Fatalf("onerilen oturum = %v, beklenen %d", r.SuggestedID, girisSonuc.SessionID)
	}

	s, err := store.GetSession(context.Background(), pool, girisSonuc.SessionID)
	if err != nil {
		t.Fatalf("oturum okunamadi: %v", err)
	}
	if s.Status != "open" {
		t.Fatalf("oturum durumu = %q, beklenen open", s.Status)
	}
	kayit, bulundu, err := store.FindEventByCameraKey(context.Background(), pool, cikisOlay.CameraID, cikisOlay.CameraEventID)
	if err != nil {
		t.Fatalf("cikis olayi aranamadi: %v", err)
	}
	if !bulundu {
		t.Fatalf("cikis olayi yazilmamis")
	}
	if kayit.Outcome != session.OutcomeReview {
		t.Fatalf("cikis olayi sonucu = %q, beklenen review", kayit.Outcome)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-plate_events" WHERE lot_id = $1`, lotID); n != 2 {
		t.Fatalf("olay sayisi = %d, beklenen 2", n)
	}
}

func TestBekleyenCikisKapatmaHatasi(t *testing.T) {
	pool, svc := yeniServis(t)
	lotID := testdb.NewLot(t, pool, 100)
	if err := store.InsertTariffVersion(context.Background(), pool, lotID, erkenTarife(t)); err != nil {
		t.Fatalf("tarife eklenemedi: %v", err)
	}
	cikis := an(t, "2026-03-01 10:00:00")
	cikisOlay := olay(lotID, "cam-cikis", session.Exit, "34ABC123", cikis)
	cikisSonuc, err := svc.HandleEvent(context.Background(), cikisOlay)
	if err != nil {
		t.Fatalf("bekleyen cikis islenemedi: %v", err)
	}
	if cikisSonuc.Outcome != session.OutcomeOrphanExit {
		t.Fatalf("bekleyen cikis = %q, beklenen orphan_exit", cikisSonuc.Outcome)
	}

	girisSonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34ABC123", an(t, "2025-01-01 10:00:00")))
	if err != nil {
		t.Fatalf("giris hata verdi: %v", err)
	}
	if girisSonuc.Outcome != session.OutcomeOpened {
		t.Fatalf("giris sonucu = %q, beklenen opened", girisSonuc.Outcome)
	}
	if girisSonuc.ClosedByOrphanExit {
		t.Fatalf("ClosedByOrphanExit true, beklenen false")
	}
	if girisSonuc.AmountKurus != nil {
		t.Fatalf("giris tutari dolu: %v", girisSonuc.AmountKurus)
	}

	s, err := store.GetSession(context.Background(), pool, girisSonuc.SessionID)
	if err != nil {
		t.Fatalf("oturum okunamadi: %v", err)
	}
	if s.Status != "open" {
		t.Fatalf("oturum durumu = %q, beklenen open", s.Status)
	}

	kayit, bulundu, err := store.FindEventByCameraKey(context.Background(), pool, cikisOlay.CameraID, cikisOlay.CameraEventID)
	if err != nil {
		t.Fatalf("bekleyen cikis aranamadi: %v", err)
	}
	if !bulundu {
		t.Fatalf("bekleyen cikis bulunamadi")
	}
	if kayit.Outcome != session.OutcomeReview {
		t.Fatalf("bekleyen cikis sonucu = %q, beklenen review", kayit.Outcome)
	}
	r := incelemeOku(t, pool, cikisSonuc.EventID)
	if r.Reason != session.ReasonCloseFailed {
		t.Fatalf("inceleme nedeni = %q, beklenen %q", r.Reason, session.ReasonCloseFailed)
	}
	if r.SuggestedID == nil || *r.SuggestedID != girisSonuc.SessionID {
		t.Fatalf("onerilen oturum = %v, beklenen %d", r.SuggestedID, girisSonuc.SessionID)
	}
}

func TestTekrarOlayNanosaniye(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	at := an(t, "2026-10-27 10:00:00").Add(123456789 * time.Nanosecond)
	ev := session.Event{
		LotID:         lotID,
		CameraID:      "cam-nano",
		CameraEventID: "olay-nano",
		Direction:     session.Entry,
		PlateRaw:      "34ABC123",
		CapturedAt:    at,
	}

	ilk, err := svc.HandleEvent(context.Background(), ev)
	if err != nil {
		t.Fatalf("ilk olay islenemedi: %v", err)
	}
	if ilk.Outcome != session.OutcomeOpened {
		t.Fatalf("ilk sonuc = %q, beklenen opened", ilk.Outcome)
	}

	tekrar, err := svc.HandleEvent(context.Background(), ev)
	if err != nil {
		t.Fatalf("tekrar olay hata verdi: %v", err)
	}
	if !tekrar.Replayed {
		t.Fatalf("tekrar Replayed false")
	}
	if tekrar.EventID != ilk.EventID || tekrar.SessionID != ilk.SessionID || tekrar.Outcome != ilk.Outcome {
		t.Fatalf("tekrar sonucu farkli: %+v / %+v", tekrar, ilk)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-plate_events" WHERE lot_id = $1`, lotID); n != 1 {
		t.Fatalf("plate_events satir sayisi = %d, beklenen 1", n)
	}
}

func TestTekrarOlayNanosaniyeYuvarlama(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	giris := an(t, "2026-10-27 10:00:00").Add(999999999 * time.Nanosecond)
	cikis := an(t, "2026-10-27 11:10:00").Add(999999999 * time.Nanosecond)

	girisOlay := session.Event{
		LotID:         lotID,
		CameraID:      "cam-yuvarlama",
		CameraEventID: "giris-yuvarlama",
		Direction:     session.Entry,
		PlateRaw:      "34ABU123",
		CapturedAt:    giris,
	}
	ilkGiris, err := svc.HandleEvent(context.Background(), girisOlay)
	if err != nil {
		t.Fatalf("giris islenemedi: %v", err)
	}
	if ilkGiris.Outcome != session.OutcomeOpened {
		t.Fatalf("giris sonucu = %q, beklenen opened", ilkGiris.Outcome)
	}
	tekrarGiris, err := svc.HandleEvent(context.Background(), girisOlay)
	if err != nil {
		t.Fatalf("tekrar giris hata verdi: %v", err)
	}
	if !tekrarGiris.Replayed {
		t.Fatalf("tekrar giris Replayed false")
	}
	if tekrarGiris.EventID != ilkGiris.EventID || tekrarGiris.SessionID != ilkGiris.SessionID ||
		tekrarGiris.Outcome != ilkGiris.Outcome {
		t.Fatalf("tekrar giris sonucu farkli: %+v / %+v", tekrarGiris, ilkGiris)
	}

	cikisOlay := session.Event{
		LotID:         lotID,
		CameraID:      "cam-yuvarlama",
		CameraEventID: "cikis-yuvarlama",
		Direction:     session.Exit,
		PlateRaw:      "34ABU123",
		CapturedAt:    cikis,
	}
	ilkCikis, err := svc.HandleEvent(context.Background(), cikisOlay)
	if err != nil {
		t.Fatalf("cikis islenemedi: %v", err)
	}
	if ilkCikis.Outcome != session.OutcomeClosed {
		t.Fatalf("cikis sonucu = %q, beklenen closed", ilkCikis.Outcome)
	}
	if ilkCikis.AmountKurus == nil {
		t.Fatalf("ilk cikis tutari bos")
	}
	tekrarCikis, err := svc.HandleEvent(context.Background(), cikisOlay)
	if err != nil {
		t.Fatalf("tekrar cikis hata verdi: %v", err)
	}
	if !tekrarCikis.Replayed {
		t.Fatalf("tekrar cikis Replayed false")
	}
	if tekrarCikis.EventID != ilkCikis.EventID || tekrarCikis.SessionID != ilkCikis.SessionID ||
		tekrarCikis.Outcome != ilkCikis.Outcome {
		t.Fatalf("tekrar cikis sonucu farkli: %+v / %+v", tekrarCikis, ilkCikis)
	}
	if tekrarCikis.AmountKurus == nil || *tekrarCikis.AmountKurus != *ilkCikis.AmountKurus {
		t.Fatalf("tekrar cikis tutari = %v, beklenen %v", tekrarCikis.AmountKurus, ilkCikis.AmountKurus)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-plate_events" WHERE lot_id = $1`, lotID); n != 2 {
		t.Fatalf("plate_events satir sayisi = %d, beklenen 2", n)
	}
}

func TestOlayCakismasiMikrosaniyeFarki(t *testing.T) {
	pool, svc, lotID, _ := hazirlaStandart(t)
	at := an(t, "2026-10-27 10:00:00")
	ev := session.Event{
		LotID:         lotID,
		CameraID:      "cam-mikro",
		CameraEventID: "olay-mikro",
		Direction:     session.Entry,
		PlateRaw:      "34ABC123",
		CapturedAt:    at,
	}
	if _, err := svc.HandleEvent(context.Background(), ev); err != nil {
		t.Fatalf("ilk olay islenemedi: %v", err)
	}

	yanlis := ev
	yanlis.CapturedAt = at.Add(1 * time.Microsecond)
	if _, err := svc.HandleEvent(context.Background(), yanlis); !errors.Is(err, sessionsvc.ErrEventConflict) {
		t.Fatalf("mikrosaniye farki hatasi = %v, beklenen ErrEventConflict", err)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-plate_events" WHERE lot_id = $1`, lotID); n != 1 {
		t.Fatalf("plate_events satir sayisi = %d, beklenen 1", n)
	}
}
