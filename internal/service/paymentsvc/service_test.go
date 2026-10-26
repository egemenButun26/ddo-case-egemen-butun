package paymentsvc_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"parkakil/internal/domain/payment"
	"parkakil/internal/domain/plate"
	"parkakil/internal/domain/session"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/service/paymentsvc"
	"parkakil/internal/store"
	"parkakil/internal/testdb"
)

var sayac atomic.Int64

func benzersiz(onek string) string {
	return fmt.Sprintf("%s-%d-%d", onek, time.Now().UnixNano(), sayac.Add(1))
}

type ortam struct {
	pool     *pgxpool.Pool
	svc      *paymentsvc.Service
	lotID    int64
	tariffID string
	cfg      paymentsvc.Config
	ctx      context.Context
}

func yeniOrtam(t *testing.T, freeMinutes int, bands ...tariff.Band) *ortam {
	t.Helper()
	if len(bands) == 0 {
		bands = []tariff.Band{{UnitMinutes: 60, UnitPrice: 500}}
	}
	pool := testdb.Open(t)
	lotID := testdb.NewLot(t, pool, 100)
	v := tariff.Version{
		ID:          benzersiz("tarife"),
		ValidFrom:   time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		Location:    time.UTC,
		FreeMinutes: freeMinutes,
		Weekday: tariff.DayRule{
			Name:  "hafta_ici",
			Bands: bands,
		},
	}
	if err := store.InsertTariffVersion(context.Background(), pool, lotID, v); err != nil {
		t.Fatalf("tarife eklenemedi: %v", err)
	}
	cfg := paymentsvc.Config{ProviderTimeout: 5 * time.Minute, Thresholds: plate.DefaultThresholds}
	svc, err := paymentsvc.New(pool, cfg)
	if err != nil {
		t.Fatalf("servis kurulamadi: %v", err)
	}
	return &ortam{pool: pool, svc: svc, lotID: lotID, tariffID: v.ID, cfg: cfg, ctx: context.Background()}
}

func (o *ortam) oturum(t *testing.T, p string, entered time.Time) int64 {
	t.Helper()
	ev := session.Event{
		LotID:         o.lotID,
		CameraID:      benzersiz("cam"),
		CameraEventID: benzersiz("olay"),
		Direction:     session.Entry,
		PlateRaw:      p,
		CapturedAt:    entered,
	}
	eventID, err := store.InsertEvent(o.ctx, o.pool, ev, p, session.OutcomeOpened, 0)
	if err != nil {
		t.Fatalf("giris olayi eklenemedi: %v", err)
	}
	id, err := store.InsertSession(o.ctx, o.pool, o.lotID, p, eventID, entered, o.tariffID)
	if err != nil {
		t.Fatalf("oturum eklenemedi: %v", err)
	}
	return id
}

func (o *ortam) kapat(t *testing.T, sessionID int64, at time.Time) {
	t.Helper()
	ev := session.Event{
		LotID:         o.lotID,
		CameraID:      benzersiz("cam"),
		CameraEventID: benzersiz("olay"),
		Direction:     session.Exit,
		PlateRaw:      "",
		CapturedAt:    at,
	}
	eventID, err := store.InsertEvent(o.ctx, o.pool, ev, "", session.OutcomeClosed, sessionID)
	if err != nil {
		t.Fatalf("cikis olayi eklenemedi: %v", err)
	}
	if err := store.CloseSession(o.ctx, o.pool, sessionID, eventID, at, 0, nil); err != nil {
		t.Fatalf("oturum kapatilamadi: %v", err)
	}
}

func (o *ortam) bekleyenleriTemizle(t *testing.T) {
	t.Helper()
	if _, err := o.pool.Exec(o.ctx, `DELETE FROM payments WHERE status = 'pending'`); err != nil {
		t.Fatalf("bekleyen odemeler temizlenemedi: %v", err)
	}
}

