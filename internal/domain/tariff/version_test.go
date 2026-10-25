package tariff

import (
	"errors"
	"testing"
	"time"
)

func basitKural() DayRule {
	return DayRule{
		Name: "hafta_ici",
		Bands: []Band{
			{UpToMinutes: 60, UnitMinutes: 15, UnitPrice: 1000},
			{UnitMinutes: 60, UnitPrice: 3000},
		},
	}
}

func temelSurum() Version {
	return Version{
		ID:        "v1",
		ValidFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Location:  time.UTC,
		Weekday:   basitKural(),
	}
}

func TestValidateGecerli(t *testing.T) {
	v := temelSurum()
	if err := v.Validate(); err != nil {
		t.Fatalf("gecerli surum reddedildi: %v", err)
	}
	v.Weekend = &DayRule{Name: "hafta_sonu", Bands: []Band{{UnitMinutes: 60, UnitPrice: 500}}}
	v.Events = []Event{{Date: Date{2026, 3, 1}, Name: "etkinlik", Rule: basitKural()}}
	if err := v.Validate(); err != nil {
		t.Fatalf("gecerli surum reddedildi: %v", err)
	}
}

func TestValidateHatalari(t *testing.T) {
	temel := func() Version { return temelSurum() }

	testler := []struct {
		ad  string
		boz func(*Version)
	}{
		{"ID bos", func(v *Version) { v.ID = "" }},
		{"Location nil", func(v *Version) { v.Location = nil }},
		{"FreeMinutes negatif", func(v *Version) { v.FreeMinutes = -1 }},
		{"ValidTo esit", func(v *Version) { v.ValidTo = v.ValidFrom }},
		{"ValidTo once", func(v *Version) { v.ValidTo = v.ValidFrom.Add(-time.Hour) }},
		{"Bands bos", func(v *Version) { v.Weekday.Bands = nil }},
		{"UnitMinutes sifir", func(v *Version) { v.Weekday.Bands[0].UnitMinutes = 0 }},
		{"UnitPrice negatif", func(v *Version) { v.Weekday.Bands[0].UnitPrice = -1 }},
		{"dilim siniri negatif", func(v *Version) {
			v.Weekday.Bands = []Band{{UpToMinutes: -5, UnitMinutes: 15, UnitPrice: 1000}, {UnitMinutes: 60, UnitPrice: 3000}}
		}},
		{"dilimler artan degil", func(v *Version) {
			v.Weekday.Bands = []Band{
				{UpToMinutes: 120, UnitMinutes: 15, UnitPrice: 1000},
				{UpToMinutes: 60, UnitMinutes: 15, UnitPrice: 1000},
				{UnitMinutes: 60, UnitPrice: 3000},
			}
		}},
		{"arada sifir dilim", func(v *Version) {
			v.Weekday.Bands = []Band{
				{UpToMinutes: 60, UnitMinutes: 15, UnitPrice: 1000},
				{UnitMinutes: 15, UnitPrice: 1000},
				{UnitMinutes: 60, UnitPrice: 3000},
			}
		}},
		{"son dilim sinirsiz degil", func(v *Version) {
			v.Weekday.Bands = []Band{{UpToMinutes: 60, UnitMinutes: 15, UnitPrice: 1000}}
		}},
		{"gece baslangici gecersiz", func(v *Version) {
			v.Weekday.Night = &Night{StartMinute: 1440, EndMinute: 420, UnitMinutes: 60, UnitPrice: 1000}
		}},
		{"gece bitisi gecersiz", func(v *Version) {
			v.Weekday.Night = &Night{StartMinute: 1320, EndMinute: -1, UnitMinutes: 60, UnitPrice: 1000}
		}},
		{"gece penceresi bos", func(v *Version) {
			v.Weekday.Night = &Night{StartMinute: 600, EndMinute: 600, UnitMinutes: 60, UnitPrice: 1000}
		}},
		{"gece birim dakikasi gecersiz", func(v *Version) {
			v.Weekday.Night = &Night{StartMinute: 1320, EndMinute: 420, UnitMinutes: 0, UnitPrice: 1000}
		}},
		{"gece fiyati negatif", func(v *Version) {
			v.Weekday.Night = &Night{StartMinute: 1320, EndMinute: 420, UnitMinutes: 60, UnitPrice: -1}
		}},
		{"gunluk tavan negatif", func(v *Version) { v.Weekday.DailyCap = -1 }},
		{"hafta sonu kurali bozuk", func(v *Version) {
			v.Weekend = &DayRule{Name: "hafta_sonu"}
		}},
		{"etkinlik kurali bozuk", func(v *Version) {
			v.Events = []Event{{Date: Date{2026, 3, 1}, Name: "x"}}
		}},
		{"ayni tarihe iki etkinlik", func(v *Version) {
			d := Date{2026, 3, 1}
			v.Events = []Event{
				{Date: d, Name: "a", Rule: basitKural()},
				{Date: d, Name: "b", Rule: basitKural()},
			}
		}},
	}

	for _, tt := range testler {
		v := temel()
		tt.boz(&v)
		if err := v.Validate(); err == nil {
			t.Errorf("%s: hata beklenirdi, gelmedi", tt.ad)
		}
	}
}

func katalogSurum(id string, from, to time.Time) Version {
	v := temelSurum()
	v.ID = id
	v.ValidFrom = from
	v.ValidTo = to
	return v
}

