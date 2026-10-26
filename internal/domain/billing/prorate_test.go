package billing

import (
	"errors"
	"math"
	"testing"
	"time"

	"parkakil/internal/domain/tariff"
)

func konum(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		t.Fatalf("Europe/Istanbul yuklenemedi: %v", err)
	}
	return loc
}

func gun(t *testing.T, s string) time.Time {
	t.Helper()
	x, err := time.ParseInLocation("2006-01-02 15:04:05", s, konum(t))
	if err != nil {
		t.Fatalf("%q cozulemedi: %v", s, err)
	}
	return x
}

func TestProrate(t *testing.T) {
	t.Run("tam ay fiyatin tamami", func(t *testing.T) {
		testler := []struct {
			ad    string
			bas   string
			son   string
			gun   int
			tutar tariff.Kurus
		}{
			{"31 gunluk ay", "2026-01-01 00:00:00", "2026-02-01 00:00:00", 31, 3100},
			{"30 gunluk ay", "2026-04-01 00:00:00", "2026-05-01 00:00:00", 30, 3000},
			{"28 gunluk subat", "2026-02-01 00:00:00", "2026-03-01 00:00:00", 28, 2800},
			{"29 gunluk subat", "2028-02-01 00:00:00", "2028-03-01 00:00:00", 29, 2900},
		}
		for _, tt := range testler {
			t.Run(tt.ad, func(t *testing.T) {
				got, err := Prorate(tariff.Kurus(tt.gun*100), gun(t, tt.bas), gun(t, tt.son), gun(t, tt.bas), gun(t, tt.son))
				if err != nil {
					t.Fatalf("Prorate: %v", err)
				}
				if got != tt.tutar {
					t.Fatalf("Prorate = %d, beklenen %d", got, tt.tutar)
				}
			})
		}
	})

	t.Run("sifir gun", func(t *testing.T) {
		for _, pair := range [][2]string{
			{"2026-04-01 00:00:00", "2026-04-01 00:00:00"},
			{"2026-04-10 00:00:00", "2026-04-10 00:00:00"},
			{"2026-03-01 00:00:00", "2026-03-20 00:00:00"},
			{"2026-05-01 00:00:00", "2026-05-10 00:00:00"},
		} {
			got, err := Prorate(3000, gun(t, "2026-04-01 00:00:00"), gun(t, "2026-05-01 00:00:00"),
				gun(t, pair[0]), gun(t, pair[1]))
			if err != nil {
				t.Fatalf("Prorate(%s, %s): %v", pair[0], pair[1], err)
			}
			if got != 0 {
				t.Fatalf("Prorate(%s, %s) = %d, beklenen 0", pair[0], pair[1], got)
			}
		}
	})

	t.Run("ay ortasi iptal 10. gun 9 gun kullanilmis", func(t *testing.T) {
		got, err := Prorate(3000, gun(t, "2026-04-01 00:00:00"), gun(t, "2026-05-01 00:00:00"),
			gun(t, "2026-04-01 00:00:00"), gun(t, "2026-04-10 00:00:00"))
		if err != nil {
			t.Fatalf("Prorate: %v", err)
		}
		if got != 900 {
			t.Fatalf("Prorate = %d, beklenen 900", got)
		}
	})

	t.Run("asagi yuvarlama", func(t *testing.T) {
		got, err := Prorate(1000, gun(t, "2026-04-01 00:00:00"), gun(t, "2026-05-01 00:00:00"),
			gun(t, "2026-04-01 00:00:00"), gun(t, "2026-04-02 00:00:00"))
		if err != nil {
			t.Fatalf("Prorate: %v", err)
		}
		if got != 33 {
			t.Fatalf("Prorate = %d, beklenen 33", got)
		}
	})

	t.Run("takvim gunu bazinda", func(t *testing.T) {
		got, err := Prorate(3000, gun(t, "2026-04-01 00:00:00"), gun(t, "2026-05-01 00:00:00"),
			gun(t, "2026-04-01 23:59:59"), gun(t, "2026-04-02 00:00:01"))
		if err != nil {
			t.Fatalf("Prorate: %v", err)
		}
		if got != 100 {
			t.Fatalf("Prorate = %d, beklenen 100", got)
		}
	})

	t.Run("donem sinirlarina kirpma", func(t *testing.T) {
		ps := gun(t, "2026-04-01 00:00:00")
		pe := gun(t, "2026-05-01 00:00:00")
		testler := []struct {
			ad   string
			from string
			to   string
			uret tariff.Kurus
		}{
			{"once baslayan", "2026-03-15 00:00:00", "2026-04-16 00:00:00", 1500},
			{"sonra biten", "2026-04-16 00:00:00", "2026-05-20 00:00:00", 1500},
			{"tamamen disi", "2026-03-01 00:00:00", "2026-03-20 00:00:00", 0},
			{"tamamen sonrasi", "2026-05-02 00:00:00", "2026-05-20 00:00:00", 0},
		}
		for _, tt := range testler {
			t.Run(tt.ad, func(t *testing.T) {
				got, err := Prorate(3000, ps, pe, gun(t, tt.from), gun(t, tt.to))
				if err != nil {
					t.Fatalf("Prorate: %v", err)
				}
				if got != tt.uret {
					t.Fatalf("Prorate = %d, beklenen %d", got, tt.uret)
				}
			})
		}
	})

	t.Run("tasma", func(t *testing.T) {
		_, err := Prorate(tariff.Kurus(math.MaxInt64), gun(t, "2026-04-01 00:00:00"),
			gun(t, "2026-05-01 00:00:00"), gun(t, "2026-04-01 00:00:00"), gun(t, "2026-05-01 00:00:00"))
		if !errors.Is(err, ErrOverflow) {
			t.Fatalf("ErrOverflow beklenirdi, gelen %v", err)
		}
	})

	t.Run("from to'dan sonra", func(t *testing.T) {
		_, err := Prorate(3000, gun(t, "2026-04-01 00:00:00"), gun(t, "2026-05-01 00:00:00"),
			gun(t, "2026-04-10 00:00:00"), gun(t, "2026-04-09 00:00:00"))
		if !errors.Is(err, ErrInvalidRange) {
			t.Fatalf("ErrInvalidRange beklenirdi, gelen %v", err)
		}
	})

	t.Run("negatif fiyat", func(t *testing.T) {
		_, err := Prorate(-1, gun(t, "2026-04-01 00:00:00"), gun(t, "2026-05-01 00:00:00"),
			gun(t, "2026-04-01 00:00:00"), gun(t, "2026-04-10 00:00:00"))
		if !errors.Is(err, ErrNegativeAmount) {
			t.Fatalf("ErrNegativeAmount beklenirdi, gelen %v", err)
		}
	})

	t.Run("gecersiz donem", func(t *testing.T) {
		_, err := Prorate(3000, gun(t, "2026-04-01 00:00:00"), gun(t, "2026-04-01 00:00:00"),
			gun(t, "2026-04-01 00:00:00"), gun(t, "2026-04-02 00:00:00"))
		if !errors.Is(err, ErrInvalidPeriod) {
			t.Fatalf("ErrInvalidPeriod beklenirdi, gelen %v", err)
		}
	})

	t.Run("sifir fiyat", func(t *testing.T) {
		got, err := Prorate(0, gun(t, "2026-04-01 00:00:00"), gun(t, "2026-05-01 00:00:00"),
			gun(t, "2026-04-01 00:00:00"), gun(t, "2026-04-10 00:00:00"))
		if err != nil || got != 0 {
			t.Fatalf("Prorate = %d, %v; 0, nil beklenirdi", got, err)
		}
	})
}