func (o *ortam) odemeEkle(t *testing.T, sessionID int64, at time.Time, amount tariff.Kurus, status payment.Status) int64 {
	t.Helper()
	var p payment.Payment
	switch status {
	case payment.StatusPending:
		p = payment.Payment{ProviderRef: benzersiz("prov"), Amount: amount, Status: status, RequestedAt: at}
	default:
		p = payment.Payment{
			ProviderRef: benzersiz("prov"),
			Amount:      amount,
			Status:      status,
			RequestedAt: at.Add(-time.Minute),
			PaidAt:      at,
			ConfirmedAt: at,
		}
	}
	id, err := store.InsertPayment(o.ctx, o.pool, sessionID, benzersiz("idem"), hash32(7), p)
	if err != nil {
		t.Fatalf("odeme eklenemedi: %v", err)
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

func basariliBildirim(ref string, at time.Time, amount tariff.Kurus) payment.Notification {
	return payment.Notification{
		ProviderRef: ref,
		Outcome:     payment.StatusSucceeded,
		Amount:      amount,
		ProviderAt:  at,
		ReceivedAt:  at.Add(time.Second),
	}
}

func TestNew(t *testing.T) {
	if _, err := paymentsvc.New(nil, paymentsvc.Config{ProviderTimeout: time.Minute}); err == nil {
		t.Fatal("nil havuz kabul edildi")
	}
	pool := testdb.Open(t)
	if _, err := paymentsvc.New(pool, paymentsvc.Config{ProviderTimeout: 0, Thresholds: plate.DefaultThresholds}); err == nil {
		t.Fatal("sifir zaman asimi kabul edildi")
	}
	if _, err := paymentsvc.New(pool, paymentsvc.Config{ProviderTimeout: time.Minute, Thresholds: plate.Thresholds{Auto: 100, Review: 500}}); err == nil {
		t.Fatal("tutarsiz esikler kabul edildi")
	}
}

func TestQuote(t *testing.T) {
	o := yeniOrtam(t, 15)
	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	sid := o.oturum(t, "34QTE001", t0)

	q, err := o.svc.Quote(o.ctx, sid, t0.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("ilk 15 dk teklifi: %v", err)
	}
	if q.Total != 0 || q.Paid != 0 || q.Due != 0 || !q.PermitExpiresAt.IsZero() {
		t.Fatalf("ilk 15 dk teklifi beklenenden farkli: %+v", q)
	}

	q, err = o.svc.Quote(o.ctx, sid, t0.Add(time.Hour))
	if err != nil {
		t.Fatalf("1 saat teklifi: %v", err)
	}
	if q.Total != 500 || q.Due != 500 {
		t.Fatalf("1 saat teklifi beklenenden farkli: %+v", q)
	}

	o.odemeEkle(t, sid, t0.Add(time.Hour), 200, payment.StatusSucceeded)
	q, err = o.svc.Quote(o.ctx, sid, t0.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("kismi odeme teklifi: %v", err)
	}
	if q.Total != 1000 || q.Paid != 200 || q.Due != 800 {
		t.Fatalf("kismi odeme teklifi beklenenden farkli: %+v", q)
	}

	kapali := o.oturum(t, "34QTE002", t0)
	o.kapat(t, kapali, t0.Add(30*time.Minute))
	if _, err := o.svc.Quote(o.ctx, kapali, t0.Add(time.Hour)); !errors.Is(err, store.ErrSessionNotOpen) {
		t.Fatalf("kapali oturum hatasi = %v, beklenen ErrSessionNotOpen", err)
	}
	if _, err := o.svc.Quote(o.ctx, 1<<60, t0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("olmayan oturum hatasi = %v, beklenen ErrNotFound", err)
	}
	if _, err := o.svc.Quote(o.ctx, 0, t0); err == nil {
		t.Fatal("gecersiz oturum kimligi kabul edildi")
	}
}

func TestCreatePayment(t *testing.T) {
	o := yeniOrtam(t, 15)
	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	sid := o.oturum(t, "34CRE001", t0)
	now := t0.Add(time.Hour)
	req := paymentsvc.CreateReq{
		SessionID:      sid,
		Amount:         500,
		IdempotencyKey: benzersiz("idem"),
		RequestHash:    hash32(1),
		ProviderRef:    benzersiz("prov"),
		Now:            now,
	}
	res, err := o.svc.CreatePayment(o.ctx, req)
	if err != nil {
		t.Fatalf("odeme olusturulamadi: %v", err)
	}
	if res.PaymentID == 0 || res.Replayed || res.Payment.Status != payment.StatusPending || res.Payment.Amount != 500 {
		t.Fatalf("odeme sonucu beklenenden farkli: %+v", res)
	}
	if !res.Payment.RequestedAt.Equal(now) {
		t.Fatalf("istek zamani = %v, beklenen %v", res.Payment.RequestedAt, now)
	}

	tekrar, err := o.svc.CreatePayment(o.ctx, req)
	if err != nil {
		t.Fatalf("tekrar istek: %v", err)
	}
	if !tekrar.Replayed || tekrar.PaymentID != res.PaymentID {
		t.Fatalf("tekrar istek beklenenden farkli: %+v", tekrar)
	}

	farkli := req
	farkli.RequestHash = hash32(2)
	if _, err := o.svc.CreatePayment(o.ctx, farkli); !errors.Is(err, store.ErrIdempotencyConflict) {
		t.Fatalf("farkli ozet hatasi = %v, beklenen ErrIdempotencyConflict", err)
	}

	ikinci := req
	ikinci.IdempotencyKey = benzersiz("idem")
	ikinci.ProviderRef = benzersiz("prov")
	if _, err := o.svc.CreatePayment(o.ctx, ikinci); !errors.Is(err, store.ErrPendingPaymentExists) {
		t.Fatalf("bekleyen varken ikinci odeme hatasi = %v, beklenen ErrPendingPaymentExists", err)
	}

	yanlis := ikinci
	yanlis.Amount = 400
	yanlis.IdempotencyKey = benzersiz("idem")
	if _, err := o.svc.CreatePayment(o.ctx, yanlis); !errors.Is(err, payment.ErrAmountMismatch) {
		t.Fatalf("yanlis tutar hatasi = %v, beklenen ErrAmountMismatch", err)
	}

	bos := o.oturum(t, "34CRE002", t0)
	if _, err := o.svc.CreatePayment(o.ctx, paymentsvc.CreateReq{
		SessionID: bos, Amount: 100, IdempotencyKey: benzersiz("idem"),
		RequestHash: hash32(1), ProviderRef: benzersiz("prov"), Now: t0.Add(10 * time.Minute),
	}); !errors.Is(err, paymentsvc.ErrNothingDue) {
		t.Fatalf("borcsuz oturum hatasi = %v, beklenen ErrNothingDue", err)
	}

	kapali := o.oturum(t, "34CRE003", t0)
	o.kapat(t, kapali, t0.Add(30*time.Minute))
	if _, err := o.svc.CreatePayment(o.ctx, paymentsvc.CreateReq{
		SessionID: kapali, Amount: 500, IdempotencyKey: benzersiz("idem"),
		RequestHash: hash32(1), ProviderRef: benzersiz("prov"), Now: t0.Add(time.Hour),
	}); !errors.Is(err, store.ErrSessionNotOpen) {
		t.Fatalf("kapali oturum odemesi hatasi = %v, beklenen ErrSessionNotOpen", err)
	}

	gecersizler := []paymentsvc.CreateReq{
		{SessionID: sid, Amount: 0, IdempotencyKey: benzersiz("idem"), ProviderRef: "p", Now: now},
		{SessionID: sid, Amount: 500, IdempotencyKey: "", ProviderRef: "p", Now: now},
		{SessionID: sid, Amount: 500, IdempotencyKey: benzersiz("idem"), ProviderRef: "", Now: now},
		{SessionID: sid, Amount: 500, IdempotencyKey: benzersiz("idem"), ProviderRef: "p", Now: time.Time{}},
		{SessionID: 0, Amount: 500, IdempotencyKey: benzersiz("idem"), ProviderRef: "p", Now: now},
	}
	for i, r := range gecersizler {
		if _, err := o.svc.CreatePayment(o.ctx, r); err == nil {
			t.Fatalf("gecersiz istek %d kabul edildi", i)
		}
	}
}

func TestCreatePaymentConcurrentSameKey(t *testing.T) {
	o := yeniOrtam(t, 15)
	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	sid := o.oturum(t, "34CRC001", t0)
	req := paymentsvc.CreateReq{
		SessionID:      sid,
		Amount:         500,
		IdempotencyKey: benzersiz("idem"),
		RequestHash:    hash32(5),
		ProviderRef:    benzersiz("prov"),
		Now:            t0.Add(time.Hour),
	}

	const n = 20
	basla := make(chan struct{})
	var wg sync.WaitGroup
	ids := make([]int64, n)
	errs := make([]error, n)
	replayed := make([]bool, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-basla
			res, err := o.svc.CreatePayment(o.ctx, req)
			ids[i] = res.PaymentID
			replayed[i] = res.Replayed
			errs[i] = err
		}(i)
	}
	close(basla)
	wg.Wait()

	var ilk int64
	gecis := 0
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("eszamanli istek %d hatasi: %v", i, errs[i])
		}
		if i == 0 {
			ilk = ids[i]
		}
		if ids[i] != ilk {
			t.Fatalf("farkli odeme kimlikleri: %v", ids)
		}
		if !replayed[i] {
			gecis++
		}
	}
	if gecis != 1 {
		t.Fatalf("yeni odeme sayisi = %d, beklenen 1", gecis)
	}
	liste, err := store.ListSessionPayments(o.ctx, o.pool, sid)
	if err != nil {
		t.Fatalf("odemeler okunamadi: %v", err)
	}
	if len(liste) != 1 {
		t.Fatalf("eszamanli ayni anahtar %d satir yazdi, beklenen 1", len(liste))
	}
}