func TestValidateCatalog(t *testing.T) {
	ocak := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	haziran := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	aralik := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	if err := ValidateCatalog([]Version{
		katalogSurum("v1", ocak, haziran),
		katalogSurum("v2", haziran, aralik),
		katalogSurum("v3", aralik, time.Time{}),
	}); err != nil {
		t.Fatalf("bitisik katalog reddedildi: %v", err)
	}

	if err := ValidateCatalog([]Version{
		katalogSurum("v1", ocak, haziran),
		katalogSurum("v2", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), aralik),
	}); err != nil {
		t.Fatalf("bosluklu katalog reddedildi: %v", err)
	}

	if err := ValidateCatalog([]Version{
		katalogSurum("v1", ocak, haziran),
		katalogSurum("v2", time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), aralik),
	}); err == nil {
		t.Fatal("cakisan katalog icin hata beklenirdi")
	}

	if err := ValidateCatalog([]Version{
		katalogSurum("v1", ocak, haziran),
		katalogSurum("v1", haziran, aralik),
	}); err == nil {
		t.Fatal("ID tekrari icin hata beklenirdi")
	}

	bozuk := katalogSurum("v1", ocak, haziran)
	bozuk.Location = nil
	if err := ValidateCatalog([]Version{bozuk}); err == nil {
		t.Fatal("bozuk surum icin hata beklenirdi")
	}
}

func TestSelectVersion(t *testing.T) {
	ocak := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	haziran := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	temmuz := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	katalog := []Version{
		katalogSurum("v1", ocak, haziran),
		katalogSurum("v2", haziran, time.Time{}),
	}

	if _, err := SelectVersion(katalog, ocak.Add(-time.Second)); !errors.Is(err, ErrNoVersion) {
		t.Fatalf("once ErrNoVersion beklenirdi, gelen %v", err)
	}
	if v, err := SelectVersion(katalog, ocak); err != nil || v.ID != "v1" {
		t.Fatalf("tam ValidFrom v1 olmali: %q, %v", v.ID, err)
	}
	if v, err := SelectVersion(katalog, haziran.Add(-time.Second)); err != nil || v.ID != "v1" {
		t.Fatalf("sinirdan once v1 olmali: %q, %v", v.ID, err)
	}
	if v, err := SelectVersion(katalog, haziran); err != nil || v.ID != "v2" {
		t.Fatalf("tam ValidTo v2 olmali: %q, %v", v.ID, err)
	}

	bosluklu := []Version{
		katalogSurum("v1", ocak, haziran),
		katalogSurum("v2", temmuz, time.Time{}),
	}
	if _, err := SelectVersion(bosluklu, haziran.Add(24*time.Hour)); !errors.Is(err, ErrNoVersion) {
		t.Fatalf("boslukta ErrNoVersion beklenirdi, gelen %v", err)
	}

	cakisan := []Version{
		katalogSurum("v1", ocak, temmuz),
		katalogSurum("v2", haziran, time.Time{}),
	}
	if _, err := SelectVersion(cakisan, haziran.Add(24*time.Hour)); !errors.Is(err, ErrAmbiguousVersion) {
		t.Fatalf("cakismada ErrAmbiguousVersion beklenirdi, gelen %v", err)
	}
}

func TestDondurma(t *testing.T) {
	istanbul, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		t.Fatalf("Istanbul yuklenemedi: %v", err)
	}

	v1 := temelSurum()
	v1.ID = "v1"
	v1.Location = istanbul
	v1.ValidFrom = time.Date(2026, 1, 1, 0, 0, 0, 0, istanbul)
	v1.ValidTo = time.Date(2026, 11, 1, 0, 0, 0, 0, istanbul)
	v1.Weekday.Bands = []Band{{UnitMinutes: 60, UnitPrice: 1000}}

	v2 := temelSurum()
	v2.ID = "v2"
	v2.Location = istanbul
	v2.ValidFrom = v1.ValidTo
	v2.Weekday.Bands = []Band{{UnitMinutes: 60, UnitPrice: 2000}}

	catalog := []Version{v1, v2}
	if err := ValidateCatalog(catalog); err != nil {
		t.Fatalf("katalog gecersiz: %v", err)
	}

	giris := time.Date(2026, 10, 31, 23, 0, 0, 0, istanbul)
	cikis := time.Date(2026, 11, 1, 2, 0, 0, 0, istanbul)

	secili, err := SelectVersion(catalog, giris)
	if err != nil {
		t.Fatalf("surum secilemedi: %v", err)
	}
	if secili.ID != "v1" {
		t.Fatalf("giriste v1 secilmeliydi, gelen %q", secili.ID)
	}

	q1, err := Calculate(secili, giris, cikis)
	if err != nil {
		t.Fatalf("v1 hesabi: %v", err)
	}
	q2, err := Calculate(v2, giris, cikis)
	if err != nil {
		t.Fatalf("v2 hesabi: %v", err)
	}
	if q1.Total != 3000 {
		t.Fatalf("v1 toplami 3000 olmali, gelen %v", q1.Total)
	}
	if q2.Total != 6000 {
		t.Fatalf("v2 toplami 6000 olmali, gelen %v", q2.Total)
	}
	if q1.Total == q2.Total {
		t.Fatal("dondurma testi ayirt edici degil")
	}
}
