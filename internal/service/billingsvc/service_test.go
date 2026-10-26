package billingsvc_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"parkakil/internal/domain/billing"
	"parkakil/internal/domain/tariff"
	"parkakil/internal/service/billingsvc"
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

func yeniServis(t *testing.T) (*pgxpool.Pool, *billingsvc.Service) {
	t.Helper()
	pool := testdb.Open(t)
	svc, err := billingsvc.New(pool)
	if err != nil {
		t.Fatalf("servis olusturulamadi: %v", err)
	}
	return pool, svc
}

func planOlustur(t *testing.T, pool *pgxpool.Pool, fiyat tariff.Kurus) string {
	t.Helper()
	lotID := testdb.NewLot(t, pool, 100)
	code := benzersiz("plan")
	if _, err := store.InsertPlan(context.Background(), pool, code, lotID, fiyat); err != nil {
		t.Fatalf("plan eklenemedi: %v", err)
	}
	return code
}

func faturalar(t *testing.T, pool *pgxpool.Pool, subID int64) []store.Invoice {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx, `SELECT id FROM invoices WHERE subscription_id = $1 ORDER BY id`, subID)
	if err != nil {
		t.Fatalf("faturalar okunamadi: %v", err)
	}
	var idler []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("fatura id okunamadi: %v", err)
		}
		idler = append(idler, id)
	}
	rows.Close()
	var sonuc []store.Invoice
	for _, id := range idler {
		inv, err := store.GetInvoice(ctx, pool, id)
		if err != nil {
			t.Fatalf("fatura okunamadi: %v", err)
		}
		sonuc = append(sonuc, inv)
	}
	return sonuc
}