func TestCancelCredit(t *testing.T) {
	ps := gun(t, "2026-04-01 00:00:00")
	pe := gun(t, "2026-05-01 00:00:00")

	got, err := CancelCredit(3000, ps, pe, gun(t, "2026-04-10 00:00:00"))
	if err != nil {
		t.Fatalf("CancelCredit: %v", err)
	}
	if got != 2100 {
		t.Fatalf("CancelCredit = %d, beklenen 2100", got)
	}

	got, err = CancelCredit(3000, ps, pe, gun(t, "2026-04-01 00:00:00"))
	if err != nil {
		t.Fatalf("CancelCredit: %v", err)
	}
	if got != 3000 {
		t.Fatalf("donem basinda iade = %d, beklenen 3000", got)
	}

	got, err = CancelCredit(3000, ps, pe, gun(t, "2026-05-10 00:00:00"))
	if err != nil {
		t.Fatalf("CancelCredit: %v", err)
	}
	if got != 0 {
		t.Fatalf("donem sonrasinda iade = %d, beklenen 0", got)
	}

	if _, err := CancelCredit(-5, ps, pe, gun(t, "2026-04-10 00:00:00")); !errors.Is(err, ErrNegativeAmount) {
		t.Fatalf("ErrNegativeAmount beklenirdi, gelen %v", err)
	}
	if _, err := CancelCredit(3000, pe, ps, gun(t, "2026-04-10 00:00:00")); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("ErrInvalidRange beklenirdi, gelen %v", err)
	}
}

