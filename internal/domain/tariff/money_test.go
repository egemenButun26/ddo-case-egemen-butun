package tariff

import (
	"errors"
	"math"
	"testing"
)

func TestKurusString(t *testing.T) {
	testler := []struct {
		k     Kurus
		bekle string
	}{
		{0, "0,00 TL"},
		{5, "0,05 TL"},
		{123450, "1.234,50 TL"},
		{-150, "-1,50 TL"},
		{-5, "-0,05 TL"},
		{100000000, "1.000.000,00 TL"},
		{Kurus(math.MaxInt64), "92.233.720.368.547.758,07 TL"},
	}
	for _, tt := range testler {
		if got := tt.k.String(); got != tt.bekle {
			t.Errorf("%d.String() = %q, bekle %q", int64(tt.k), got, tt.bekle)
		}
	}
}

func TestMulKurus(t *testing.T) {
	got, err := mulKurus(3, 1250)
	if err != nil || got != 3750 {
		t.Fatalf("mulKurus(3, 1250) = %v, %v", got, err)
	}
	if got, err := mulKurus(0, 1250); err != nil || got != 0 {
		t.Fatalf("mulKurus(0, ...) = %v, %v", got, err)
	}
	if got, err := mulKurus(7, 0); err != nil || got != 0 {
		t.Fatalf("mulKurus(7, 0) = %v, %v", got, err)
	}
	if _, err := mulKurus(2, Kurus(math.MaxInt64)); !errors.Is(err, ErrOverflow) {
		t.Fatalf("tasma beklenirdi, gelen %v", err)
	}
	if _, err := mulKurus(-1, 10); !errors.Is(err, ErrNegativeAmount) {
		t.Fatalf("negatif birim icin ErrNegativeAmount beklenirdi, gelen %v", err)
	}
	if got, err := mulKurus(1, Kurus(math.MaxInt64)); err != nil || int64(got) != math.MaxInt64 {
		t.Fatalf("sinir carpimi = %v, %v", got, err)
	}
}

func TestAddKurus(t *testing.T) {
	if got, err := addKurus(5, 7); err != nil || got != 12 {
		t.Fatalf("addKurus(5, 7) = %v, %v", got, err)
	}
	if _, err := addKurus(Kurus(math.MaxInt64), 1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("pozitif tasma beklenirdi, gelen %v", err)
	}
	if _, err := addKurus(Kurus(math.MinInt64), -1); !errors.Is(err, ErrOverflow) {
		t.Fatalf("negatif tasma beklenirdi, gelen %v", err)
	}
}