func TestSubscribeCancel(t *testing.T) {
	pool, svc := yeniServis(t)
	ctx := context.Background()
	plan := planOlustur(t, pool, 3000)

	plaka := benzersiz("34SUB")
	sub, err := svc.Subscribe(ctx, plaka, plan, an(t, "2026-04-15 13:00:00"))
	if err != nil {
		t.Fatalf("abonelik acilamadi: %v", err)
	}
	if sub.Status != billing.StatusActive || sub.Plate != plaka || sub.MonthlyPrice != 3000 {
		t.Fatalf("abonelik alanlari yanlis: %+v", sub)
	}
	if !sub.CurrentPeriodStart.Equal(tarihUTC(2026, 4, 1)) || !sub.CurrentPeriodEnd.Equal(tarihUTC(2026, 5, 1)) {
		t.Fatalf("donem takvim ayi degil: %v - %v", sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
	}
	if _, err := svc.Subscribe(ctx, plaka, plan, an(t, "2026-04-20 00:00:00")); !errors.Is(err, store.ErrSubscriptionExists) {
		t.Fatalf("cift abonelik hatasi = %v, beklenen ErrSubscriptionExists", err)
	}

	c, err := svc.Cancel(ctx, sub.ID, an(t, "2026-04-10 00:00:00"))
	if err != nil {
		t.Fatalf("iptal edilemedi: %v", err)
	}
	if c.Credit != 2100 || c.InvoiceID == 0 {
		t.Fatalf("iptal sonucu yanlis: %+v", c)
	}
	inv, err := store.GetInvoice(ctx, pool, c.InvoiceID)
	if err != nil {
		t.Fatalf("kredi faturasi okunamadi: %v", err)
	}
	if inv.Kind != store.InvoiceKindProrationCredit || inv.Amount != 2100 || inv.Status != store.InvoiceStatusOpen {
		t.Fatalf("kredi faturasi yanlis: %+v", inv)
	}
	iptal, err := store.GetSubscription(ctx, pool, sub.ID)
	if err != nil {
		t.Fatalf("abonelik okunamadi: %v", err)
	}
	if iptal.Status != billing.StatusCancelled || iptal.CancelledAt == nil {
		t.Fatalf("abonelik iptal durumu yanlis: %+v", iptal)
	}
	if _, err := svc.Cancel(ctx, sub.ID, an(t, "2026-04-11 00:00:00")); !errors.Is(err, store.ErrSubscriptionCancelled) {
		t.Fatalf("ikinci iptal hatasi = %v, beklenen ErrSubscriptionCancelled", err)
	}

	if _, err := svc.Subscribe(ctx, "  ", plan, an(t, "2026-04-01 00:00:00")); !errors.Is(err, billingsvc.ErrInvalidPlate) {
		t.Fatalf("bos plaka hatasi = %v, beklenen ErrInvalidPlate", err)
	}
	if _, err := svc.Subscribe(ctx, benzersiz("34SUB"), benzersiz("yok"), an(t, "2026-04-01 00:00:00")); !errors.Is(err, store.ErrPlanNotFound) {
		t.Fatalf("olmayan plan hatasi = %v, beklenen ErrPlanNotFound", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE plans SET active = false WHERE code = $1`, plan); err != nil {
		t.Fatalf("plan pasif yapilamadi: %v", err)
	}
	if _, err := svc.Subscribe(ctx, benzersiz("34SUB"), plan, an(t, "2026-04-01 00:00:00")); !errors.Is(err, billingsvc.ErrInvalidPlan) {
		t.Fatalf("pasif plan hatasi = %v, beklenen ErrInvalidPlan", err)
	}
}

func tarihUTC(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestChangePlan(t *testing.T) {
	pool, svc := yeniServis(t)
	ctx := context.Background()
	ucuz := planOlustur(t, pool, 3000)
	pahali := planOlustur(t, pool, 6000)

	t.Run("yukseltme", func(t *testing.T) {
		sub, err := svc.Subscribe(ctx, benzersiz("34CHG"), ucuz, an(t, "2026-04-01 00:00:00"))
		if err != nil {
			t.Fatalf("abonelik acilamadi: %v", err)
		}
		r, err := svc.ChangePlan(ctx, sub.ID, pahali, an(t, "2026-04-10 00:00:00"))
		if err != nil {
			t.Fatalf("plan degistirilemedi: %v", err)
		}
		if r.Charge != 4200 || r.Credit != 2100 || r.Net != 2100 || r.Direction != "upgrade" {
			t.Fatalf("degisim yanlis: %+v", r)
		}
		charge, err := store.GetInvoice(ctx, pool, r.ChargeInvoiceID)
		if err != nil {
			t.Fatalf("ucret faturasi okunamadi: %v", err)
		}
		credit, err := store.GetInvoice(ctx, pool, r.CreditInvoiceID)
		if err != nil {
			t.Fatalf("kredi faturasi okunamadi: %v", err)
		}
		if charge.Kind != store.InvoiceKindProrationCharge || charge.Amount != 4200 {
			t.Fatalf("ucret faturasi yanlis: %+v", charge)
		}
		if credit.Kind != store.InvoiceKindProrationCredit || credit.Amount != 2100 {
			t.Fatalf("kredi faturasi yanlis: %+v", credit)
		}
		guncel, err := store.GetSubscription(ctx, pool, sub.ID)
		if err != nil {
			t.Fatalf("abonelik okunamadi: %v", err)
		}
		if guncel.PlanCode != pahali || guncel.MonthlyPrice != 6000 {
			t.Fatalf("abonelik plani degismedi: %+v", guncel)
		}
		if _, err := svc.ChangePlan(ctx, sub.ID, pahali, an(t, "2026-04-11 00:00:00")); !errors.Is(err, billingsvc.ErrSamePlan) {
			t.Fatalf("ayni plan hatasi = %v, beklenen ErrSamePlan", err)
		}
	})

	t.Run("dusurme", func(t *testing.T) {
		sub, err := svc.Subscribe(ctx, benzersiz("34CHG"), pahali, an(t, "2026-04-01 00:00:00"))
		if err != nil {
			t.Fatalf("abonelik acilamadi: %v", err)
		}
		r, err := svc.ChangePlan(ctx, sub.ID, ucuz, an(t, "2026-04-10 00:00:00"))
		if err != nil {
			t.Fatalf("plan degistirilemedi: %v", err)
		}
		if r.Charge != 2100 || r.Credit != 4200 || r.Net != -2100 || r.Direction != "downgrade" {
			t.Fatalf("degisim yanlis: %+v", r)
		}
	})

	t.Run("hatalar", func(t *testing.T) {
		sub, err := svc.Subscribe(ctx, benzersiz("34CHG"), ucuz, an(t, "2026-04-01 00:00:00"))
		if err != nil {
			t.Fatalf("abonelik acilamadi: %v", err)
		}
		if _, err := svc.ChangePlan(ctx, sub.ID, benzersiz("yok"), an(t, "2026-04-10 00:00:00")); !errors.Is(err, store.ErrPlanNotFound) {
			t.Fatalf("olmayan plan hatasi = %v, beklenen ErrPlanNotFound", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE plans SET active = false WHERE code = $1`, pahali); err != nil {
			t.Fatalf("plan pasif yapilamadi: %v", err)
		}
		if _, err := svc.ChangePlan(ctx, sub.ID, pahali, an(t, "2026-04-10 00:00:00")); !errors.Is(err, billingsvc.ErrInvalidPlan) {
			t.Fatalf("pasif plan hatasi = %v, beklenen ErrInvalidPlan", err)
		}
		if _, err := svc.Cancel(ctx, sub.ID, an(t, "2026-04-02 00:00:00")); err != nil {
			t.Fatalf("iptal edilemedi: %v", err)
		}
		if _, err := svc.ChangePlan(ctx, sub.ID, ucuz, an(t, "2026-04-10 00:00:00")); !errors.Is(err, store.ErrSubscriptionCancelled) {
			t.Fatalf("iptal abonelik plan degisikligi hatasi = %v, beklenen ErrSubscriptionCancelled", err)
		}
	})
}