func TestPlanChange(t *testing.T) {
	ps := gun(t, "2026-04-01 00:00:00")
	pe := gun(t, "2026-05-01 00:00:00")

	t.Run("yukseltme net ucret", func(t *testing.T) {
		c, err := ChangePlans(3000, 6000, ps, pe, gun(t, "2026-04-10 00:00:00"))
		if err != nil {
			t.Fatalf("ChangePlans: %v", err)
		}
		if c.Credit != 2100 || c.Charge != 4200 || c.Net != 2100 {
			t.Fatalf("degisim yanlis: %+v", c)
		}
		if c.Direction() != "upgrade" {
			t.Fatalf("yon = %q, beklenen upgrade", c.Direction())
		}
	})

	t.Run("dusurme net kredi", func(t *testing.T) {
		c, err := ChangePlans(6000, 3000, ps, pe, gun(t, "2026-04-10 00:00:00"))
		if err != nil {
			t.Fatalf("ChangePlans: %v", err)
		}
		if c.Credit != 4200 || c.Charge != 2100 || c.Net != -2100 {
			t.Fatalf("degisim yanlis: %+v", c)
		}
		if c.Direction() != "downgrade" {
			t.Fatalf("yon = %q, beklenen downgrade", c.Direction())
		}
	})

	t.Run("ayni gun gecis", func(t *testing.T) {
		c, err := ChangePlans(3000, 6000, ps, pe, ps)
		if err != nil {
			t.Fatalf("ChangePlans: %v", err)
		}
		if c.Credit != 3000 || c.Charge != 6000 || c.Net != 3000 {
			t.Fatalf("degisim yanlis: %+v", c)
		}
	})

	t.Run("ayni fiyat", func(t *testing.T) {
		c, err := ChangePlans(3000, 3000, ps, pe, gun(t, "2026-04-10 00:00:00"))
		if err != nil {
			t.Fatalf("ChangePlans: %v", err)
		}
		if c.Net != 0 || c.Direction() != "even" {
			t.Fatalf("degisim yanlis: %+v", c)
		}
	})

	t.Run("donem sonunda sifir", func(t *testing.T) {
		c, err := ChangePlans(3000, 6000, ps, pe, pe)
		if err != nil {
			t.Fatalf("ChangePlans: %v", err)
		}
		if c.Charge != 0 || c.Credit != 0 || c.Net != 0 {
			t.Fatalf("degisim yanlis: %+v", c)
		}
	})

	t.Run("hatalar", func(t *testing.T) {
		if _, err := ChangePlans(-1, 6000, ps, pe, ps); !errors.Is(err, ErrNegativeAmount) {
			t.Fatalf("ErrNegativeAmount beklenirdi, gelen %v", err)
		}
		if _, err := ChangePlans(3000, 6000, pe, ps, ps); !errors.Is(err, ErrInvalidPeriod) {
			t.Fatalf("ErrInvalidPeriod beklenirdi, gelen %v", err)
		}
	})
}
