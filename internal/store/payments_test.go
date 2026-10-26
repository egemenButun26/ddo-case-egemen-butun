package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"parkakil/internal/domain/payment"
	"parkakil/internal/domain/session"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/store"
	"parkakil/internal/testdb"
)

func odemeOturumu(t *testing.T, pool *pgxpool.Pool, lotID int64, tariffID, plate string, entered time.Time) int64 {
	t.Helper()
	ev := session.Event{
		LotID:         lotID,
		CameraID:      benzersiz("cam"),
		CameraEventID: benzersiz("olay"),
		Direction:     session.Entry,
		PlateRaw:      plate,
		CapturedAt:    entered,
	}
	eventID, err := store.InsertEvent(context.Background(), pool, ev, plate, session.OutcomeOpened, 0)
	if err != nil {
		t.Fatalf("olay eklenemedi: %v", err)
	}
	id, err := store.InsertSession(context.Background(), pool, lotID, plate, eventID, entered, tariffID)
	if err != nil {
		t.Fatalf("oturum eklenemedi: %v", err)
	}
	return id
}

func hash32(b byte) [32]byte {
	var h [32]byte
	for i := range h {
		h[i] = b
	}
	return h
}

func bekleyenOdeme(at time.Time, amount int64, ref string) payment.Payment {
	return payment.Payment{
		ProviderRef: ref,
		Amount:      tariff.Kurus(amount),
		Status:      payment.StatusPending,
		RequestedAt: at,
	}
}

func basariliOdeme(at time.Time, amount int64, ref string) payment.Payment {
	t0 := at.Add(-time.Minute)
	return payment.Payment{
		ProviderRef: ref,
		Amount:      tariff.Kurus(amount),
		Status:      payment.StatusSucceeded,
		RequestedAt: t0,
		PaidAt:      at,
		ConfirmedAt: at.Add(time.Second),
	}
}

func TestPaymentStore(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID, tariffID := lotHazirla(t, pool)

	at := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	sessionID := odemeOturumu(t, pool, lotID, tariffID, "34PAY001", at)

	p := bekleyenOdeme(at, 500, benzersiz("prov"))
	key := benzersiz("idem")
	id, err := store.InsertPayment(ctx, pool, sessionID, key, hash32(1), p)
	if err != nil {
		t.Fatalf("odeme eklenemedi: %v", err)
	}

	sp, err := store.GetPaymentByIdempotencyKey(ctx, pool, key)
	if err != nil {
		t.Fatalf("anahtarla okunamadi: %v", err)
	}
	if sp.ID != id || sp.SessionID != sessionID || sp.Payment.Amount != p.Amount || sp.Payment.Status != payment.StatusPending {
		t.Fatalf("okunan odeme beklenenden farkli: %+v", sp)
	}
	if sp.RequestHash != hash32(1) {
		t.Fatal("istek ozeti korunmadi")
	}

	byRef, err := store.GetPaymentByProviderRef(ctx, pool, p.ProviderRef)
	if err != nil || byRef.ID != id {
		t.Fatalf("saglayici referansiyla okunamadi: %v", err)
	}
	locked, err := store.LockPaymentByProviderRef(ctx, pool, p.ProviderRef)
	if err != nil || locked.ID != id {
		t.Fatalf("kilitli okuma basarisiz: %v", err)
	}
	lockedByID, err := store.LockPaymentByID(ctx, pool, id)
	if err != nil || lockedByID.ID != id {
		t.Fatalf("kimlikle kilitli okuma basarisiz: %v", err)
	}

	liste, err := store.ListSessionPayments(ctx, pool, sessionID)
	if err != nil {
		t.Fatalf("oturum odemeleri okunamadi: %v", err)
	}
	if len(liste) != 1 || liste[0].ID != id {
		t.Fatalf("oturum odemeleri beklenenden farkli: %+v", liste)
	}

	okunan := sp.Payment
	okunan.Status = payment.StatusSucceeded
	okunan.PaidAt = at.Add(2 * time.Minute)
	okunan.ConfirmedAt = at.Add(3 * time.Minute)
	if err := store.UpdatePaymentStatus(ctx, pool, id, okunan, false); err != nil {
		t.Fatalf("durum guncellenemedi: %v", err)
	}
	son, err := store.GetPaymentByIdempotencyKey(ctx, pool, key)
	if err != nil {
		t.Fatalf("guncel odeme okunamadi: %v", err)
	}
	if son.Payment.Status != payment.StatusSucceeded || !son.Payment.PaidAt.Equal(okunan.PaidAt) {
		t.Fatalf("guncel odeme beklenenden farkli: %+v", son.Payment)
	}
	if err := store.UpdatePaymentStatus(ctx, pool, id, okunan, false); !errors.Is(err, store.ErrPaymentStateConflict) {
		t.Fatalf("basarili odemeyi tekrar succeeded yapma hatasi = %v, beklenen ErrPaymentStateConflict", err)
	}

	if _, err := store.GetPaymentByIdempotencyKey(ctx, pool, benzersiz("yok")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("olmayan anahtar hatasi = %v, beklenen ErrNotFound", err)
	}
	if _, err := store.LockPaymentByProviderRef(ctx, pool, benzersiz("yok")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("olmayan referans hatasi = %v, beklenen ErrNotFound", err)
	}
	if _, err := store.LockPaymentByID(ctx, pool, 1<<60); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("olmayan kimlik hatasi = %v, beklenen ErrNotFound", err)
	}
}