func TestRunPeriodEnd(t *testing.T) {
	pool, svc := yeniServis(t)
	ctx := context.Background()
	plan := planOlustur(t, pool, 1000)

	var subIDler []int64
	for i := 0; i < 3; i++ {
		sub, err := svc.Subscribe(ctx, benzersiz("34RUN"), plan, an(t, "2026-05-01 00:00:00"))
		if err != nil {
			t.Fatalf("abonelik acilamadi: %v", err)
		}
		subIDler = append(subIDler, sub.ID)
	}
	iptal, err := svc.Subscribe(ctx, benzersiz("34RUN"), plan, an(t, "2026-05-01 00:00:00"))
	if err != nil {
		t.Fatalf("abonelik acilamadi: %v", err)
	}
	if _, err := svc.Cancel(ctx, iptal.ID, an(t, "2026-05-02 00:00:00")); err != nil {
		t.Fatalf("abonelik iptal edilemedi: %v", err)
	}

	due := dueSubscriptions(t, pool, an(t, "2026-05-01 00:00:00"))
	created, err := svc.RunPeriodEnd(ctx, an(t, "2026-05-01 00:00:00"))
	if err != nil {
		t.Fatalf("donem sonu calismadi: %v", err)
	}
	if created != due {
		t.Fatalf("olusturulan fatura = %d, odenmesi gereken abonelik = %d", created, due)
	}
	if created < 3 {
		t.Fatalf("olusturulan fatura = %d, en az 3 beklenirdi", created)
	}
	for _, id := range subIDler {
		fs := faturalar(t, pool, int64(id))
		if len(fs) != 1 {
			t.Fatalf("%d abonelik icin fatura sayisi = %d, beklenen 1", id, len(fs))
		}
		if fs[0].Kind != store.InvoiceKindPeriod || fs[0].Amount != 1000 || fs[0].Status != store.InvoiceStatusOpen {
			t.Fatalf("donem faturasi yanlis: %+v", fs[0])
		}
		sub, err := store.GetSubscription(ctx, pool, id)
		if err != nil {
			t.Fatalf("abonelik okunamadi: %v", err)
		}
		if !sub.CurrentPeriodStart.Equal(tarihUTC(2026, 6, 1)) || !sub.CurrentPeriodEnd.Equal(tarihUTC(2026, 7, 1)) {
			t.Fatalf("abonelik donemi ilerlemedi: %v - %v", sub.CurrentPeriodStart, sub.CurrentPeriodEnd)
		}
	}
	for _, inv := range faturalar(t, pool, iptal.ID) {
		if inv.Kind == store.InvoiceKindPeriod {
			t.Fatalf("iptal abonelik donem faturasi aldi: %+v", inv)
		}
	}

	created, err = svc.RunPeriodEnd(ctx, an(t, "2026-05-01 00:00:00"))
	if err != nil {
		t.Fatalf("ikinci calisma: %v", err)
	}
	if created != 0 {
		t.Fatalf("ikinci calisma %d fatura uretti, beklenen 0", created)
	}
	var runSayisi, faturaSayisi int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM billing_runs WHERE period_start = '2026-05-01'`).Scan(&runSayisi); err != nil {
		t.Fatalf("billing_runs okunamadi: %v", err)
	}
	if runSayisi != 1 {
		t.Fatalf("billing_runs sayisi = %d, beklenen 1", runSayisi)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM invoices WHERE period_start = '2026-05-01' AND kind = 'period'`).Scan(&faturaSayisi); err != nil {
		t.Fatalf("fatura sayisi okunamadi: %v", err)
	}
	if faturaSayisi < 3 {
		t.Fatalf("fatura sayisi = %d, en az 3 beklenirdi", faturaSayisi)
	}
}

