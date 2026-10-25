package tariff

import (
	"errors"
	"testing"
	"time"
)

func yukle(t *testing.T, ad string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(ad)
	if err != nil {
		t.Fatalf("%s yuklenemedi: %v", ad, err)
	}
	return loc
}

func an(t *testing.T, loc *time.Location, s string) time.Time {
	t.Helper()
	x, err := time.ParseInLocation("2006-01-02 15:04:05", s, loc)
	if err != nil {
		t.Fatalf("%q cozulemedi: %v", s, err)
	}
	return x
}

// standartSurum: brifteki kabul surumu.
func standartSurum(t *testing.T) Version {
	t.Helper()
	loc := yukle(t, "Europe/Istanbul")
	return Version{
		ID:          "std",
		ValidFrom:   time.Date(2026, 1, 1, 0, 0, 0, 0, loc),
		Location:    loc,
		FreeMinutes: 15,
		Weekday: DayRule{
			Name:     "hafta_ici",
			Bands:    []Band{{60, 15, 1000}, {0, 60, 3000}},
			Night:    &Night{1320, 420, 60, 1500},
			DailyCap: 25000,
		},
		Weekend: &DayRule{
			Name:     "hafta_sonu",
			Bands:    []Band{{60, 15, 1500}, {0, 60, 4000}},
			Night:    &Night{1320, 420, 60, 2000},
			DailyCap: 30000,
		},
		Events: []Event{{
			Date: Date{2026, 11, 1},
			Name: "derbi",
			Rule: DayRule{Bands: []Band{{0, 60, 10000}}},
		}},
	}
}

func TestBeklenenDegerler(t *testing.T) {
	v := standartSurum(t)
	loc := v.Location

	testler := []struct {
		ad       string
		giris    string
		cikis    string
		toplam   Kurus
		ucretsiz bool
		gun      int
	}{
		{"a on bes dakika tam ucretsiz", "2026-10-27 10:00:00", "2026-10-27 10:15:00", 0, true, 0},
		{"b on bes dakika bir saniye asinca", "2026-10-27 10:00:00", "2026-10-27 10:15:01", 2000, false, 1},
		{"c yetmis dakika iki dilim", "2026-10-27 10:00:00", "2026-10-27 11:10:00", 7000, false, 1},
		{"d on uc saat tavan", "2026-10-27 08:00:00", "2026-10-27 21:00:00", 25000, false, 1},
		{"e gece yarisini asan dort saat", "2026-10-27 21:00:00", "2026-10-28 01:00:00", 8500, false, 2},
		{"f gece yarisi on dakika ucretsiz", "2026-10-27 23:55:00", "2026-10-28 00:05:00", 0, true, 0},
		{"g gece yarisinda iki baslayan birim", "2026-10-27 23:50:00", "2026-10-28 00:10:00", 3000, false, 2},
		{"h cumartesi hafta sonu", "2026-10-31 10:00:00", "2026-10-31 11:00:00", 6000, false, 1},
		{"i etkinlik hafta sonundan once", "2026-11-01 10:00:00", "2026-11-01 12:30:00", 30000, false, 1},
		{"j cuma gecesi cumartesi sabahi", "2026-10-30 23:00:00", "2026-10-31 08:00:00", 21500, false, 2},
		{"k cok gunlu oturum", "2026-10-27 08:00:00", "2026-10-29 08:00:00", 64500, false, 3},
	}

	for _, tt := range testler {
		giris := an(t, loc, tt.giris)
		cikis := an(t, loc, tt.cikis)
		q, err := Calculate(v, giris, cikis)
		if err != nil {
			t.Errorf("%s: hata: %v", tt.ad, err)
			continue
		}
		if q.Total != tt.toplam {
			t.Errorf("%s: toplam %v, bekle %v", tt.ad, q.Total, tt.toplam)
		}
		if q.Free != tt.ucretsiz {
			t.Errorf("%s: Free %v, bekle %v", tt.ad, q.Free, tt.ucretsiz)
		}
		if len(q.Days) != tt.gun {
			t.Errorf("%s: gun sayisi %d, bekle %d", tt.ad, len(q.Days), tt.gun)
		}
		if q.DurationSeconds != int64(cikis.Sub(giris)/time.Second) {
			t.Errorf("%s: sure %d", tt.ad, q.DurationSeconds)
		}
	}
}