func TestCreatePaymentConcurrentDifferentKeys(t *testing.T) {
	o := yeniOrtam(t, 15)
	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	sid := o.oturum(t, "34CRC002", t0)

	const n = 20
	basla := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-basla
			_, err := o.svc.CreatePayment(o.ctx, paymentsvc.CreateReq{
				SessionID:      sid,
				Amount:         500,
				IdempotencyKey: benzersiz("idem"),
				RequestHash:    hash32(byte(i)),
				ProviderRef:    benzersiz("prov"),
				Now:            t0.Add(time.Hour),
			})
			errs[i] = err
		}(i)
	}
	close(basla)
	wg.Wait()

	basarili := 0
	bekleyen := 0
	for i := 0; i < n; i++ {
		switch {
		case errs[i] == nil:
			basarili++
		case errors.Is(errs[i], store.ErrPendingPaymentExists):
			bekleyen++
		default:
			t.Fatalf("beklenmeyen hata %d: %v", i, errs[i])
		}
	}
	if basarili != 1 || bekleyen != n-1 {
		t.Fatalf("basarili=%d bekleyen=%d, beklenen 1/%d", basarili, bekleyen, n-1)
	}
}

func TestApplyNotification(t *testing.T) {
	o := yeniOrtam(t, 15)
	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	sid := o.oturum(t, "34APL001", t0)
	now := t0.Add(time.Hour)

	res, err := o.svc.CreatePayment(o.ctx, paymentsvc.CreateReq{
		SessionID: sid, Amount: 500, IdempotencyKey: benzersiz("idem"),
		RequestHash: hash32(1), ProviderRef: benzersiz("prov"), Now: now.Add(-2 * time.Second),
	})
	if err != nil {
		t.Fatalf("odeme: %v", err)
	}

	n := basariliBildirim(res.Payment.ProviderRef, now.Add(-time.Second), 500)
	uyg, err := o.svc.ApplyNotification(o.ctx, n)
	if err != nil {
		t.Fatalf("bildirim uygulanamadi: %v", err)
	}
	if uyg.Payment.Status != payment.StatusSucceeded || uyg.Transition.Duplicate || uyg.RefundRequired || uyg.Overpaid != 0 {
		t.Fatalf("bildirim sonucu beklenenden farkli: %+v", uyg)
	}
	if !uyg.Payment.PaidAt.Equal(n.ProviderAt) || !uyg.Payment.ConfirmedAt.Equal(n.ReceivedAt) {
		t.Fatalf("odeme zamanlari beklenenden farkli: %+v", uyg.Payment)
	}

	tekrar, err := o.svc.ApplyNotification(o.ctx, n)
	if err != nil {
		t.Fatalf("yinelenen bildirim: %v", err)
	}
	if !tekrar.Transition.Duplicate || !tekrar.Payment.PaidAt.Equal(n.ProviderAt) {
		t.Fatalf("yinelenen bildirim beklenenden farkli: %+v", tekrar)
	}

	karar, gotSession, err := o.svc.DecideGate(o.ctx, o.lotID, "34APL001", now)
	if err != nil {
		t.Fatalf("kapi karari: %v", err)
	}
	if gotSession != sid || karar.Action != payment.ActionOpen || karar.Reason != payment.ReasonPaid {
		t.Fatalf("odeme sonrasi karar beklenenden farkli: %+v", karar)
	}
	karar, _, err = o.svc.DecideGate(o.ctx, o.lotID, "34APL001", now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("izin karari: %v", err)
	}
	if karar.Action != payment.ActionOpen || karar.Reason != payment.ReasonPermitValid {
		t.Fatalf("izin karari beklenenden farkli: %+v", karar)
	}
	if !karar.PermitExpiresAt.Equal(n.ProviderAt.Add(payment.PermitWindow)) {
		t.Fatalf("izin bitisi = %v, beklenen %v", karar.PermitExpiresAt, n.ProviderAt.Add(payment.PermitWindow))
	}

	if _, err := o.svc.ApplyNotification(o.ctx, basariliBildirim(benzersiz("yok"), now, 500)); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("olmayan referans hatasi = %v, beklenen ErrNotFound", err)
	}
	if _, err := o.svc.ApplyNotification(o.ctx, payment.Notification{
		ProviderRef: "", Outcome: payment.StatusSucceeded, Amount: 500,
		ProviderAt: now, ReceivedAt: now,
	}); !errors.Is(err, payment.ErrInvalidNotification) {
		t.Fatalf("bos referans hatasi = %v, beklenen ErrInvalidNotification", err)
	}

	yanlisTutar := basariliBildirim(res.Payment.ProviderRef, now.Add(10*time.Second), 400)
	if _, err := o.svc.ApplyNotification(o.ctx, yanlisTutar); !errors.Is(err, payment.ErrAmountMismatch) {
		t.Fatalf("tutar uyusmazligi hatasi = %v, beklenen ErrAmountMismatch", err)
	}

	basarisiz := o.oturum(t, "34APL002", t0)
	p2, err := o.svc.CreatePayment(o.ctx, paymentsvc.CreateReq{
		SessionID: basarisiz, Amount: 500, IdempotencyKey: benzersiz("idem"),
		RequestHash: hash32(2), ProviderRef: benzersiz("prov"), Now: now,
	})
	if err != nil {
		t.Fatalf("ikinci odeme: %v", err)
	}
	f, err := o.svc.ApplyNotification(o.ctx, payment.Notification{
		ProviderRef: p2.Payment.ProviderRef,
		Outcome:     payment.StatusFailed,
		Amount:      500,
		ProviderAt:  now.Add(time.Second),
		ReceivedAt:  now.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("basarisiz bildirim: %v", err)
	}
	if f.Payment.Status != payment.StatusFailed || f.RefundRequired {
		t.Fatalf("basarisiz bildirim sonucu beklenenden farkli: %+v", f)
	}
	if _, err := o.svc.ApplyNotification(o.ctx, basariliBildirim(p2.Payment.ProviderRef, now.Add(3*time.Second), 500)); !errors.Is(err, payment.ErrNotificationConflict) {
		t.Fatalf("celisen bildirim hatasi = %v, beklenen ErrNotificationConflict", err)
	}
}