func TestRunPeriodEndConcurrent(t *testing.T) {
	pool, svc := yeniServis(t)
	ctx := context.Background()
	plan := planOlustur(t, pool, 2000)

	const adet = 5
	var subIDler []int64
	for i := 0; i < adet; i++ {
		sub, err := svc.Subscribe(ctx, benzersiz("34CON"), plan, an(t, "2026-09-01 00:00:00"))
		if err != nil {
			t.Fatalf("abonelik acilamadi: %v", err)
		}
		subIDler = append(subIDler, sub.ID)
	}

	donem := an(t, "2026-09-01 00:00:00")
	due := dueSubscriptions(t, pool, donem)
	basla := make(chan struct{})
	sonuclar := make(chan int, adet)
	hatalar := make(chan error, adet)
	var wg sync.WaitGroup
	for i := 0; i < adet; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-basla
			n, err := svc.RunPeriodEnd(ctx, donem)
			if err != nil {
				hatalar <- err
				return
			}
			sonuclar <- n
		}()
	}
	close(basla)
	wg.Wait()
	close(sonuclar)
	close(hatalar)
	for err := range hatalar {
		t.Fatalf("eszamanli calisma hatasi: %v", err)
	}
	toplam := 0
	for n := range sonuclar {
		toplam += n
	}
	if toplam != due {
		t.Fatalf("eszamanli toplam fatura = %d, faturalanacak abonelik = %d", toplam, due)
	}
	if toplam < adet {
		t.Fatalf("eszamanli toplam fatura = %d, en az %d beklenirdi", toplam, adet)
	}
	for _, id := range subIDler {
		if fs := faturalar(t, pool, id); len(fs) != 1 {
			t.Fatalf("%d abonelik icin fatura sayisi = %d, beklenen 1", id, len(fs))
		}
	}
	var runSayisi int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM billing_runs WHERE period_start = '2026-09-01'`).Scan(&runSayisi); err != nil {
		t.Fatalf("billing_runs okunamadi: %v", err)
	}
	if runSayisi != 1 {
		t.Fatalf("billing_runs sayisi = %d, beklenen 1", runSayisi)
	}
}

func TestDunning(t *testing.T) {
	pool, svc := yeniServis(t)
	ctx := context.Background()
	plan := planOlustur(t, pool, 1000)

	sub, err := svc.Subscribe(ctx, benzersiz("34DUN"), plan, an(t, "2026-07-01 00:00:00"))
	if err != nil {
		t.Fatalf("abonelik acilamadi: %v", err)
	}
	if _, err := svc.RunPeriodEnd(ctx, an(t, "2026-07-01 00:00:00")); err != nil {
		t.Fatalf("donem sonu: %v", err)
	}
	fs := faturalar(t, pool, sub.ID)
	if len(fs) != 1 {
		t.Fatalf("fatura sayisi = %d, beklenen 1", len(fs))
	}
	invID := fs[0].ID
	ilk := an(t, "2026-07-01 12:00:00")

	inv, err := svc.RecordPaymentAttempt(ctx, invID, false, ilk)
	if err != nil {
		t.Fatalf("basarisiz deneme kaydedilemedi: %v", err)
	}
	if inv.Status != store.InvoiceStatusFailed || inv.Attempts != 1 || inv.NextRetryAt == nil ||
		!inv.NextRetryAt.Equal(ilk.Add(24*time.Hour)) {
		t.Fatalf("ilk basarisizlik yanlis: %+v", inv)
	}
	subGuncel, err := store.GetSubscription(ctx, pool, sub.ID)
	if err != nil {
		t.Fatalf("abonelik okunamadi: %v", err)
	}
	if subGuncel.Status != billing.StatusPastDue {
		t.Fatalf("abonelik durumu = %q, beklenen past_due", subGuncel.Status)
	}
	if due, err := svc.DueRetries(ctx, ilk.Add(time.Hour)); err != nil || len(due) != 0 {
		t.Fatalf("erken tekrar denemesi = %d, %v; 0, nil beklenirdi", len(due), err)
	}
	if due, err := svc.DueRetries(ctx, ilk.Add(25*time.Hour)); err != nil || len(due) != 1 || due[0].ID != invID {
		t.Fatalf("vadesi gelen tekrar denemesi yanlis: %+v, %v", due, err)
	}

	ikinci := ilk.Add(24 * time.Hour)
	inv, err = svc.RecordPaymentAttempt(ctx, invID, false, ikinci)
	if err != nil {
		t.Fatalf("ikinci basarisiz deneme: %v", err)
	}
	if inv.Attempts != 2 || inv.NextRetryAt == nil || !inv.NextRetryAt.Equal(ilk.Add(72*time.Hour)) {
		t.Fatalf("ikinci basarisizlik yanlis: %+v", inv)
	}

	ucuncu := ilk.Add(72 * time.Hour)
	inv, err = svc.RecordPaymentAttempt(ctx, invID, false, ucuncu)
	if err != nil {
		t.Fatalf("ucuncu basarisiz deneme: %v", err)
	}
	if inv.Attempts != 3 || inv.NextRetryAt == nil || !inv.NextRetryAt.Equal(ilk.Add(7*24*time.Hour)) {
		t.Fatalf("ucuncu basarisizlik yanlis: %+v", inv)
	}
	subGuncel, err = store.GetSubscription(ctx, pool, sub.ID)
	if err != nil {
		t.Fatalf("abonelik okunamadi: %v", err)
	}
	if subGuncel.Status != billing.StatusSuspended {
		t.Fatalf("ucuncu basarisizlikta durum = %q, beklenen suspended", subGuncel.Status)
	}
	if due, err := svc.DueRetries(ctx, ilk.Add(30*24*time.Hour)); err != nil || len(due) != 0 {
		t.Fatalf("askiya alinan abonelik tekrar denemeye geldi: %+v, %v", due, err)
	}

	basarili, err := svc.Subscribe(ctx, benzersiz("34DUN"), plan, an(t, "2026-08-01 00:00:00"))
	if err != nil {
		t.Fatalf("abonelik acilamadi: %v", err)
	}
	if _, err := svc.RunPeriodEnd(ctx, an(t, "2026-08-01 00:00:00")); err != nil {
		t.Fatalf("donem sonu: %v", err)
	}
	fs = faturalar(t, pool, basarili.ID)
	if len(fs) != 1 {
		t.Fatalf("fatura sayisi = %d, beklenen 1", len(fs))
	}
	invID2 := fs[0].ID
	zaman := an(t, "2026-08-01 12:00:00")
	if _, err := svc.RecordPaymentAttempt(ctx, invID2, false, zaman); err != nil {
		t.Fatalf("basarisiz deneme: %v", err)
	}
	inv2, err := svc.RecordPaymentAttempt(ctx, invID2, true, zaman.Add(time.Hour))
	if err != nil {
		t.Fatalf("basarili deneme: %v", err)
	}
	if inv2.Status != store.InvoiceStatusPaid || inv2.Attempts != 0 || inv2.NextRetryAt != nil || inv2.PaidAt == nil {
		t.Fatalf("basarili odeme sonucu yanlis: %+v", inv2)
	}
	subGuncel, err = store.GetSubscription(ctx, pool, basarili.ID)
	if err != nil {
		t.Fatalf("abonelik okunamadi: %v", err)
	}
	if subGuncel.Status != billing.StatusActive {
		t.Fatalf("basarili odeme sonrasi durum = %q, beklenen active", subGuncel.Status)
	}
	if _, err := svc.RecordPaymentAttempt(ctx, invID2, true, zaman.Add(2*time.Hour)); !errors.Is(err, store.ErrInvoiceAlreadyPaid) {
		t.Fatalf("odendi fatura tekrar hatasi = %v, beklenen ErrInvoiceAlreadyPaid", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE invoices SET status = 'void', paid_at = NULL WHERE id = $1`, invID); err != nil {
		t.Fatalf("fatura void yapilamadi: %v", err)
	}
	if _, err := svc.RecordPaymentAttempt(ctx, invID, true, zaman); !errors.Is(err, billingsvc.ErrInvalidAttempts) {
		t.Fatalf("void fatura hatasi = %v, beklenen ErrInvalidAttempts", err)
	}
	if _, err := svc.RecordPaymentAttempt(ctx, 1<<60, true, zaman); !errors.Is(err, store.ErrInvoiceNotFound) {
		t.Fatalf("olmayan fatura hatasi = %v, beklenen ErrInvoiceNotFound", err)
	}
}

