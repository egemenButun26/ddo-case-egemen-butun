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
		`SELECT reason, confidence, suggested_session_id FROM review_items WHERE event_id = $1`,
		eventID,
	).Scan(&s.Reason, &s.Confidence, &s.SuggestedID); err != nil {
		t.Fatalf("inceleme okunamadi: %v", err)
	}
	return s
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

	if n := say(t, pool, `SELECT count(*) FROM plate_events WHERE lot_id = $1`, lotID); n != 50 {
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
		`SELECT count(*) FROM sessions WHERE lot_id = $1 AND status = 'open'`, lotID); n != 1 {
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

	if n := say(t, pool, `SELECT count(*) FROM plate_events WHERE lot_id = $1`, lotID); n != 1 {
		t.Fatalf("plate_events satir sayisi = %d, beklenen 1", n)
	}
	if n := say(t, pool, `SELECT count(*) FROM sessions WHERE lot_id = $1`, lotID); n != 1 {
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
	if n := say(t, pool, `SELECT count(*) FROM sessions WHERE lot_id = $1`, lotID); n != 0 {
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
	if n := say(t, pool, `SELECT count(*) FROM sessions WHERE lot_id = $1 AND status = 'open'`, lotID); n != 1 {
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
	if n := say(t, pool, `SELECT count(*) FROM sessions WHERE lot_id = $1 AND status = 'open'`, lotID); n != 1 {
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
	if n := say(t, pool, `SELECT count(*) FROM sessions WHERE lot_id = $1 AND status = 'open'`, lotID); n != 1 {
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
	if n := say(t, pool, `SELECT count(*) FROM sessions WHERE lot_id = $1`, lotID); n != 0 {
		t.Fatalf("oturum sayisi = %d, beklenen 0", n)
	}
	if n := say(t, pool, `SELECT count(*) FROM review_items WHERE lot_id = $1`, lotID); n != 2 {
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
	if n := say(t, pool, `SELECT count(*) FROM sessions WHERE lot_id = $1`, lotID); n != 0 {
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
		`SELECT quote FROM sessions WHERE id = $1`, sonuc.SessionID).Scan(&quote); err != nil {
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
	if n := say(t, pool, `SELECT count(*) FROM sessions WHERE lot_id = $1`, lotID); n != 1 {
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

	if n := say(t, pool, `SELECT count(*) FROM plate_events WHERE lot_id = $1`, lotID); n != 0 {
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

	if _, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-cikis", session.Exit, "34ABC123", cikis)); err != nil {
		t.Fatalf("bekleyen cikis islenemedi: %v", err)
	}

	_, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34ABC123", giris))
	if !errors.Is(err, tariff.ErrTooLong) {
		t.Fatalf("hata = %v, beklenen ErrTooLong", err)
	}

	if n := say(t, pool, `SELECT count(*) FROM sessions WHERE lot_id = $1`, lotID); n != 0 {
		t.Fatalf("oturum sayisi = %d, beklenen 0", n)
	}
	if n := say(t, pool, `SELECT count(*) FROM plate_events WHERE lot_id = $1`, lotID); n != 1 {
		t.Fatalf("olay sayisi = %d, beklenen 1", n)
	}
}