func TestGunDokumleri(t *testing.T) {
	v := standartSurum(t)
	loc := v.Location

	t.Run("d tavan", func(t *testing.T) {
		q, err := Calculate(v, an(t, loc, "2026-10-27 08:00:00"), an(t, loc, "2026-10-27 21:00:00"))
		if err != nil {
			t.Fatal(err)
		}
		d := q.Days[0]
		if d.DayAmount != 40000 || d.NightAmount != 0 || d.Amount != 25000 || !d.Capped {
			t.Fatalf("gun dokumu yanlis: %+v", d)
		}
		if d.DayMinutes != 780 {
			t.Fatalf("gun dakikasi 780 olmali, gelen %d", d.DayMinutes)
		}
	})

	t.Run("e iki gun", func(t *testing.T) {
		q, err := Calculate(v, an(t, loc, "2026-10-27 21:00:00"), an(t, loc, "2026-10-28 01:00:00"))
		if err != nil {
			t.Fatal(err)
		}
		ilk, son := q.Days[0], q.Days[1]
		if ilk.DaySeconds != 3600 || ilk.NightSeconds != 7200 || ilk.DayAmount != 4000 || ilk.NightAmount != 3000 || ilk.Amount != 7000 {
			t.Fatalf("ilk gun yanlis: %+v", ilk)
		}
		if son.DaySeconds != 0 || son.NightSeconds != 3600 || son.NightAmount != 1500 || son.Amount != 1500 {
			t.Fatalf("ikinci gun yanlis: %+v", son)
		}
		if ilk.Date != (Date{2026, time.October, 27}) || son.Date != (Date{2026, time.October, 28}) {
			t.Fatalf("tarihler yanlis: %+v %+v", ilk.Date, son.Date)
		}
	})

	t.Run("g her gune ayri birim", func(t *testing.T) {
		q, err := Calculate(v, an(t, loc, "2026-10-27 23:50:00"), an(t, loc, "2026-10-28 00:10:00"))
		if err != nil {
			t.Fatal(err)
		}
		for i, d := range q.Days {
			if d.Amount != 1500 || d.NightAmount != 1500 {
				t.Fatalf("%d. gun yanlis: %+v", i, d)
			}
		}
	})

	t.Run("j cuma cumartesi", func(t *testing.T) {
		q, err := Calculate(v, an(t, loc, "2026-10-30 23:00:00"), an(t, loc, "2026-10-31 08:00:00"))
		if err != nil {
			t.Fatal(err)
		}
		cuma, cumartesi := q.Days[0], q.Days[1]
		if cuma.Amount != 1500 || cuma.RuleName != "hafta_ici" {
			t.Fatalf("cuma yanlis: %+v", cuma)
		}
		if cumartesi.RuleName != "hafta_sonu" || cumartesi.NightSeconds != 25200 || cumartesi.NightAmount != 14000 || cumartesi.DayAmount != 6000 || cumartesi.Amount != 20000 {
			t.Fatalf("cumartesi yanlis: %+v", cumartesi)
		}
	})

	t.Run("i etkinlik kurali", func(t *testing.T) {
		q, err := Calculate(v, an(t, loc, "2026-11-01 10:00:00"), an(t, loc, "2026-11-01 12:30:00"))
		if err != nil {
			t.Fatal(err)
		}
		d := q.Days[0]
		if d.RuleName != "derbi" || d.Amount != 30000 || d.Capped {
			t.Fatalf("etkinlik gunu yanlis: %+v", d)
		}
	})

	t.Run("k tam gunler tavana takilir", func(t *testing.T) {
		q, err := Calculate(v, an(t, loc, "2026-10-27 08:00:00"), an(t, loc, "2026-10-29 08:00:00"))
		if err != nil {
			t.Fatal(err)
		}
		bekle := []Kurus{25000, 25000, 14500}
		tavan := []bool{true, true, false}
		for i, d := range q.Days {
			if d.Amount != bekle[i] || d.Capped != tavan[i] || d.RuleName != "hafta_ici" {
				t.Fatalf("%d. gun yanlis: %+v", i, d)
			}
		}
		if q.Days[0].Date != (Date{2026, time.October, 27}) || q.Days[2].Date != (Date{2026, time.October, 29}) {
			t.Fatalf("tarihler yanlis")
		}
	})
}

func TestYazSaatiBerlin(t *testing.T) {
	berlin := yukle(t, "Europe/Berlin")
	v := Version{
		ID:          "dst",
		ValidFrom:   time.Date(2026, 1, 1, 0, 0, 0, 0, berlin),
		Location:    berlin,
		FreeMinutes: 0,
		Weekday:     DayRule{Name: "tek", Bands: []Band{{0, 60, 100}}},
	}
	giris := an(t, berlin, "2026-10-25 00:00:00")
	cikis := an(t, berlin, "2026-10-26 00:00:00")

	q, err := Calculate(v, giris, cikis)
	if err != nil {
		t.Fatalf("hesap hatasi: %v", err)
	}
	if q.DurationSeconds != 90000 {
		t.Fatalf("gercek sure 90000 sn olmali, gelen %d", q.DurationSeconds)
	}
	if len(q.Days) != 1 {
		t.Fatalf("tek gun beklenirdi, gelen %d", len(q.Days))
	}
	if q.Days[0].DaySeconds != 90000 {
		t.Fatalf("gun saniyesi 90000 olmali, gelen %d", q.Days[0].DaySeconds)
	}
	if q.Total != 2500 {
		t.Fatalf("toplam 2500 olmali, gelen %v", q.Total)
	}
}

