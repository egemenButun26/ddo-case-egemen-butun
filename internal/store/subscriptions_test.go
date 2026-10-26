package store_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/pressly/goose/v3"

	"parkakil/internal/domain/billing"
	"parkakil/internal/store"
	"parkakil/internal/testdb"
	"parkakil/migrations"
)

func tarih(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestBillingSchema(t *testing.T) {
	pool := testdb.Open(t)
	ctx := context.Background()

	tablolar := map[string]bool{}
	rows, err := pool.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public'`)
	if err != nil {
		t.Fatalf("tablolar okunamadi: %v", err)
	}
	for rows.Next() {
		var ad string
		if err := rows.Scan(&ad); err != nil {
			t.Fatalf("tablo adi okunamadi: %v", err)
		}
		tablolar[ad] = true
	}
	rows.Close()
	for _, ad := range []string{"plans", "subscriptions", "invoices", "billing_runs", "provider_settlements", "reconciliation_reports"} {
		if !tablolar[ad] {
			t.Fatalf("%s tablosu yok", ad)
		}
	}
	if !dizinVarMi(t, openSQL(t), "subscriptions_one_active_per_plate_lot") {
		t.Fatal("subscriptions_one_active_per_plate_lot dizini yok")
	}

	lotID := testdb.NewLot(t, pool, 100)
	planKodu := benzersiz("plan")
	planID, err := store.InsertPlan(ctx, pool, planKodu, lotID, 5000)
	if err != nil {
		t.Fatalf("plan eklenemedi: %v", err)
	}
	if _, err := store.InsertPlan(ctx, pool, planKodu, lotID, 6000); !errors.Is(err, store.ErrPlanExists) {
		t.Fatalf("ayni plan kodu hatasi = %v, beklenen ErrPlanExists", err)
	}
	plan, err := store.GetPlanByCode(ctx, pool, planKodu)
	if err != nil {
		t.Fatalf("plan okunamadi: %v", err)
	}
	if plan.ID != planID || plan.MonthlyPrice != 5000 || plan.LotID != lotID || !plan.Active {
		t.Fatalf("plan alanlari yanlis: %+v", plan)
	}
	if _, err := store.GetPlanByCode(ctx, pool, benzersiz("yok")); !errors.Is(err, store.ErrPlanNotFound) {
		t.Fatalf("olmayan plan hatasi = %v, beklenen ErrPlanNotFound", err)
	}

	subID, err := store.InsertSubscription(ctx, pool, planID, "34BIL001", tarih(2026, 4, 1), tarih(2026, 5, 1))
	if err != nil {
		t.Fatalf("abonelik eklenemedi: %v", err)
	}
	sub, err := store.GetSubscription(ctx, pool, subID)
	if err != nil {
		t.Fatalf("abonelik okunamadi: %v", err)
	}
	if sub.Status != billing.StatusActive || sub.Plate != "34BIL001" || sub.MonthlyPrice != 5000 ||
		!sub.CurrentPeriodStart.Equal(tarih(2026, 4, 1)) || !sub.CurrentPeriodEnd.Equal(tarih(2026, 5, 1)) {
		t.Fatalf("abonelik alanlari yanlis: %+v", sub)
	}
	if _, err := store.InsertSubscription(ctx, pool, planID, "34BIL001", tarih(2026, 4, 1), tarih(2026, 5, 1)); !errors.Is(err, store.ErrSubscriptionExists) {
		t.Fatalf("cift aktif abonelik hatasi = %v, beklenen ErrSubscriptionExists", err)
	}
	if _, err := store.InsertSubscription(ctx, pool, planID, "34BIL002", tarih(2026, 5, 1), tarih(2026, 5, 1)); pgKodu(err) != "23514" {
		t.Fatalf("ters donem hatasi = %v, beklenen 23514", err)
	}
	if err := store.SetSubscriptionStatus(ctx, pool, subID, billing.StatusCancelled, ptrZaman(tarih(2026, 4, 10))); err != nil {
		t.Fatalf("abonelik iptal edilemedi: %v", err)
	}
	if _, err := store.InsertSubscription(ctx, pool, planID, "34BIL001", tarih(2026, 5, 1), tarih(2026, 6, 1)); err != nil {
		t.Fatalf("iptal sonrasi yeni abonelik eklenemedi: %v", err)
	}

	inv := store.Invoice{
		SubscriptionID: subID,
		PeriodStart:    tarih(2026, 4, 1),
		PeriodEnd:      tarih(2026, 5, 1),
		Kind:           store.InvoiceKindPeriod,
		Amount:         5000,
		Status:         store.InvoiceStatusOpen,
	}
	if _, err := store.InsertInvoice(ctx, pool, inv); err != nil {
		t.Fatalf("fatura eklenemedi: %v", err)
	}
	if inserted, err := store.InsertInvoiceIfAbsent(ctx, pool, inv); err != nil || inserted {
		t.Fatalf("ayni fatura tekrar eklendi: inserted=%v hata=%v", inserted, err)
	}
	inv.Kind = store.InvoiceKindProrationCharge
	inv.Amount = 1000
	if inserted, err := store.InsertInvoiceIfAbsent(ctx, pool, inv); err != nil || !inserted {
		t.Fatalf("farkli tur fatura eklenemedi: inserted=%v hata=%v", inserted, err)
	}

	bozuk := inv
	bozuk.PeriodStart = tarih(2026, 6, 1)
	bozuk.PeriodEnd = tarih(2026, 7, 1)
	bozuk.Kind = "period"
	bozuk.Amount = -1
	if _, err := store.InsertInvoice(ctx, pool, bozuk); pgKodu(err) != "23514" {
		t.Fatalf("negatif tutar hatasi = %v, beklenen 23514", err)
	}
	bozuk.Amount = 100
	bozuk.Kind = "baska"
	if _, err := store.InsertInvoice(ctx, pool, bozuk); pgKodu(err) != "23514" {
		t.Fatalf("bilinmeyen tur hatasi = %v, beklenen 23514", err)
	}
	bozuk.Kind = store.InvoiceKindPeriod
	bozuk.Status = "baska"
	if _, err := store.InsertInvoice(ctx, pool, bozuk); pgKodu(err) != "23514" {
		t.Fatalf("bilinmeyen durum hatasi = %v, beklenen 23514", err)
	}
	bozuk.Status = store.InvoiceStatusOpen
	bozuk.Attempts = -1
	if _, err := store.InsertInvoice(ctx, pool, bozuk); pgKodu(err) != "23514" {
		t.Fatalf("negatif deneme hatasi = %v, beklenen 23514", err)
	}
	bozuk.Attempts = 0
	bozuk.Status = store.InvoiceStatusPaid
	if _, err := store.InsertInvoice(ctx, pool, bozuk); pgKodu(err) != "23514" {
		t.Fatalf("paid_at'siz paid hatasi = %v, beklenen 23514", err)
	}

	runID, ok, err := store.StartBillingRun(ctx, pool, tarih(2026, 4, 1))
	if err != nil || !ok || runID == 0 {
		t.Fatalf("faturalama calismasi baslatilamadi: id=%d ok=%v hata=%v", runID, ok, err)
	}
	if _, ok, err := store.StartBillingRun(ctx, pool, tarih(2026, 4, 1)); err != nil || ok {
		t.Fatalf("ayni donem ikinci kez baslatildi: ok=%v hata=%v", ok, err)
	}
	if err := store.FinishBillingRun(ctx, pool, runID, 3); err != nil {
		t.Fatalf("calisma bitirilemedi: %v", err)
	}

	gun := tarih(2026, 5, 20)
	satirlar := []store.SettlementRow{
		{ProviderRef: "prov-A", Amount: 1000},
		{ProviderRef: "prov-B", Amount: 2000},
	}
	if _, err := store.UpsertProviderSettlements(ctx, pool, gun, satirlar); err != nil {
		t.Fatalf("mutabakat satirlari eklenemedi: %v", err)
	}
	satirlar[1].Amount = 2500
	if _, err := store.UpsertProviderSettlements(ctx, pool, gun, satirlar); err != nil {
		t.Fatalf("mutabakat satirlari guncellenemedi: %v", err)
	}
	okunan, err := store.ProviderSettlementsOnDay(ctx, pool, gun)
	if err != nil {
		t.Fatalf("mutabakat satirlari okunamadi: %v", err)
	}
	if len(okunan) != 2 || okunan[1].Amount != 2500 {
		t.Fatalf("mutabakat satirlari yanlis: %+v", okunan)
	}
	if _, err := store.UpsertProviderSettlements(ctx, pool, gun, []store.SettlementRow{{ProviderRef: ""}}); !errors.Is(err, store.ErrInvalidSettlement) {
		t.Fatalf("bos provider_ref hatasi = %v, beklenen ErrInvalidSettlement", err)
	}

	raporGunu := tarih(2026, 5, 21)
	if err := store.SaveReconciliationReport(ctx, pool, raporGunu, 100, 120, 20, []byte(`{"a":1}`)); err != nil {
		t.Fatalf("rapor yazilamadi: %v", err)
	}
	if err := store.SaveReconciliationReport(ctx, pool, raporGunu, 200, 150, -50, []byte(`{"a":2}`)); err != nil {
		t.Fatalf("rapor guncellenemedi: %v", err)
	}
	var adet int
	var fark int64
	if err := pool.QueryRow(ctx, `SELECT count(*), max(diff) FROM reconciliation_reports WHERE day = $1`, raporGunu.Format("2006-01-02")).Scan(&adet, &fark); err != nil {
		t.Fatalf("rapor okunamadi: %v", err)
	}
	if adet != 1 || fark != -50 {
		t.Fatalf("rapor tek satir degil: adet=%d fark=%d", adet, fark)
	}
}

func ptrZaman(t time.Time) *time.Time {
	return &t
}

func openSQL(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("baglanti acilamadi: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrationAbonelikTekrar(t *testing.T) {
	testdb.Open(t)
	db := openSQL(t)
	latest := latestMigrationVersion(t)

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("goose ayari: %v", err)
	}
	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("goose.Up basarisiz: %v", err)
	}
	version, err := goose.GetDBVersion(db)
	if err != nil {
		t.Fatalf("surum okunamadi: %v", err)
	}
	if version != latest {
		t.Fatalf("surum = %d, beklenen %d", version, latest)
	}

	if err := goose.DownTo(db, ".", 6); err != nil {
		t.Fatalf("goose.DownTo(6) basarisiz: %v", err)
	}
	for _, ad := range []string{"plans", "subscriptions", "invoices", "billing_runs", "provider_settlements", "reconciliation_reports"} {
		if tabloVarMi(t, db, ad) {
			t.Fatalf("down sonrasi %s tablosu hala var", ad)
		}
	}

	if err := goose.Up(db, "."); err != nil {
		t.Fatalf("yeniden goose.Up basarisiz: %v", err)
	}
	for _, ad := range []string{"plans", "subscriptions", "invoices", "billing_runs", "provider_settlements", "reconciliation_reports"} {
		if !tabloVarMi(t, db, ad) {
			t.Fatalf("up sonrasi %s tablosu yok", ad)
		}
	}
	if !dizinVarMi(t, db, "subscriptions_one_active_per_plate_lot") {
		t.Fatal("up sonrasi subscriptions_one_active_per_plate_lot yok")
	}
	version, err = goose.GetDBVersion(db)
	if err != nil {
		t.Fatalf("surum okunamadi: %v", err)
	}
	if version != latest {
		t.Fatalf("yeniden up sonrasi surum = %d, beklenen %d", version, latest)
	}
}
