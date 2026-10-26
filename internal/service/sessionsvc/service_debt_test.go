package sessionsvc_test

import (
	"context"
	"testing"
	"time"

	"parkakil/internal/domain/payment"
	"parkakil/internal/domain/session"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/store"
)

func TestExitDebtYazimiServis(t *testing.T) {
	pool, svc, lotID, v := hazirlaStandart(t)
	giris := an(t, "2026-10-27 10:00:00")

	girisYap := func(plaka string) int64 {
		t.Helper()
		res, err := svc.HandleEvent(context.Background(), olay(lotID, "cam-giris", session.Entry, plaka, giris))
		if err != nil {
			t.Fatalf("giris: %v", err)
		}
		return res.SessionID
	}
	cikisYap := func(plaka string, at time.Time) (int64, tariff.Kurus) {
		t.Helper()
		res, err := svc.HandleEvent(context.Background(), olay(lotID, "cam-cikis", session.Exit, plaka, at))
		if err != nil {
			t.Fatalf("cikis: %v", err)
		}
		if res.AmountKurus == nil {
			t.Fatalf("tutar yok: %+v", res)
		}
		return res.SessionID, *res.AmountKurus
	}
	borc := func(sessionID int64) (tariff.Kurus, bool) {
		t.Helper()
		var toplam tariff.Kurus
		err := pool.QueryRow(context.Background(),
			`SELECT COALESCE(sum(remaining_kurus), 0) FROM "ya-debts" WHERE session_id = $1 AND status = 'open'`,
			sessionID).Scan(&toplam)
		if err != nil {
			t.Fatalf("borc: %v", err)
		}
		var adet int
		if err := pool.QueryRow(context.Background(),
			`SELECT count(*) FROM "ya-debts" WHERE session_id = $1`, sessionID).Scan(&adet); err != nil {
			t.Fatalf("borc sayisi: %v", err)
		}
		return toplam, adet > 0
	}
	odemeEkle := func(sessionID int64, tutar tariff.Kurus, at time.Time) {
		t.Helper()
		p := payment.Payment{
			ProviderRef: benzersiz("prv"),
			Amount:      tutar,
			Status:      payment.StatusSucceeded,
			RequestedAt: at.Add(-time.Minute),
			PaidAt:      at,
			ConfirmedAt: at.Add(time.Second),
		}
		if _, err := store.InsertPayment(context.Background(), pool, sessionID, benzersiz("idem"), [32]byte{9}, p); err != nil {
			t.Fatalf("odeme: %v", err)
		}
	}

	// Odemesiz: borc tam tutar.
	plaka1 := "34ZQA117"
	s1 := girisYap(plaka1)
	_, tutar1 := cikisYap(plaka1, giris.Add(70*time.Minute))
	borc1, var1 := borc(s1)
	if !var1 || borc1 != tutar1 || borc1 == 0 {
		t.Fatalf("odemesiz borc = %d (var %v), tutar %d", borc1, var1, tutar1)
	}

	// Kismi odeme: borc fark kadar.
	plaka2 := "06WXB248"
	s2 := girisYap(plaka2)
	q, err := tariff.Calculate(v, giris, giris.Add(70*time.Minute))
	if err != nil {
		t.Fatalf("hesap: %v", err)
	}
	odemeEkle(s2, q.Total/2, giris.Add(30*time.Minute))
	_, tutar2 := cikisYap(plaka2, giris.Add(70*time.Minute))
	borc2, var2 := borc(s2)
	if !var2 || borc2 != tutar2-q.Total/2 {
		t.Fatalf("kismi borc = %d (var %v), beklenen %d", borc2, var2, tutar2-q.Total/2)
	}

	// Tam odeme: borc yok.
	plaka3 := "35YVC369"
	s3 := girisYap(plaka3)
	odemeEkle(s3, q.Total, giris.Add(30*time.Minute))
	_, _ = cikisYap(plaka3, giris.Add(70*time.Minute))
	if borc3, var3 := borc(s3); var3 || borc3 != 0 {
		t.Fatalf("tam odemede borc = %d (var %v)", borc3, var3)
	}

	// Ucretsiz oturum: borc yazilmaz (total <= 0 dali).
	plaka4 := "07UTD480"
	s4 := girisYap(plaka4)
	if _, tutar4 := cikisYap(plaka4, giris.Add(10*time.Minute)); tutar4 != 0 {
		t.Fatalf("ucretsiz oturum tutari = %d", tutar4)
	}
	if borc4, var4 := borc(s4); var4 || borc4 != 0 {
		t.Fatalf("ucretsiz oturumda borc = %d (var %v)", borc4, var4)
	}
}