func TestGecePenceresiGunduz(t *testing.T) {
	v := standartSurum(t)
	v.Weekday = DayRule{
		Name:  "gece",
		Bands: []Band{{0, 60, 100}},
		Night: &Night{60, 300, 60, 500},
	}
	loc := v.Location
	q, err := Calculate(v, an(t, loc, "2026-10-27 00:00:00"), an(t, loc, "2026-10-27 06:00:00"))
	if err != nil {
		t.Fatalf("hesap hatasi: %v", err)
	}
	if len(q.Days) != 1 {
		t.Fatalf("tek gun beklenirdi, gelen %d", len(q.Days))
	}
	d := q.Days[0]
	if d.NightSeconds != 14400 || d.DaySeconds != 7200 {
		t.Fatalf("saniye bolumu yanlis: %+v", d)
	}
	if d.NightAmount != 2000 || d.DayAmount != 200 || d.Amount != 2200 {
		t.Fatalf("tutar yanlis: %+v", d)
	}
}

func TestUTCGirdiAyniSonuc(t *testing.T) {
	v := standartSurum(t)
	loc := v.Location
	giris := an(t, loc, "2026-10-27 21:00:00")
	cikis := an(t, loc, "2026-10-28 01:00:00")

	qYerel, err := Calculate(v, giris, cikis)
	if err != nil {
		t.Fatal(err)
	}
	qUTC, err := Calculate(v, giris.UTC(), cikis.UTC())
	if err != nil {
		t.Fatal(err)
	}
	if qYerel.Total != qUTC.Total || qYerel.DurationSeconds != qUTC.DurationSeconds || len(qYerel.Days) != len(qUTC.Days) {
		t.Fatalf("UTC girdisi farkli sonuc: %+v / %+v", qYerel, qUTC)
	}
	for i := range qYerel.Days {
		a, b := qYerel.Days[i], qUTC.Days[i]
		if a.Date != b.Date || a.DaySeconds != b.DaySeconds || a.NightSeconds != b.NightSeconds || a.Amount != b.Amount {
			t.Fatalf("%d. gun farkli: %+v / %+v", i, a, b)
		}
	}
}

func TestCalculateHatalari(t *testing.T) {
	v := standartSurum(t)
	loc := v.Location
	giris := an(t, loc, "2026-10-27 10:00:00")

	if _, err := Calculate(v, giris, giris.Add(-time.Second)); !errors.Is(err, ErrNegativeDuration) {
		t.Fatalf("negatif sure icin ErrNegativeDuration beklenirdi, gelen %v", err)
	}

	if _, err := Calculate(v, giris, giris.Add(401*24*time.Hour)); !errors.Is(err, ErrTooLong) {
		t.Fatalf("401 gun icin ErrTooLong beklenirdi, gelen %v", err)
	}

	bozuk := v
	bozuk.Location = nil
	if _, err := Calculate(bozuk, giris, giris.Add(time.Hour)); err == nil {
		t.Fatal("gecersiz surum icin hata beklenirdi")
	}
}

func TestCalculateSifirSure(t *testing.T) {
	v := standartSurum(t)
	giris := an(t, v.Location, "2026-10-27 10:00:00")
	q, err := Calculate(v, giris, giris)
	if err != nil {
		t.Fatalf("hesap hatasi: %v", err)
	}
	if !q.Free || q.Total != 0 || q.Days != nil || q.DurationSeconds != 0 {
		t.Fatalf("sifir sure yanlis: %+v", q)
	}
}

func TestSaniyeAltiYuvarlanir(t *testing.T) {
	v := standartSurum(t)
	loc := v.Location
	giris := an(t, loc, "2026-10-27 10:00:00").Add(900 * time.Millisecond)
	cikis := an(t, loc, "2026-10-27 10:15:00")
	q, err := Calculate(v, giris, cikis)
	if err != nil {
		t.Fatalf("hesap hatasi: %v", err)
	}
	if q.DurationSeconds != 899 || !q.Free {
		t.Fatalf("saniye alti yuvarlanmaliydi: %+v", q)
	}
}
