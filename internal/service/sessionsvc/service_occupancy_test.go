package sessionsvc_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"parkakil/internal/domain/session"
	"parkakil/internal/service/occupancysvc"
	"parkakil/internal/service/sessionsvc"
	"parkakil/internal/store"
)

func sayim(t *testing.T, sonuclar []sessionsvc.Result) (acilan, inceleme int) {
	t.Helper()
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
	return acilan, inceleme
}

func TestRezervasyonluKapasite(t *testing.T) {
	pool, svc, lotID, _ := hazirlaKapasite(t, 10)
	rezervasyon, err := occupancysvc.New(pool)
	if err != nil {
		t.Fatalf("occupancysvc olusturulamadi: %v", err)
	}
	at := an(t, "2026-10-27 10:00:00")
	liste := kapasitePlakalari(25)
	if len(liste) != 25 {
		t.Fatalf("plaka sayisi = %d, beklenen 25", len(liste))
	}

	for i := 0; i < 5; i++ {
		if _, err := rezervasyon.Reserve(context.Background(), lotID, liste[i], at); err != nil {
			t.Fatalf("%s rezervasyonu eklenemedi: %v", liste[i], err)
		}
	}

	sonuclar := esZamanli(t, 20, func(i int) (sessionsvc.Result, error) {
		return svc.HandleEvent(context.Background(),
			olay(lotID, "cam-giris", session.Entry, liste[5+i], at))
	})

	acilan, inceleme := sayim(t, sonuclar)
	if acilan != 5 {
		t.Fatalf("rezervasyonsuz acilan = %d, beklenen 5", acilan)
	}
	if inceleme != 15 {
		t.Fatalf("capacity_full incelemesi = %d, beklenen 15", inceleme)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1 AND status = 'open'`, lotID); n != 5 {
		t.Fatalf("acik oturum = %d, beklenen 5", n)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-review_items" WHERE lot_id = $1 AND reason = $2`,
		lotID, session.ReasonCapacityFull); n != 15 {
		t.Fatalf("capacity_full inceleme = %d, beklenen 15", n)
	}

	sahipSonuclar := esZamanli(t, 5, func(i int) (sessionsvc.Result, error) {
		return svc.HandleEvent(context.Background(),
			olay(lotID, "cam-giris", session.Entry, liste[i], at.Add(time.Minute)))
	})
	for _, r := range sahipSonuclar {
		if r.Outcome != session.OutcomeOpened {
			t.Fatalf("rezervasyon sahibi giris = %q, beklenen opened", r.Outcome)
		}
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1 AND status = 'open'`, lotID); n != 10 {
		t.Fatalf("toplam acik oturum = %d, beklenen 10", n)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-reservations" WHERE lot_id = $1 AND status = 'arrived'`, lotID); n != 5 {
		t.Fatalf("arrived rezervasyon = %d, beklenen 5", n)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-reservations" WHERE lot_id = $1 AND status = 'active'`, lotID); n != 0 {
		t.Fatalf("kalan aktif rezervasyon = %d, beklenen 0", n)
	}
	if n := say(t, pool, `SELECT occupied FROM "ya-lot_counters" WHERE lot_id = $1`, lotID); n != 10 {
		t.Fatalf("sayac = %d, beklenen 10", n)
	}
}

func TestSayacGirisCikis(t *testing.T) {
	pool, svc, lotID, _ := hazirlaKapasite(t, 100)
	girisAt := an(t, "2026-10-27 10:00:00")

	ilk, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34AAA001", girisAt))
	if err != nil {
		t.Fatalf("giris islenemedi: %v", err)
	}
	if ilk.Outcome != session.OutcomeOpened {
		t.Fatalf("giris = %q, beklenen opened", ilk.Outcome)
	}
	if n := say(t, pool, `SELECT occupied FROM "ya-lot_counters" WHERE lot_id = $1`, lotID); n != 1 {
		t.Fatalf("giris sonrasi sayac = %d, beklenen 1", n)
	}

	tekrarOlay := olay(lotID, "cam-giris", session.Entry, "34AAA001", girisAt.Add(time.Minute))
	tekrar, err := svc.HandleEvent(context.Background(), tekrarOlay)
	if err != nil {
		t.Fatalf("tekrar giris islenemedi: %v", err)
	}
	if tekrar.Outcome != session.OutcomeDuplicateEntry {
		t.Fatalf("tekrar giris = %q, beklenen duplicate_entry", tekrar.Outcome)
	}
	if n := say(t, pool, `SELECT occupied FROM "ya-lot_counters" WHERE lot_id = $1`, lotID); n != 1 {
		t.Fatalf("tekrar giris sonrasi sayac = %d, beklenen 1", n)
	}

	cikis, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-cikis", session.Exit, "34AAA001", girisAt.Add(2*time.Hour)))
	if err != nil {
		t.Fatalf("cikis islenemedi: %v", err)
	}
	if cikis.Outcome != session.OutcomeClosed {
		t.Fatalf("cikis = %q, beklenen closed", cikis.Outcome)
	}
	if n := say(t, pool, `SELECT occupied FROM "ya-lot_counters" WHERE lot_id = $1`, lotID); n != 0 {
		t.Fatalf("cikis sonrasi sayac = %d, beklenen 0", n)
	}

	// Bekleyen cikis once gelir, giris onu kapatir: sayaç +1 sonra -1 olmali.
	bekleyenAt := an(t, "2026-10-27 12:40:00")
	bekleyen, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-cikis", session.Exit, "34BBB002", bekleyenAt))
	if err != nil {
		t.Fatalf("bekleyen cikis islenemedi: %v", err)
	}
	if bekleyen.Outcome != session.OutcomeOrphanExit {
		t.Fatalf("bekleyen cikis = %q, beklenen orphan_exit", bekleyen.Outcome)
	}
	if n := say(t, pool, `SELECT occupied FROM "ya-lot_counters" WHERE lot_id = $1`, lotID); n != 0 {
		t.Fatalf("bekleyen cikis sonrasi sayac = %d, beklenen 0", n)
	}

	gecGiris, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34BBB002", an(t, "2026-10-27 12:00:00")))
	if err != nil {
		t.Fatalf("gec giris islenemedi: %v", err)
	}
	if !gecGiris.ClosedByOrphanExit {
		t.Fatalf("bekleyen cikisla kapanmadi: %+v", gecGiris)
	}
	if n := say(t, pool, `SELECT occupied FROM "ya-lot_counters" WHERE lot_id = $1`, lotID); n != 0 {
		t.Fatalf("bekleyen cikisla kapanis sonrasi sayac = %d, beklenen 0", n)
	}
}