func TestPaymentUniqueConstraints(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID, tariffID := lotHazirla(t, pool)

	at := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	sessionA := odemeOturumu(t, pool, lotID, tariffID, "34PAY002", at)
	sessionB := odemeOturumu(t, pool, lotID, tariffID, "34PAY003", at)

	key := benzersiz("idem")
	ref := benzersiz("prov")
	if _, err := store.InsertPayment(ctx, pool, sessionA, key, hash32(1), bekleyenOdeme(at, 500, ref)); err != nil {
		t.Fatalf("ilk odeme eklenemedi: %v", err)
	}

	if _, err := store.InsertPayment(ctx, pool, sessionB, key, hash32(2), bekleyenOdeme(at, 500, benzersiz("prov"))); !errors.Is(err, store.ErrIdempotencyConflict) {
		t.Fatalf("ayni anahtar hatasi = %v, beklenen ErrIdempotencyConflict", err)
	}
	if _, err := store.InsertPayment(ctx, pool, sessionA, benzersiz("idem"), hash32(1), bekleyenOdeme(at, 500, benzersiz("prov"))); !errors.Is(err, store.ErrPendingPaymentExists) {
		t.Fatalf("ikinci bekleyen odeme hatasi = %v, beklenen ErrPendingPaymentExists", err)
	}
	if _, err := store.InsertPayment(ctx, pool, sessionB, benzersiz("idem"), hash32(1), bekleyenOdeme(at, 500, ref)); !errors.Is(err, store.ErrProviderRefTaken) {
		t.Fatalf("ayni saglayici referansi hatasi = %v, beklenen ErrProviderRefTaken", err)
	}

	// Ayni oturumda basarili odemeler birlikte bulunabilir.
	if _, err := store.InsertPayment(ctx, pool, sessionB, benzersiz("idem"), hash32(1), basariliOdeme(at, 300, benzersiz("prov"))); err != nil {
		t.Fatalf("basarili odeme eklenemedi: %v", err)
	}
	if _, err := store.InsertPayment(ctx, pool, sessionB, benzersiz("idem"), hash32(1), basariliOdeme(at, 200, benzersiz("prov"))); err != nil {
		t.Fatalf("ikinci basarili odeme eklenemedi: %v", err)
	}
}