func TestApplyNotificationLateApproval(t *testing.T) {
	o := yeniOrtam(t, 15, tariff.Band{UnitMinutes: 240, UnitPrice: 500})
	o.bekleyenleriTemizle(t)
	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	sid := o.oturum(t, "34LAT001", t0)
	at := t0.Add(time.Hour)

	res, err := o.svc.CreatePayment(o.ctx, paymentsvc.CreateReq{
		SessionID: sid, Amount: 500, IdempotencyKey: benzersiz("idem"),
		RequestHash: hash32(1), ProviderRef: benzersiz("prov"), Now: at,
	})
	if err != nil {
		t.Fatalf("odeme: %v", err)
	}
	n, err := o.svc.ExpirePending(o.ctx, at.Add(5*time.Minute))
	if err != nil || n != 1 {
		t.Fatalf("expire = %d (%v), beklenen 1", n, err)
	}

	uyg, err := o.svc.ApplyNotification(o.ctx, payment.Notification{
		ProviderRef: res.Payment.ProviderRef,
		Outcome:     payment.StatusSucceeded,
		Amount:      500,
		ProviderAt:  at.Add(time.Second),
		ReceivedAt:  at.Add(5*time.Minute + time.Second),
	})
	if err != nil {
		t.Fatalf("gec onay: %v", err)
	}
	if !uyg.Transition.LateApproval || uyg.Payment.Status != payment.StatusSucceeded || uyg.RefundRequired {
		t.Fatalf("gec onay sonucu beklenenden farkli: %+v", uyg)
	}
}