func dueSubscriptions(t *testing.T, pool *pgxpool.Pool, periodStart time.Time) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM subscriptions
		  WHERE status IN ('active','past_due') AND current_period_start <= $1`,
		periodStart.Format("2006-01-02"),
	).Scan(&n); err != nil {
		t.Fatalf("faturalanacak abonelik sayisi okunamadi: %v", err)
	}
	return n
}

func TestChangePlanCancelSamePeriod(t *testing.T) {
	pool, svc := yeniServis(t)
	ctx := context.Background()
	ucuz := planOlustur(t, pool, 3000)
	pahali := planOlustur(t, pool, 6000)

	sub, err := svc.Subscribe(ctx, benzersiz("34PCS"), ucuz, an(t, "2026-11-01 00:00:00"))
	if err != nil {
		t.Fatalf("abonelik acilamadi: %v", err)
	}
	degisim, err := svc.ChangePlan(ctx, sub.ID, pahali, an(t, "2026-11-10 00:00:00"))
	if err != nil {
		t.Fatalf("plan degistirilemedi: %v", err)
	}
	if degisim.Credit != 2100 || degisim.Charge != 4200 {
		t.Fatalf("ilk degisim yanlis: %+v", degisim)
	}

	iptal, err := svc.Cancel(ctx, sub.ID, an(t, "2026-11-20 00:00:00"))
	if err != nil {
		t.Fatalf("plan gecisinden sonra iptal basarisiz: %v", err)
	}
	if iptal.Credit != 2200 {
		t.Fatalf("iptal kredisi = %d, beklenen 2200", iptal.Credit)
	}
	if iptal.InvoiceID != degisim.CreditInvoiceID {
		t.Fatalf("iptal kredisi ayri fatura acti: %d != %d", iptal.InvoiceID, degisim.CreditInvoiceID)
	}
	credit, err := store.GetInvoice(ctx, pool, degisim.CreditInvoiceID)
	if err != nil {
		t.Fatalf("kredi faturasi okunamadi: %v", err)
	}
	if credit.Amount != 4300 {
		t.Fatalf("birlesik kredi = %d, beklenen 4300", credit.Amount)
	}
	charge, err := store.GetInvoice(ctx, pool, degisim.ChargeInvoiceID)
	if err != nil {
		t.Fatalf("ucret faturasi okunamadi: %v", err)
	}
	if charge.Amount != 4200 {
		t.Fatalf("ucret faturasi = %d, beklenen 4200", charge.Amount)
	}
	fs := faturalar(t, pool, sub.ID)
	if len(fs) != 2 {
		t.Fatalf("fatura sayisi = %d, beklenen 2 (donem faturasi yok)", len(fs))
	}
	guncel, err := store.GetSubscription(ctx, pool, sub.ID)
	if err != nil {
		t.Fatalf("abonelik okunamadi: %v", err)
	}
	if guncel.Status != billing.StatusCancelled {
		t.Fatalf("durum = %q, beklenen cancelled", guncel.Status)
	}
}

func TestChangePlanTwiceSamePeriod(t *testing.T) {
	pool, svc := yeniServis(t)
	ctx := context.Background()
	ucuz := planOlustur(t, pool, 3000)
	orta := planOlustur(t, pool, 6000)
	yeni := planOlustur(t, pool, 4000)

	sub, err := svc.Subscribe(ctx, benzersiz("34PCS"), ucuz, an(t, "2026-11-01 00:00:00"))
	if err != nil {
		t.Fatalf("abonelik acilamadi: %v", err)
	}
	ilk, err := svc.ChangePlan(ctx, sub.ID, orta, an(t, "2026-11-10 00:00:00"))
	if err != nil {
		t.Fatalf("ilk plan gecisi basarisiz: %v", err)
	}
	ikinci, err := svc.ChangePlan(ctx, sub.ID, yeni, an(t, "2026-11-20 00:00:00"))
	if err != nil {
		t.Fatalf("ikinci plan gecisi basarisiz: %v", err)
	}
	if ikinci.Credit != 2200 || ikinci.Charge != 1466 || ikinci.Direction != "downgrade" {
		t.Fatalf("ikinci gecis yanlis: %+v", ikinci)
	}
	credit, err := store.GetInvoice(ctx, pool, ilk.CreditInvoiceID)
	if err != nil {
		t.Fatalf("kredi faturasi okunamadi: %v", err)
	}
	if credit.Amount != 4300 || credit.ID != ikinci.CreditInvoiceID {
		t.Fatalf("kredi birlestirilemedi: %+v (ikinci id %d)", credit, ikinci.CreditInvoiceID)
	}
	charge, err := store.GetInvoice(ctx, pool, ilk.ChargeInvoiceID)
	if err != nil {
		t.Fatalf("ucret faturasi okunamadi: %v", err)
	}
	if charge.Amount != 5666 || charge.ID != ikinci.ChargeInvoiceID {
		t.Fatalf("ucret birlestirilemedi: %+v (ikinci id %d)", charge, ikinci.ChargeInvoiceID)
	}
	if fs := faturalar(t, pool, sub.ID); len(fs) != 2 {
		t.Fatalf("fatura sayisi = %d, beklenen 2", len(fs))
	}
	guncel, err := store.GetSubscription(ctx, pool, sub.ID)
	if err != nil {
		t.Fatalf("abonelik okunamadi: %v", err)
	}
	if guncel.PlanCode != yeni || guncel.MonthlyPrice != 4000 {
		t.Fatalf("son plan yanlis: %+v", guncel)
	}
}

func TestRunPeriodEndPastDue(t *testing.T) {
	pool, svc := yeniServis(t)
	ctx := context.Background()
	plan := planOlustur(t, pool, 1234)

	sub, err := svc.Subscribe(ctx, benzersiz("34PST"), plan, an(t, "2027-02-01 00:00:00"))
	if err != nil {
		t.Fatalf("abonelik acilamadi: %v", err)
	}
	if err := store.SetSubscriptionStatus(ctx, pool, sub.ID, billing.StatusPastDue, nil); err != nil {
		t.Fatalf("past_due yapilamadi: %v", err)
	}
	if _, err := svc.RunPeriodEnd(ctx, an(t, "2027-02-01 00:00:00")); err != nil {
		t.Fatalf("donem sonu: %v", err)
	}
	fs := faturalar(t, pool, sub.ID)
	if len(fs) != 1 || fs[0].Kind != store.InvoiceKindPeriod || fs[0].Amount != 1234 {
		t.Fatalf("past_due donem faturasi yanlis: %+v", fs)
	}
	guncel, err := store.GetSubscription(ctx, pool, sub.ID)
	if err != nil {
		t.Fatalf("abonelik okunamadi: %v", err)
	}
	if guncel.Status != billing.StatusPastDue {
		t.Fatalf("durum = %q, beklenen past_due", guncel.Status)
	}
	if !guncel.CurrentPeriodStart.Equal(tarihUTC(2027, 3, 1)) || !guncel.CurrentPeriodEnd.Equal(tarihUTC(2027, 4, 1)) {
		t.Fatalf("past_due donemi ilerlemedi: %v - %v", guncel.CurrentPeriodStart, guncel.CurrentPeriodEnd)
	}
}

func TestRunPeriodEndSuspendedReactivated(t *testing.T) {
	pool, svc := yeniServis(t)
	ctx := context.Background()
	plan := planOlustur(t, pool, 777)

	sub, err := svc.Subscribe(ctx, benzersiz("34SUS"), plan, an(t, "2027-03-01 00:00:00"))
	if err != nil {
		t.Fatalf("abonelik acilamadi: %v", err)
	}
	if err := store.SetSubscriptionStatus(ctx, pool, sub.ID, billing.StatusSuspended, nil); err != nil {
		t.Fatalf("suspended yapilamadi: %v", err)
	}
	if _, err := svc.RunPeriodEnd(ctx, an(t, "2027-03-01 00:00:00")); err != nil {
		t.Fatalf("askidayken donem sonu: %v", err)
	}
	if fs := faturalar(t, pool, sub.ID); len(fs) != 0 {
		t.Fatalf("suspended abonelik faturalandi: %+v", fs)
	}
	askida, err := store.GetSubscription(ctx, pool, sub.ID)
	if err != nil {
		t.Fatalf("abonelik okunamadi: %v", err)
	}
	if !askida.CurrentPeriodStart.Equal(tarihUTC(2027, 3, 1)) {
		t.Fatalf("suspended donemi ilerledi: %v", askida.CurrentPeriodStart)
	}

	if err := store.SetSubscriptionStatus(ctx, pool, sub.ID, billing.StatusActive, nil); err != nil {
		t.Fatalf("active yapilamadi: %v", err)
	}
	if _, err := svc.RunPeriodEnd(ctx, an(t, "2027-04-01 00:00:00")); err != nil {
		t.Fatalf("yeniden aktif donem sonu: %v", err)
	}
	fs := faturalar(t, pool, sub.ID)
	if len(fs) != 1 {
		t.Fatalf("yeniden aktif sonrasi fatura sayisi = %d, beklenen 1", len(fs))
	}
	if !fs[0].PeriodStart.Equal(tarihUTC(2027, 4, 1)) || fs[0].Amount != 777 {
		t.Fatalf("yeniden aktif fatura yanlis: %+v", fs[0])
	}
	guncel, err := store.GetSubscription(ctx, pool, sub.ID)
	if err != nil {
		t.Fatalf("abonelik okunamadi: %v", err)
	}
	if !guncel.CurrentPeriodStart.Equal(tarihUTC(2027, 5, 1)) || !guncel.CurrentPeriodEnd.Equal(tarihUTC(2027, 6, 1)) {
		t.Fatalf("yeniden aktif donemi ilerlemedi: %v - %v", guncel.CurrentPeriodStart, guncel.CurrentPeriodEnd)
	}
}

func oturumHazirla(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	ctx := context.Background()
	lotID := testdb.NewLot(t, pool, 100)

	var eventID int64
	at := time.Date(2026, 5, 10, 9, 0, 0, 0, time.UTC)
	if err := pool.QueryRow(ctx,
		`INSERT INTO plate_events (lot_id, camera_id, camera_event_id, direction, plate_raw, plate_norm, captured_at, outcome)
		 VALUES ($1, 'cam', $2, 'entry', '34MUT001', '34MUT001', $3, 'opened') RETURNING id`,
		lotID, benzersiz("olay"), at,
	).Scan(&eventID); err != nil {
		t.Fatalf("olay eklenemedi: %v", err)
	}
	tarifeID := benzersiz("tarife")
	if _, err := pool.Exec(ctx,
		`INSERT INTO tariff_versions (id, lot_id, valid_from, definition)
		 VALUES ($1, $2, $3, '{"location":"Europe/Istanbul"}')`,
		tarifeID, lotID, at.Add(-time.Hour),
	); err != nil {
		t.Fatalf("tarife eklenemedi: %v", err)
	}
	var sessionID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO sessions (lot_id, plate, status, entry_event_id, entered_at, tariff_version_id)
		 VALUES ($1, $2, 'open', $3, $4, $5) RETURNING id`,
		lotID, benzersiz("34MUT"), eventID, at, tarifeID,
	).Scan(&sessionID); err != nil {
		t.Fatalf("oturum eklenemedi: %v", err)
	}
	return sessionID
}