func TestListExpiredPending(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM payments WHERE status = 'pending'`); err != nil {
		t.Fatalf("bekleyen odemeler temizlenemedi: %v", err)
	}
	lotID, tariffID := lotHazirla(t, pool)

	at := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	s1 := odemeOturumu(t, pool, lotID, tariffID, "34PAY004", at)
	s2 := odemeOturumu(t, pool, lotID, tariffID, "34PAY005", at)

	id1, err := store.InsertPayment(ctx, pool, s1, benzersiz("idem"), hash32(1), bekleyenOdeme(at, 500, benzersiz("prov")))
	if err != nil {
		t.Fatalf("odeme 1: %v", err)
	}
	if _, err := store.InsertPayment(ctx, pool, s2, benzersiz("idem"), hash32(1), bekleyenOdeme(at.Add(10*time.Minute), 500, benzersiz("prov"))); err != nil {
		t.Fatalf("odeme 2: %v", err)
	}

	cutoff := at.Add(5 * time.Minute)
	items, err := store.ListExpiredPending(ctx, pool, cutoff, 10)
	if err != nil {
		t.Fatalf("expired listesi: %v", err)
	}
	if len(items) != 1 || items[0].ID != id1 {
		t.Fatalf("expired listesi beklenenden farkli: %+v", items)
	}

	// Ayni satirlar baska islemde kilitliyken SKIP LOCKED bos doner.
	tx1, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("tx1: %v", err)
	}
	defer func() { _ = tx1.Rollback(ctx) }()
	if _, err := store.ListExpiredPending(ctx, tx1, cutoff, 10); err != nil {
		t.Fatalf("tx1 liste: %v", err)
	}
	tx2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("tx2: %v", err)
	}
	defer func() { _ = tx2.Rollback(ctx) }()
	ctx2, cancel2 := context.WithTimeout(ctx, 5*time.Second)
	defer cancel2()
	items2, err := store.ListExpiredPending(ctx2, tx2, cutoff, 10)
	if err != nil {
		t.Fatalf("tx2 liste: %v", err)
	}
	if len(items2) != 0 {
		t.Fatalf("kilitli satir SKIP LOCKED ile atlanmadi: %+v", items2)
	}
}

func TestRefundStore(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID, tariffID := lotHazirla(t, pool)

	at := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	sessionID := odemeOturumu(t, pool, lotID, tariffID, "34PAY006", at)
	paymentID, err := store.InsertPayment(ctx, pool, sessionID, benzersiz("idem"), hash32(1), basariliOdeme(at, 500, benzersiz("prov")))
	if err != nil {
		t.Fatalf("odeme: %v", err)
	}

	key := benzersiz("iade")
	id, err := store.InsertRefund(ctx, pool, paymentID, 200, "manual", key, hash32(3), "", at.Add(time.Minute))
	if err != nil {
		t.Fatalf("iade eklenemedi: %v", err)
	}
	r, err := store.GetRefundByIdempotencyKey(ctx, pool, key)
	if err != nil {
		t.Fatalf("iade okunamadi: %v", err)
	}
	if r.ID != id || r.PaymentID != paymentID || r.Amount != 200 || r.Status != "pending" || r.ProviderRef != "" {
		t.Fatalf("okunan iade beklenenden farkli: %+v", r)
	}

	toplam, err := store.SumRefunds(ctx, pool, paymentID)
	if err != nil || toplam != 200 {
		t.Fatalf("iade toplami = %d (%v), beklenen 200", toplam, err)
	}

	ikinciKey := benzersiz("iade")
	ikinciID, err := store.InsertRefund(ctx, pool, paymentID, 100, "overpaid", ikinciKey, hash32(4), "", at.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("ikinci iade: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE refunds SET status = 'failed', completed_at = requested_at + interval '1 minute' WHERE id = $1`, ikinciID,
	); err != nil {
		t.Fatalf("iade durumu guncellenemedi: %v", err)
	}
	toplam, err = store.SumRefunds(ctx, pool, paymentID)
	if err != nil || toplam != 200 {
		t.Fatalf("basarisiz iade toplama katildi: %d (%v)", toplam, err)
	}

	if _, err := store.InsertRefund(ctx, pool, paymentID, 50, "manual", key, hash32(9), "", at); !errors.Is(err, store.ErrIdempotencyConflict) {
		t.Fatalf("ayni iade anahtari hatasi = %v, beklenen ErrIdempotencyConflict", err)
	}
	if _, err := store.InsertRefund(ctx, pool, paymentID, 0, "manual", benzersiz("iade"), hash32(1), "", at); err == nil {
		t.Fatal("sifir tutarli iade kabul edildi")
	}
	if _, err := store.InsertRefund(ctx, pool, paymentID, 10, "baska", benzersiz("iade"), hash32(1), "", at); err == nil {
		t.Fatal("gecersiz nedenli iade kabul edildi")
	}
	if _, err := store.GetRefundByIdempotencyKey(ctx, pool, benzersiz("yok")); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("olmayan iade hatasi = %v, beklenen ErrNotFound", err)
	}
}

func TestDebtStore(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID, tariffID := lotHazirla(t, pool)

	at := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	sessionID := odemeOturumu(t, pool, lotID, tariffID, "34PAY007", at)

	id, err := store.InsertDebt(ctx, pool, sessionID, lotID, "34PAY007", 500)
	if err != nil {
		t.Fatalf("borc eklenemedi: %v", err)
	}
	toplam, err := store.SumOpenDebtByPlate(ctx, pool, "34PAY007")
	if err != nil || toplam != 500 {
		t.Fatalf("borc toplami = %d (%v), beklenen 500", toplam, err)
	}
	liste, err := store.ListOpenDebtsByPlate(ctx, pool, "34PAY007")
	if err != nil {
		t.Fatalf("borc listesi: %v", err)
	}
	if len(liste) != 1 || liste[0].ID != id || liste[0].Remaining != 500 || liste[0].Status != "open" {
		t.Fatalf("borc listesi beklenenden farkli: %+v", liste)
	}
	if toplam, err := store.SumOpenDebtByPlate(ctx, pool, "34YOK999"); err != nil || toplam != 0 {
		t.Fatalf("olmayan plaka borcu = %d (%v), beklenen 0", toplam, err)
	}
	if _, err := store.InsertDebt(ctx, pool, sessionID, lotID, "34PAY007", 100); !errors.Is(err, store.ErrDebtExists) {
		t.Fatalf("ayni oturum borcu hatasi = %v, beklenen ErrDebtExists", err)
	}
	if _, err := store.InsertDebt(ctx, pool, sessionID, lotID, "34PAY007", 0); err == nil {
		t.Fatal("sifir tutarli borc kabul edildi")
	}

	// Bozuk bicimli plaka normalize edilerek yazilir ve sorguda bulunur.
	bozukSid := odemeOturumu(t, pool, lotID, tariffID, "34PAY008", at)
	if _, err := store.InsertDebt(ctx, pool, bozukSid, lotID, "34 pay 008", 300); err != nil {
		t.Fatalf("bicimsiz plakali borc eklenemedi: %v", err)
	}
	if toplam, err := store.SumOpenDebtByPlate(ctx, pool, "34PAY008"); err != nil || toplam != 300 {
		t.Fatalf("normalize edilmis plaka borcu = %d (%v), beklenen 300", toplam, err)
	}
}