func TestApplyNotificationDoublePayment(t *testing.T) {
	o := yeniOrtam(t, 15, tariff.Band{UnitMinutes: 240, UnitPrice: 500})
	o.bekleyenleriTemizle(t)
	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	sid := o.oturum(t, "34DBL001", t0)
	at := t0.Add(time.Hour)

	a, err := o.svc.CreatePayment(o.ctx, paymentsvc.CreateReq{
		SessionID: sid, Amount: 500, IdempotencyKey: benzersiz("idem"),
		RequestHash: hash32(1), ProviderRef: benzersiz("prov"), Now: at,
	})
	if err != nil {
		t.Fatalf("ilk odeme: %v", err)
	}
	if n, err := o.svc.ExpirePending(o.ctx, at.Add(5*time.Minute)); err != nil || n != 1 {
		t.Fatalf("expire = %d (%v)", n, err)
	}

	b, err := o.svc.CreatePayment(o.ctx, paymentsvc.CreateReq{
		SessionID: sid, Amount: 500, IdempotencyKey: benzersiz("idem"),
		RequestHash: hash32(2), ProviderRef: benzersiz("prov"), Now: at.Add(6 * time.Minute),
	})
	if err != nil {
		t.Fatalf("ikinci odeme: %v", err)
	}
	if _, err := o.svc.ApplyNotification(o.ctx, basariliBildirim(b.Payment.ProviderRef, at.Add(7*time.Minute), 500)); err != nil {
		t.Fatalf("ikinci odeme bildirimi: %v", err)
	}

	gec, err := o.svc.ApplyNotification(o.ctx, payment.Notification{
		ProviderRef: a.Payment.ProviderRef,
		Outcome:     payment.StatusSucceeded,
		Amount:      500,
		ProviderAt:  at.Add(time.Second),
		ReceivedAt:  at.Add(8 * time.Minute),
	})
	if err != nil {
		t.Fatalf("gec onay: %v", err)
	}
	if !gec.Transition.LateApproval || !gec.RefundRequired || gec.Overpaid != 500 {
		t.Fatalf("cift odeme sonucu beklenenden farkli: %+v", gec)
	}
}