func odemeEkle(t *testing.T, pool *pgxpool.Pool, sessionID int64, tutar int64, paidAt time.Time) string {
	t.Helper()
	ref := benzersiz("prov")
	_, err := pool.Exec(context.Background(),
		`INSERT INTO payments (session_id, amount_kurus, status, provider_ref, idempotency_key,
		                       request_hash, requested_at, paid_at, confirmed_at)
		 VALUES ($1, $2, 'succeeded', $3, $4, $5, $6, $7, $8)`,
		sessionID, tutar, ref, benzersiz("idem"), bytes.Repeat([]byte{0x11}, 32),
		paidAt.Add(-time.Minute), paidAt, paidAt.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("odeme eklenemedi: %v", err)
	}
	return ref
}

func TestReconcileDay(t *testing.T) {
	pool, svc := yeniServis(t)
	ctx := context.Background()
	gun := an(t, "2026-05-10 00:00:00")
	paidAt := an(t, "2026-05-10 12:00:00")

	sessionID := oturumHazirla(t, pool)
	refA := odemeEkle(t, pool, sessionID, 100, paidAt)
	refB := odemeEkle(t, pool, sessionID, 200, paidAt)
	odemeEkle(t, pool, sessionID, 300, paidAt)
	refD := odemeEkle(t, pool, sessionID, 400, paidAt)
	refE := benzersiz("prov")

	plan := planOlustur(t, pool, 500)
	sub, err := svc.Subscribe(ctx, benzersiz("34REC"), plan, an(t, "2026-05-01 00:00:00"))
	if err != nil {
		t.Fatalf("abonelik acilamadi: %v", err)
	}
	paid := an(t, "2026-05-10 11:00:00")
	invID, err := store.InsertInvoice(ctx, pool, store.Invoice{
		SubscriptionID: sub.ID,
		PeriodStart:    an(t, "2026-05-01 00:00:00"),
		PeriodEnd:      an(t, "2026-06-01 00:00:00"),
		Kind:           store.InvoiceKindPeriod,
		Amount:         500,
		Status:         store.InvoiceStatusPaid,
		PaidAt:         &paid,
	})
	if err != nil {
		t.Fatalf("odendi fatura eklenemedi: %v", err)
	}

	_, err = svc.ImportProviderSettlement(ctx, gun, []store.SettlementRow{
		{ProviderRef: refA, Amount: 100},
		{ProviderRef: refB, Amount: 200},
		{ProviderRef: refD, Amount: 350},
		{ProviderRef: refE, Amount: 600},
		{ProviderRef: fmt.Sprintf("invoice:%d", invID), Amount: 500},
	})
	if err != nil {
		t.Fatalf("saglayici raporu yuklenemedi: %v", err)
	}

	rapor, err := svc.ReconcileDay(ctx, gun)
	if err != nil {
		t.Fatalf("mutabakat yapilamadi: %v", err)
	}
	if len(rapor.Matched) != 3 || len(rapor.MissingInProvider) != 1 || len(rapor.MissingInOurs) != 1 || len(rapor.AmountMismatch) != 1 {
		t.Fatalf("mutabakat listeleri yanlis: %+v", rapor)
	}
	if rapor.MissingInProvider[0].Amount != 300 {
		t.Fatalf("saglayicida eksik tutari = %d, beklenen 300", rapor.MissingInProvider[0].Amount)
	}
	if rapor.MissingInOurs[0].ProviderRef != refE || rapor.MissingInOurs[0].Amount != 600 {
		t.Fatalf("bizde eksik yanlis: %+v", rapor.MissingInOurs)
	}
	if m := rapor.AmountMismatch[0]; m.Ours != 400 || m.Provider != 350 {
		t.Fatalf("tutar farki yanlis: %+v", m)
	}
	if rapor.OurTotal != 1500 || rapor.ProviderTotal != 1750 || rapor.Diff != 250 {
		t.Fatalf("toplamlar yanlis: our=%d provider=%d diff=%d", rapor.OurTotal, rapor.ProviderTotal, rapor.Diff)
	}

	rapor2, err := svc.ReconcileDay(ctx, gun)
	if err != nil {
		t.Fatalf("ikinci mutabakat: %v", err)
	}
	if rapor2.OurTotal != rapor.OurTotal || rapor2.ProviderTotal != rapor.ProviderTotal || rapor2.Diff != rapor.Diff {
		t.Fatalf("ikinci mutabakat farkli: %+v", rapor2)
	}
	var adet int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM reconciliation_reports WHERE day = '2026-05-10'`).Scan(&adet); err != nil {
		t.Fatalf("rapor sayisi okunamadi: %v", err)
	}
	if adet != 1 {
		t.Fatalf("rapor satiri = %d, beklenen 1", adet)
	}
}
