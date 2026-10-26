package billing

import "testing"

func TestReconcile(t *testing.T) {
	ours := []Txn{
		{ProviderRef: "A", Amount: 100},
		{ProviderRef: "B", Amount: 200},
		{ProviderRef: "C", Amount: 300},
		{ProviderRef: "D", Amount: 400},
	}
	provider := []Txn{
		{ProviderRef: "A", Amount: 100},
		{ProviderRef: "B", Amount: 200},
		{ProviderRef: "D", Amount: 450},
		{ProviderRef: "E", Amount: 500},
	}

	r := Reconcile(ours, provider)
	if len(r.Matched) != 2 || r.Matched[0].ProviderRef != "A" || r.Matched[1].ProviderRef != "B" {
		t.Fatalf("eslesenler yanlis: %+v", r.Matched)
	}
	if len(r.MissingInProvider) != 1 || r.MissingInProvider[0].ProviderRef != "C" {
		t.Fatalf("saglayicida eksikler yanlis: %+v", r.MissingInProvider)
	}
	if len(r.MissingInOurs) != 1 || r.MissingInOurs[0].ProviderRef != "E" {
		t.Fatalf("bizde eksikler yanlis: %+v", r.MissingInOurs)
	}
	if len(r.AmountMismatch) != 1 {
		t.Fatalf("tutar farki sayisi = %d, beklenen 1", len(r.AmountMismatch))
	}
	if m := r.AmountMismatch[0]; m.ProviderRef != "D" || m.Ours != 400 || m.Provider != 450 {
		t.Fatalf("tutar farki yanlis: %+v", m)
	}
	if r.OurTotal != 1000 {
		t.Fatalf("OurTotal = %d, beklenen 1000", r.OurTotal)
	}
	if r.ProviderTotal != 1250 {
		t.Fatalf("ProviderTotal = %d, beklenen 1250", r.ProviderTotal)
	}
	if r.Diff != 250 {
		t.Fatalf("Diff = %d, beklenen 250", r.Diff)
	}
}

func TestReconcileTemiz(t *testing.T) {
	r := Reconcile(nil, nil)
	if r.OurTotal != 0 || r.ProviderTotal != 0 || r.Diff != 0 {
		t.Fatalf("bos girdi toplamlari yanlis: %+v", r)
	}
	if len(r.Matched)+len(r.MissingInProvider)+len(r.MissingInOurs)+len(r.AmountMismatch) != 0 {
		t.Fatalf("bos girdi listeleri bos degil: %+v", r)
	}

	ayni := []Txn{{ProviderRef: "A", Amount: 50}}
	r = Reconcile(ayni, ayni)
	if len(r.Matched) != 1 || r.Diff != 0 {
		t.Fatalf("ayni girdi yanlis: %+v", r)
	}
}