func TestApplyNotificationConcurrent(t *testing.T) {
	o := yeniOrtam(t, 15)
	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	sid := o.oturum(t, "34CON001", t0)
	now := t0.Add(time.Hour)
	res, err := o.svc.CreatePayment(o.ctx, paymentsvc.CreateReq{
		SessionID: sid, Amount: 500, IdempotencyKey: benzersiz("idem"),
		RequestHash: hash32(1), ProviderRef: benzersiz("prov"), Now: now,
	})
	if err != nil {
		t.Fatalf("odeme: %v", err)
	}
	n := basariliBildirim(res.Payment.ProviderRef, now.Add(5*time.Second), 500)

	const sayi = 10
	basla := make(chan struct{})
	var wg sync.WaitGroup
	duplicate := make([]bool, sayi)
	errs := make([]error, sayi)
	for i := 0; i < sayi; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-basla
			uyg, err := o.svc.ApplyNotification(o.ctx, n)
			if err == nil {
				duplicate[i] = uyg.Transition.Duplicate
			}
			errs[i] = err
		}(i)
	}
	close(basla)
	wg.Wait()

	gecis := 0
	for i := 0; i < sayi; i++ {
		if errs[i] != nil {
			t.Fatalf("eszamanli bildirim %d hatasi: %v", i, errs[i])
		}
		if !duplicate[i] {
			gecis++
		}
	}
	if gecis != 1 {
		t.Fatalf("durum gecisi %d kez oldu, beklenen 1", gecis)
	}
}

func TestExpirePending(t *testing.T) {
	o := yeniOrtam(t, 15)
	o.bekleyenleriTemizle(t)
	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	sid := o.oturum(t, "34EXP001", t0)
	at := t0.Add(time.Hour)
	pid := o.odemeEkle(t, sid, at, 500, payment.StatusPending)

	if n, err := o.svc.ExpirePending(o.ctx, at.Add(5*time.Minute-time.Second)); err != nil || n != 0 {
		t.Fatalf("sinir oncesi expire = %d (%v), beklenen 0", n, err)
	}
	if n, err := o.svc.ExpirePending(o.ctx, at.Add(5*time.Minute)); err != nil || n != 1 {
		t.Fatalf("sinir expire = %d (%v), beklenen 1", n, err)
	}
	sp, err := store.LockPaymentByID(o.ctx, o.pool, pid)
	if err != nil {
		t.Fatalf("odeme okunamadi: %v", err)
	}
	if sp.Payment.Status != payment.StatusTimedOut {
		t.Fatalf("odeme durumu = %q, beklenen timed_out", sp.Payment.Status)
	}
}

func TestExpirePendingConcurrent(t *testing.T) {
	o := yeniOrtam(t, 15)
	o.bekleyenleriTemizle(t)
	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	sid := o.oturum(t, "34EXP002", t0)
	at := t0.Add(time.Hour)
	pid := o.odemeEkle(t, sid, at, 500, payment.StatusPending)
	simdi := at.Add(5 * time.Minute)

	basla := make(chan struct{})
	var wg sync.WaitGroup
	sayilar := make([]int, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-basla
			sayilar[i], errs[i] = o.svc.ExpirePending(o.ctx, simdi)
		}(i)
	}
	close(basla)
	wg.Wait()

	toplam := 0
	for i := 0; i < 2; i++ {
		if errs[i] != nil {
			t.Fatalf("eszamanli expire %d hatasi: %v", i, errs[i])
		}
		toplam += sayilar[i]
	}
	if toplam != 1 {
		t.Fatalf("eszamanli expire toplami = %d, beklenen 1", toplam)
	}
	sp, err := store.LockPaymentByID(o.ctx, o.pool, pid)
	if err != nil {
		t.Fatalf("odeme okunamadi: %v", err)
	}
	if sp.Payment.Status != payment.StatusTimedOut {
		t.Fatalf("odeme durumu = %q, beklenen timed_out", sp.Payment.Status)
	}
}