func TestSayacEsZamanliAyniPlaka(t *testing.T) {
	pool, svc, lotID, _ := hazirlaKapasite(t, 100)
	at := an(t, "2026-10-27 10:00:00")

	esZamanli(t, 50, func(i int) (sessionsvc.Result, error) {
		ev := olay(lotID, "cam-giris", session.Entry, "34CCC003", at)
		ev.CameraEventID = fmt.Sprintf("sayac-%d", i)
		return svc.HandleEvent(context.Background(), ev)
	})

	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1 AND status = 'open'`, lotID); n != 1 {
		t.Fatalf("acik oturum = %d, beklenen 1", n)
	}
	if n := say(t, pool, `SELECT occupied FROM "ya-lot_counters" WHERE lot_id = $1`, lotID); n != 1 {
		t.Fatalf("sayac = %d, beklenen 1", n)
	}
}

func TestRezervasyonluKapasiteDolu(t *testing.T) {
	pool, svc, lotID, _ := hazirlaKapasite(t, 1)
	rezervasyon, err := occupancysvc.New(pool)
	if err != nil {
		t.Fatalf("occupancysvc olusturulamadi: %v", err)
	}
	at := an(t, "2026-10-27 10:00:00")
	if _, err := rezervasyon.Reserve(context.Background(), lotID, "34EEE005", at); err != nil {
		t.Fatalf("rezervasyon eklenemedi: %v", err)
	}

	sonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34FFF006", at.Add(time.Minute)))
	if err != nil {
		t.Fatalf("rezervasyonsuz giris islenemedi: %v", err)
	}
	if sonuc.Outcome != session.OutcomeReview {
		t.Fatalf("rezervasyonsuz giris = %q, beklenen review", sonuc.Outcome)
	}
	if r := incelemeOku(t, pool, sonuc.EventID); r.Reason != session.ReasonCapacityFull {
		t.Fatalf("inceleme nedeni = %q, beklenen %q", r.Reason, session.ReasonCapacityFull)
	}
	if n := say(t, pool, `SELECT count(*) FROM "ya-sessions" WHERE lot_id = $1`, lotID); n != 0 {
		t.Fatalf("oturum sayisi = %d, beklenen 0", n)
	}

	// Rezervasyon sahibi kendi yerine girebilir.
	sahip, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34EEE005", at.Add(2*time.Minute)))
	if err != nil {
		t.Fatalf("rezervasyon sahibi girisi islenemedi: %v", err)
	}
	if sahip.Outcome != session.OutcomeOpened {
		t.Fatalf("rezervasyon sahibi girisi = %q, beklenen opened", sahip.Outcome)
	}
	if n := say(t, pool, `SELECT occupied FROM "ya-lot_counters" WHERE lot_id = $1`, lotID); n != 1 {
		t.Fatalf("sayac = %d, beklenen 1", n)
	}
}

func TestRezervasyonluGirisSayac(t *testing.T) {
	pool, svc, lotID, _ := hazirlaKapasite(t, 1)
	rezervasyon, err := occupancysvc.New(pool)
	if err != nil {
		t.Fatalf("occupancysvc olusturulamadi: %v", err)
	}
	at := an(t, "2026-10-27 10:00:00")
	if _, err := rezervasyon.Reserve(context.Background(), lotID, "34DDD004", at); err != nil {
		t.Fatalf("rezervasyon eklenemedi: %v", err)
	}

	sonuc, err := svc.HandleEvent(context.Background(),
		olay(lotID, "cam-giris", session.Entry, "34DDD004", at.Add(time.Minute)))
	if err != nil {
		t.Fatalf("rezervasyonlu giris islenemedi: %v", err)
	}
	if sonuc.Outcome != session.OutcomeOpened {
		t.Fatalf("rezervasyonlu giris = %q, beklenen opened", sonuc.Outcome)
	}
	if n := say(t, pool, `SELECT occupied FROM "ya-lot_counters" WHERE lot_id = $1`, lotID); n != 1 {
		t.Fatalf("sayac = %d, beklenen 1", n)
	}

	if _, err := store.GetSession(context.Background(), pool, sonuc.SessionID); err != nil {
		t.Fatalf("oturum okunamadi: %v", err)
	}
}
