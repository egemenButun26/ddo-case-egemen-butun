package tariff

import (
	"errors"
	"math"
	"testing"
	"time"
)

func sinirSurum() Version {
	return Version{
		ID:        "sinir",
		ValidFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Location:  time.UTC,
		Weekday: DayRule{
			Name:  "hafta_ici",
			Bands: []Band{{UpToMinutes: 60, UnitMinutes: 60, UnitPrice: 100}, {UnitMinutes: 60, UnitPrice: 500}},
		},
	}
}

func geceSurum(unitMinutes int) *Night {
	return &Night{StartMinute: 1320, EndMinute: 420, UnitMinutes: unitMinutes, UnitPrice: 1000}
}

func TestValidateSinirlar(t *testing.T) {
	testler := []struct {
		ad      string
		boz     func(*Version)
		gecerli bool
	}{
		{"FreeMinutes 0", func(v *Version) { v.FreeMinutes = 0 }, true},
		{"FreeMinutes -1", func(v *Version) { v.FreeMinutes = -1 }, false},
		{"FreeMinutes MaxTariffMinutes", func(v *Version) { v.FreeMinutes = MaxTariffMinutes }, true},
		{"FreeMinutes MaxTariffMinutes+1", func(v *Version) { v.FreeMinutes = MaxTariffMinutes + 1 }, false},
		{"FreeMinutes MaxInt64/60", func(v *Version) { v.FreeMinutes = math.MaxInt64 / 60 }, false},
		{"FreeMinutes MaxInt64/60+1", func(v *Version) { v.FreeMinutes = math.MaxInt64/60 + 1 }, false},
		{"FreeMinutes MaxInt64/30", func(v *Version) { v.FreeMinutes = math.MaxInt64 / 30 }, false},
		{"FreeMinutes 1<<62+60", func(v *Version) { v.FreeMinutes = 1<<62 + 60 }, false},
		{"FreeMinutes MaxInt", func(v *Version) { v.FreeMinutes = math.MaxInt }, false},

		{"UnitMinutes 0", func(v *Version) { v.Weekday.Bands[0].UnitMinutes = 0 }, false},
		{"UnitMinutes -1", func(v *Version) { v.Weekday.Bands[0].UnitMinutes = -1 }, false},
		{"UnitMinutes MaxTariffMinutes", func(v *Version) { v.Weekday.Bands[0].UnitMinutes = MaxTariffMinutes }, true},
		{"UnitMinutes MaxTariffMinutes+1", func(v *Version) { v.Weekday.Bands[0].UnitMinutes = MaxTariffMinutes + 1 }, false},
		{"UnitMinutes MaxInt64/60", func(v *Version) { v.Weekday.Bands[0].UnitMinutes = math.MaxInt64 / 60 }, false},
		{"UnitMinutes MaxInt64/60+1", func(v *Version) { v.Weekday.Bands[0].UnitMinutes = math.MaxInt64/60 + 1 }, false},
		{"UnitMinutes MaxInt64/30", func(v *Version) { v.Weekday.Bands[0].UnitMinutes = math.MaxInt64 / 30 }, false},
		{"UnitMinutes 1<<62+60", func(v *Version) { v.Weekday.Bands[0].UnitMinutes = 1<<62 + 60 }, false},
		{"UnitMinutes MaxInt", func(v *Version) { v.Weekday.Bands[0].UnitMinutes = math.MaxInt }, false},

		{"UpToMinutes 0", func(v *Version) { v.Weekday.Bands[0].UpToMinutes = 0 }, false},
		{"UpToMinutes -1", func(v *Version) { v.Weekday.Bands[0].UpToMinutes = -1 }, false},
		{"UpToMinutes MaxTariffMinutes", func(v *Version) { v.Weekday.Bands[0].UpToMinutes = MaxTariffMinutes }, true},
		{"UpToMinutes MaxTariffMinutes+1", func(v *Version) { v.Weekday.Bands[0].UpToMinutes = MaxTariffMinutes + 1 }, false},
		{"UpToMinutes MaxInt64/60", func(v *Version) { v.Weekday.Bands[0].UpToMinutes = math.MaxInt64 / 60 }, false},
		{"UpToMinutes MaxInt64/60+1", func(v *Version) { v.Weekday.Bands[0].UpToMinutes = math.MaxInt64/60 + 1 }, false},
		{"UpToMinutes MaxInt64/30", func(v *Version) { v.Weekday.Bands[0].UpToMinutes = math.MaxInt64 / 30 }, false},
		{"UpToMinutes 1<<62+60", func(v *Version) { v.Weekday.Bands[0].UpToMinutes = 1<<62 + 60 }, false},
		{"UpToMinutes MaxInt", func(v *Version) { v.Weekday.Bands[0].UpToMinutes = math.MaxInt }, false},

		{"Night.UnitMinutes 0", func(v *Version) { v.Weekday.Night = geceSurum(0) }, false},
		{"Night.UnitMinutes -1", func(v *Version) { v.Weekday.Night = geceSurum(-1) }, false},
		{"Night.UnitMinutes MaxTariffMinutes", func(v *Version) { v.Weekday.Night = geceSurum(MaxTariffMinutes) }, true},
		{"Night.UnitMinutes MaxTariffMinutes+1", func(v *Version) { v.Weekday.Night = geceSurum(MaxTariffMinutes + 1) }, false},
		{"Night.UnitMinutes MaxInt64/60", func(v *Version) { v.Weekday.Night = geceSurum(math.MaxInt64 / 60) }, false},
		{"Night.UnitMinutes MaxInt64/60+1", func(v *Version) { v.Weekday.Night = geceSurum(math.MaxInt64/60 + 1) }, false},
		{"Night.UnitMinutes MaxInt64/30", func(v *Version) { v.Weekday.Night = geceSurum(math.MaxInt64 / 30) }, false},
		{"Night.UnitMinutes 1<<62+60", func(v *Version) { v.Weekday.Night = geceSurum(1<<62 + 60) }, false},
		{"Night.UnitMinutes MaxInt", func(v *Version) { v.Weekday.Night = geceSurum(math.MaxInt) }, false},
	}

	for _, tt := range testler {
		t.Run(tt.ad, func(t *testing.T) {
			v := sinirSurum()
			tt.boz(&v)
			err := v.Validate()
			if tt.gecerli {
				if err != nil {
					t.Fatalf("gecerli deger reddedildi: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("gecersiz deger kabul edildi")
			}
			if !errors.Is(err, ErrInvalidTariff) {
				t.Fatalf("ErrInvalidTariff beklenirdi, gelen %v", err)
			}
		})
	}
}

func TestValidateSinirlarHaftaSonuEtkinlik(t *testing.T) {
	t.Run("hafta sonu UnitMinutes", func(t *testing.T) {
		v := sinirSurum()
		v.Weekend = &DayRule{
			Name:  "hafta_sonu",
			Bands: []Band{{UnitMinutes: MaxTariffMinutes + 1, UnitPrice: 100}, {UnitMinutes: 60, UnitPrice: 500}},
		}
		if err := v.Validate(); !errors.Is(err, ErrInvalidTariff) {
			t.Fatalf("hafta sonu icin ErrInvalidTariff beklenirdi, gelen %v", err)
		}
	})

	t.Run("etkinlik UpToMinutes", func(t *testing.T) {
		v := sinirSurum()
		v.Events = []Event{{
			Date: Date{Year: 2026, Month: time.November, Day: 1},
			Name: "sinir",
			Rule: DayRule{
				Bands: []Band{{UpToMinutes: MaxTariffMinutes + 1, UnitMinutes: 60, UnitPrice: 100}, {UnitMinutes: 60, UnitPrice: 500}},
			},
		}}
		if err := v.Validate(); !errors.Is(err, ErrInvalidTariff) {
			t.Fatalf("etkinlik icin ErrInvalidTariff beklenirdi, gelen %v", err)
		}
	})
}

func hesapSurum(t *testing.T) Version {
	t.Helper()
	loc := yukle(t, "Europe/Istanbul")
	return Version{
		ID:        "hesap",
		ValidFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, loc),
		Location:  loc,
		Weekday: DayRule{
			Name:  "hafta_ici",
			Bands: []Band{{UpToMinutes: 60, UnitMinutes: 60, UnitPrice: 100}, {UnitMinutes: 60, UnitPrice: 500}},
		},
	}
}

func TestCalculateTasmaReddedilir(t *testing.T) {
	loc := yukle(t, "Europe/Istanbul")
	giris := an(t, loc, "2026-10-27 10:00:00")
	cikis := an(t, loc, "2026-10-27 13:00:00")

	normal := hesapSurum(t)
	q, err := Calculate(normal, giris, cikis)
	if err != nil {
		t.Fatalf("normal hesap: %v", err)
	}
	if q.Total != 1100 {
		t.Fatalf("normal toplam 1100 olmali, gelen %v", q.Total)
	}

	testler := []struct {
		ad  string
		boz func(*Version)
	}{
		{"Onceki davranis: UpToMinutes MaxInt64/30", func(v *Version) { v.Weekday.Bands[0].UpToMinutes = math.MaxInt64 / 30 }},
		{"Onceki davranis: FreeMinutes MaxInt64/60+1", func(v *Version) { v.FreeMinutes = math.MaxInt64/60 + 1 }},
		{"Onceki davranis: UnitMinutes MaxInt64/60+2", func(v *Version) { v.Weekday.Bands[1].UnitMinutes = math.MaxInt64/60 + 2 }},
		{"Onceki davranis: UnitMinutes sarma", func(v *Version) { v.Weekday.Bands[1].UnitMinutes = 1<<62 + 60 }},
	}

	for _, tt := range testler {
		t.Run(tt.ad, func(t *testing.T) {
			v := hesapSurum(t)
			tt.boz(&v)
			q, err := Calculate(v, giris, cikis)
			if err == nil {
				t.Fatal("hata beklenirdi, sessiz tutar dondu: " + q.Total.String())
			}
			if !errors.Is(err, ErrInvalidTariff) {
				t.Fatalf("ErrInvalidTariff beklenirdi, gelen %v", err)
			}
			if q.Total != 0 {
				t.Fatalf("hata halinde Total 0 olmali, gelen %v", q.Total)
			}
		})
	}
}

func TestCalculateSinirdaDogru(t *testing.T) {
	loc := yukle(t, "Europe/Istanbul")
	giris := an(t, loc, "2026-10-27 10:00:00")
	cikis := an(t, loc, "2026-10-27 13:00:00")

	t.Run("UpToMinutes MaxTariffMinutes", func(t *testing.T) {
		v := hesapSurum(t)
		v.Weekday.Bands = []Band{{UpToMinutes: MaxTariffMinutes, UnitMinutes: 60, UnitPrice: 100}, {UnitMinutes: 60, UnitPrice: 500}}
		q, err := Calculate(v, giris, cikis)
		if err != nil {
			t.Fatalf("hesap: %v", err)
		}
		if q.Total != 300 {
			t.Fatalf("toplam 300 olmali, gelen %v", q.Total)
		}
	})

	t.Run("FreeMinutes MaxTariffMinutes", func(t *testing.T) {
		v := hesapSurum(t)
		v.FreeMinutes = MaxTariffMinutes
		q, err := Calculate(v, giris, cikis)
		if err != nil {
			t.Fatalf("hesap: %v", err)
		}
		if !q.Free || q.Total != 0 || q.Days != nil {
			t.Fatalf("ucretsiz olmaliydi: %+v", q)
		}
	})

	t.Run("UnitMinutes MaxTariffMinutes tek birim", func(t *testing.T) {
		v := hesapSurum(t)
		v.Weekday.Bands = []Band{{UnitMinutes: MaxTariffMinutes, UnitPrice: 100}}
		q, err := Calculate(v, giris, cikis)
		if err != nil {
			t.Fatalf("hesap: %v", err)
		}
		if q.Total != 100 {
			t.Fatalf("tek birim 100 olmali, gelen %v", q.Total)
		}
		if len(q.Days) != 1 || q.Days[0].Amount != 100 {
			t.Fatalf("gun dokumu yanlis: %+v", q.Days)
		}
	})
}

func TestDakikaSaniye(t *testing.T) {
	if _, err := dakikaSaniye(-1); !errors.Is(err, ErrInvalidTariff) {
		t.Fatalf("negatif icin ErrInvalidTariff beklenirdi, gelen %v", err)
	}
	if got, err := dakikaSaniye(0); err != nil || got != 0 {
		t.Fatalf("dakikaSaniye(0) = %d, %v", got, err)
	}
	if got, err := dakikaSaniye(60); err != nil || got != 3600 {
		t.Fatalf("dakikaSaniye(60) = %d, %v", got, err)
	}
	sinir := int(math.MaxInt64 / 60)
	if got, err := dakikaSaniye(sinir); err != nil || got != int64(sinir)*60 {
		t.Fatalf("sinir dakikaSaniye = %d, %v", got, err)
	}
	if _, err := dakikaSaniye(sinir + 1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("tasma icin ErrOverflow beklenirdi, gelen %v", err)
	}
}

func TestGunduzTutariSavunmaDerinligi(t *testing.T) {
	testler := []struct {
		ad   string
		band Band
	}{
		{"cok buyuk UpToMinutes", Band{UpToMinutes: math.MaxInt64 / 30, UnitMinutes: 60, UnitPrice: 100}},
		{"cok buyuk UnitMinutes", Band{UnitMinutes: math.MaxInt64/60 + 2, UnitPrice: 100}},
	}
	for _, tt := range testler {
		t.Run(tt.ad, func(t *testing.T) {
			tutar, err := gunduzTutari([]Band{tt.band}, 3*60*60)
			if err == nil {
				t.Fatalf("hata beklenirdi, sessiz tutar dondu: %v", tutar)
			}
			if tutar != 0 {
				t.Fatalf("hata halinde tutar 0 olmali, gelen %v", tutar)
			}
		})
	}
}

func TestMulKurusNegatifVeTasma(t *testing.T) {
	if _, err := mulKurus(-1, 10); !errors.Is(err, ErrNegativeAmount) {
		t.Fatalf("negatif birim icin ErrNegativeAmount beklenirdi, gelen %v", err)
	}
	if _, err := mulKurus(1, -10); !errors.Is(err, ErrNegativeAmount) {
		t.Fatalf("negatif fiyat icin ErrNegativeAmount beklenirdi, gelen %v", err)
	}
	if _, err := mulKurus(2, Kurus(math.MaxInt64)); !errors.Is(err, ErrOverflow) {
		t.Fatalf("tasma icin ErrOverflow beklenirdi, gelen %v", err)
	}
}