func TestDecideGate(t *testing.T) {
	o := yeniOrtam(t, 15)
	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	sid := o.oturum(t, "34ABC34", t0)

	d, got, err := o.svc.DecideGate(o.ctx, o.lotID, "34ZZZ99", t0.Add(time.Hour))
	if err != nil {
		t.Fatalf("bilinmeyen plaka: %v", err)
	}
	if d.Action != payment.ActionDeny || d.Reason != payment.ReasonNoMatch || got != 0 {
		t.Fatalf("bilinmeyen plaka karari beklenenden farkli: %+v (oturum %d)", d, got)
	}

	d, got, err = o.svc.DecideGate(o.ctx, o.lotID, "34ABC34", t0.Add(time.Hour))
	if err != nil {
		t.Fatalf("odeme gerekli karari: %v", err)
	}
	if got != sid || d.Action != payment.ActionDeny || d.Reason != payment.ReasonPaymentRequired || d.Due != 500 {
		t.Fatalf("odeme gerekli karari beklenenden farkli: %+v (oturum %d)", d, got)
	}

	o.odemeEkle(t, sid, t0.Add(time.Hour), 500, payment.StatusSucceeded)
	d, _, err = o.svc.DecideGate(o.ctx, o.lotID, "34ABC34", t0.Add(time.Hour))
	if err != nil {
		t.Fatalf("odendi karari: %v", err)
	}
	if d.Action != payment.ActionOpen || d.Reason != payment.ReasonPaid {
		t.Fatalf("odendi karari beklenenden farkli: %+v", d)
	}

	d, _, err = o.svc.DecideGate(o.ctx, o.lotID, "34ABC34", t0.Add(time.Hour+time.Minute))
	if err != nil {
		t.Fatalf("izin karari: %v", err)
	}
	if d.Action != payment.ActionOpen || d.Reason != payment.ReasonPermitValid {
		t.Fatalf("izin karari beklenenden farkli: %+v", d)
	}

	d, _, err = o.svc.DecideGate(o.ctx, o.lotID, "34ABC34", t0.Add(time.Hour+16*time.Minute))
	if err != nil {
		t.Fatalf("izin doldu karari: %v", err)
	}
	if d.Action != payment.ActionDeny || d.Reason != payment.ReasonPermitExpired || d.Due != 500 {
		t.Fatalf("izin doldu karari beklenenden farkli: %+v", d)
	}

	// Onceki borc ayri raporlanir, Due'ya eklenmez.
	borcSid := o.oturum(t, "34XYZ34", t0)
	if _, err := store.InsertDebt(o.ctx, o.pool, borcSid, o.lotID, "34XYZ34", 250); err != nil {
		t.Fatalf("borc eklenemedi: %v", err)
	}
	d, got, err = o.svc.DecideGate(o.ctx, o.lotID, "34XYZ34", t0.Add(time.Hour))
	if err != nil {
		t.Fatalf("borc karari: %v", err)
	}
	if got != borcSid || d.Action != payment.ActionDeny || d.Reason != payment.ReasonPriorDebt {
		t.Fatalf("borc karari beklenenden farkli: %+v (oturum %d)", d, got)
	}
	if d.PriorDebt != 250 || d.Due != 500 {
		t.Fatalf("borc ve kalan ayri olmali: Due=%d PriorDebt=%d", d.Due, d.PriorDebt)
	}
}

func TestDecideGateP95(t *testing.T) {
	o := yeniOrtam(t, 15)
	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	harfler := "CFJKPRWY"
	plakalar := make([]string, 50)
	for i := 0; i < 50; i++ {
		p := fmt.Sprintf("34%s%s01", string(harfler[i/8]), string(harfler[i%8]))
		plakalar[i] = p
		sid := o.oturum(t, p, t0)
		for j := 0; j < 4; j++ {
			o.odemeEkle(t, sid, t0.Add(time.Hour), 100, payment.StatusSucceeded)
		}
	}

	simdi := t0.Add(2 * time.Hour)
	for i := 0; i < 10; i++ {
		if _, _, err := o.svc.DecideGate(o.ctx, o.lotID, plakalar[i], simdi); err != nil {
			t.Fatalf("isinma cagrisi: %v", err)
		}
	}

	const cagri = 500
	sureler := make([]time.Duration, cagri)
	for i := 0; i < cagri; i++ {
		p := plakalar[i%len(plakalar)]
		bas := time.Now()
		if _, _, err := o.svc.DecideGate(o.ctx, o.lotID, p, simdi); err != nil {
			t.Fatalf("cagri %d: %v", i, err)
		}
		sureler[i] = time.Since(bas)
	}
	sort.Slice(sureler, func(i, j int) bool { return sureler[i] < sureler[j] })
	p95 := sureler[int(math.Ceil(0.95*float64(cagri)))-1]
	t.Logf("DecideGate p95 = %v (500 cagri)", p95)
	if p95 >= 50*time.Millisecond {
		t.Fatalf("DecideGate p95 = %v, esik 50ms", p95)
	}
}

