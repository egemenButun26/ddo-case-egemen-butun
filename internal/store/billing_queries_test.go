package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"parkakil/internal/store"
	"parkakil/internal/testdb"
)

func TestAddProrationInvoiceSamePeriod(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID := testdb.NewLot(t, pool, 100)
	planID, err := store.InsertPlan(ctx, pool, benzersiz("plan"), lotID, 5000)
	if err != nil {
		t.Fatalf("plan eklenemedi: %v", err)
	}
	subID, err := store.InsertSubscription(ctx, pool, planID, benzersiz("34PRR"), tarih(2026, 4, 1), tarih(2026, 5, 1))
	if err != nil {
		t.Fatalf("abonelik eklenemedi: %v", err)
	}
	inv := store.Invoice{
		SubscriptionID: subID,
		PeriodStart:    tarih(2026, 4, 1),
		PeriodEnd:      tarih(2026, 5, 1),
		Kind:           store.InvoiceKindProrationCharge,
		Amount:         1000,
		Status:         store.InvoiceStatusOpen,
	}
	firstID, err := store.AddProrationInvoice(ctx, pool, inv)
	if err != nil {
		t.Fatalf("ilk proration: %v", err)
	}
	inv.Amount = 500
	secondID, err := store.AddProrationInvoice(ctx, pool, inv)
	if err != nil {
		t.Fatalf("ikinci proration: %v", err)
	}
	if firstID != secondID {
		t.Fatalf("ayni donem/kind icin fatura id degisti: %d -> %d", firstID, secondID)
	}
	saved, err := store.GetInvoice(ctx, pool, firstID)
	if err != nil {
		t.Fatalf("fatura okunamadi: %v", err)
	}
	if saved.Amount != 1500 {
		t.Fatalf("birlesik tutar = %d, beklenen 1500", saved.Amount)
	}
	var adet int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM invoices WHERE subscription_id = $1 AND period_start = $2 AND kind = $3`,
		subID, tarih(2026, 4, 1), store.InvoiceKindProrationCharge,
	).Scan(&adet); err != nil {
		t.Fatalf("fatura sayisi: %v", err)
	}
	if adet != 1 {
		t.Fatalf("ayni donem/kind fatura sayisi = %d, beklenen 1", adet)
	}

	inv.Kind = store.InvoiceKindProrationCredit
	inv.Amount = 700
	creditID, err := store.AddProrationInvoice(ctx, pool, inv)
	if err != nil {
		t.Fatalf("kredi proration: %v", err)
	}
	if creditID == firstID {
		t.Fatal("farkli kind ayni faturayi kullandi")
	}
}

func TestListSubscriptionsFilter(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID := testdb.NewLot(t, pool, 100)
	planID, err := store.InsertPlan(ctx, pool, benzersiz("plan"), lotID, 1000)
	if err != nil {
		t.Fatalf("plan eklenemedi: %v", err)
	}
	plate := benzersiz("34LST")
	var idler []int64
	for i := 0; i < 3; i++ {
		id, err := store.InsertSubscription(ctx, pool, planID, plate, tarih(2026, 4, 1), tarih(2026, 5, 1))
		if err != nil && !errors.Is(err, store.ErrSubscriptionExists) {
			t.Fatalf("abonelik eklenemedi: %v", err)
		}
		if err == nil {
			idler = append(idler, id)
			break
		}
	}
	if len(idler) == 0 {
		t.Fatal("abonelik acilamadi")
	}

	otherPlate := benzersiz("34LST")
	otherID, err := store.InsertSubscription(ctx, pool, planID, otherPlate, tarih(2026, 4, 1), tarih(2026, 5, 1))
	if err != nil {
		t.Fatalf("ikinci abonelik: %v", err)
	}

	list, err := store.ListSubscriptions(ctx, pool, store.SubscriptionFilter{Plate: &plate, Limit: 10})
	if err != nil {
		t.Fatalf("plaka filtresi: %v", err)
	}
	if len(list) != 1 || list[0].ID != idler[0] {
		t.Fatalf("plaka filtresi yanlis: %+v", list)
	}

	all, err := store.ListSubscriptions(ctx, pool, store.SubscriptionFilter{AfterID: idler[0], Limit: 10})
	if err != nil {
		t.Fatalf("keyset: %v", err)
	}
	if len(all) == 0 || all[0].ID <= idler[0] {
		t.Fatalf("keyset ihlali: %+v", all)
	}
	bulundu := false
	for _, sub := range all {
		if sub.ID == otherID {
			bulundu = true
		}
	}
	if !bulundu {
		t.Fatalf("after_id sonrasi diger abonelik yok: %+v", all)
	}

	page, err := store.ListSubscriptions(ctx, pool, store.SubscriptionFilter{Limit: 1})
	if err != nil {
		t.Fatalf("limit: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("limit=1 icin %d satir", len(page))
	}
}

func TestListInvoicesFilter(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	lotID := testdb.NewLot(t, pool, 100)
	planID, err := store.InsertPlan(ctx, pool, benzersiz("plan"), lotID, 1000)
	if err != nil {
		t.Fatalf("plan eklenemedi: %v", err)
	}
	plate := benzersiz("34INV")
	subID, err := store.InsertSubscription(ctx, pool, planID, plate, tarih(2026, 4, 1), tarih(2026, 5, 1))
	if err != nil {
		t.Fatalf("abonelik eklenemedi: %v", err)
	}
	base := store.Invoice{
		SubscriptionID: subID,
		PeriodStart:    tarih(2026, 4, 1),
		PeriodEnd:      tarih(2026, 5, 1),
		Kind:           store.InvoiceKindPeriod,
		Amount:         1000,
		Status:         store.InvoiceStatusOpen,
	}
	first, err := store.InsertInvoice(ctx, pool, base)
	if err != nil {
		t.Fatalf("ilk fatura: %v", err)
	}
	second := base
	second.Kind = store.InvoiceKindProrationCharge
	second.Status = store.InvoiceStatusPaid
	paidAt := tarih(2026, 4, 10)
	second.PaidAt = &paidAt
	secondID, err := store.InsertInvoice(ctx, pool, second)
	if err != nil {
		t.Fatalf("ikinci fatura: %v", err)
	}

	bySub, err := store.ListInvoices(ctx, pool, store.InvoiceFilter{SubscriptionID: &subID, Limit: 10})
	if err != nil {
		t.Fatalf("abonelik filtresi: %v", err)
	}
	if len(bySub) != 2 {
		t.Fatalf("abonelik faturasi = %d, beklenen 2", len(bySub))
	}

	paid := store.InvoiceStatusPaid
	byStatus, err := store.ListInvoices(ctx, pool, store.InvoiceFilter{SubscriptionID: &subID, Status: paid, Limit: 10})
	if err != nil {
		t.Fatalf("durum filtresi: %v", err)
	}
	if len(byStatus) != 1 || byStatus[0].ID != secondID {
		t.Fatalf("durum filtresi yanlis: %+v", byStatus)
	}

	byPlate, err := store.ListInvoices(ctx, pool, store.InvoiceFilter{Plate: &plate, Limit: 10})
	if err != nil {
		t.Fatalf("plaka filtresi: %v", err)
	}
	if len(byPlate) != 2 {
		t.Fatalf("plaka faturasi = %d, beklenen 2", len(byPlate))
	}

	after, err := store.ListInvoices(ctx, pool, store.InvoiceFilter{SubscriptionID: &subID, AfterID: first, Limit: 10})
	if err != nil {
		t.Fatalf("keyset: %v", err)
	}
	if len(after) != 1 || after[0].ID != secondID {
		t.Fatalf("keyset yanlis: %+v", after)
	}

	page, err := store.ListInvoices(ctx, pool, store.InvoiceFilter{Limit: 1})
	if err != nil {
		t.Fatalf("limit: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("limit=1 icin %d satir", len(page))
	}
}

func TestGetReconciliationReport(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()
	gun := tarih(2040, 1, 2)
	if _, err := store.GetReconciliationReport(ctx, pool, gun); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("bos rapor hatasi = %v, beklenen ErrNotFound", err)
	}
	details := []byte(`{"matched":[],"our_total_kurus":10,"provider_total_kurus":20,"diff_kurus":10}`)
	if err := store.SaveReconciliationReport(ctx, pool, gun, 10, 20, 10, details); err != nil {
		t.Fatalf("rapor kaydedilemedi: %v", err)
	}
	rec, err := store.GetReconciliationReport(ctx, pool, gun)
	if err != nil {
		t.Fatalf("rapor okunamadi: %v", err)
	}
	if rec.OurTotal != 10 || rec.ProviderTotal != 20 || rec.Diff != 10 {
		t.Fatalf("rapor toplamlari yanlis: %+v", rec)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Details, &got); err != nil {
		t.Fatalf("rapor detayi cozulemedi: %v (%s)", err, rec.Details)
	}
	if got["our_total_kurus"] != float64(10) || got["provider_total_kurus"] != float64(20) || got["diff_kurus"] != float64(10) {
		t.Fatalf("rapor detayi yanlis: %+v", got)
	}
}