func TestCreateRefund(t *testing.T) {
	o := yeniOrtam(t, 15)
	t0 := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	sid := o.oturum(t, "34REF001", t0)
	at := t0.Add(time.Hour)
	res, err := o.svc.CreatePayment(o.ctx, paymentsvc.CreateReq{
		SessionID: sid, Amount: 500, IdempotencyKey: benzersiz("idem"),
		RequestHash: hash32(1), ProviderRef: benzersiz("prov"), Now: at,
	})
	if err != nil {
		t.Fatalf("odeme: %v", err)
	}
	if _, err := o.svc.ApplyNotification(o.ctx, basariliBildirim(res.Payment.ProviderRef, at.Add(time.Second), 500)); err != nil {
		t.Fatalf("bildirim: %v", err)
	}

	key := benzersiz("iade")
	req := paymentsvc.RefundReq{
		PaymentID: res.PaymentID, Amount: 200, Reason: "manual",
		IdempotencyKey: key, RequestHash: hash32(3), Now: at.Add(time.Minute),
	}
	iade, err := o.svc.CreateRefund(o.ctx, req)
	if err != nil {
		t.Fatalf("iade olusturulamadi: %v", err)
	}
	if iade.Replayed || iade.Refund.Status != "pending" || iade.Refund.Amount != 200 {
		t.Fatalf("iade sonucu beklenenden farkli: %+v", iade)
	}
	tekrar, err := o.svc.CreateRefund(o.ctx, req)
	if err != nil {
		t.Fatalf("tekrar iade: %v", err)
	}
	if !tekrar.Replayed || tekrar.Refund.ID != iade.Refund.ID {
		t.Fatalf("tekrar iade beklenenden farkli: %+v", tekrar)
	}

	farkli := req
	farkli.RequestHash = hash32(4)
	if _, err := o.svc.CreateRefund(o.ctx, farkli); !errors.Is(err, store.ErrIdempotencyConflict) {
		t.Fatalf("farkli ozetli tekrar hatasi = %v, beklenen ErrIdempotencyConflict", err)
	}

	fazla := req
	fazla.IdempotencyKey = benzersiz("iade")
	fazla.Amount = 400
	if _, err := o.svc.CreateRefund(o.ctx, fazla); !errors.Is(err, paymentsvc.ErrRefundExceedsPayment) {
		t.Fatalf("fazla iade hatasi = %v, beklenen ErrRefundExceedsPayment", err)
	}

	kalan := req
	kalan.IdempotencyKey = benzersiz("iade")
	kalan.Amount = 300
	if _, err := o.svc.CreateRefund(o.ctx, kalan); err != nil {
		t.Fatalf("kalan tutarli iade: %v", err)
	}

	gecersiz := []paymentsvc.RefundReq{
		{PaymentID: res.PaymentID, Amount: 0, Reason: "manual", IdempotencyKey: benzersiz("iade"), Now: at},
		{PaymentID: res.PaymentID, Amount: 10, Reason: "baska", IdempotencyKey: benzersiz("iade"), Now: at},
		{PaymentID: res.PaymentID, Amount: 10, Reason: "manual", IdempotencyKey: "", Now: at},
		{PaymentID: res.PaymentID, Amount: 10, Reason: "manual", IdempotencyKey: benzersiz("iade"), Now: time.Time{}},
		{PaymentID: 0, Amount: 10, Reason: "manual", IdempotencyKey: benzersiz("iade"), Now: at},
	}
	for i, r := range gecersiz {
		if _, err := o.svc.CreateRefund(o.ctx, r); err == nil {
			t.Fatalf("gecersiz iade %d kabul edildi", i)
		}
	}

	if _, err := o.svc.CreateRefund(o.ctx, paymentsvc.RefundReq{
		PaymentID: 1 << 60, Amount: 10, Reason: "manual",
		IdempotencyKey: benzersiz("iade"), RequestHash: hash32(1), Now: at,
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("olmayan odeme iadesi hatasi = %v, beklenen ErrNotFound", err)
	}

	bekleyenSid := o.oturum(t, "34REF002", t0)
	bekleyen, err := o.svc.CreatePayment(o.ctx, paymentsvc.CreateReq{
		SessionID: bekleyenSid, Amount: 500, IdempotencyKey: benzersiz("idem"),
		RequestHash: hash32(5), ProviderRef: benzersiz("prov"), Now: at,
	})
	if err != nil {
		t.Fatalf("bekleyen odeme: %v", err)
	}
	if _, err := o.svc.CreateRefund(o.ctx, paymentsvc.RefundReq{
		PaymentID: bekleyen.PaymentID, Amount: 100, Reason: "manual",
		IdempotencyKey: benzersiz("iade"), RequestHash: hash32(6), Now: at,
	}); !errors.Is(err, paymentsvc.ErrRefundNotSucceeded) {
		t.Fatalf("basarisiz odeme iadesi hatasi = %v, beklenen ErrRefundNotSucceeded", err)
	}
}
