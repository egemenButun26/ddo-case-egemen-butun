package payment

import (
	"errors"
	"math"
	"testing"
	"time"

	"parkakil/internal/domain/tariff"
)

func TestSettle(t *testing.T) {
	testler := []struct {
		ad       string
		total    tariff.Kurus
		odemeler []Payment
		paid     tariff.Kurus
		due      tariff.Kurus
		overpaid tariff.Kurus
		hata     error
	}{
		{
			ad:    "odeme yok",
			total: 1000,
			due:   1000,
		},
		{
			ad:       "kismi odeme",
			total:    1000,
			odemeler: []Payment{odemeYap(StatusSucceeded, 400, "p1", testT0, zamanEkle(time.Minute))},
			paid:     400,
			due:      600,
		},
		{
			ad:       "tam odeme",
			total:    1000,
			odemeler: []Payment{odemeYap(StatusSucceeded, 1000, "p1", testT0, zamanEkle(time.Minute))},
			paid:     1000,
		},
		{
			ad:       "fazla odeme",
			total:    1000,
			odemeler: []Payment{odemeYap(StatusSucceeded, 1500, "p1", testT0, zamanEkle(time.Minute))},
			paid:     1500,
			overpaid: 500,
		},
		{
			ad:    "bekleyen basarisiz zaman asimli sayilmaz",
			total: 1000,
			odemeler: []Payment{
				odemeYap(StatusPending, 400, "p1", testT0, time.Time{}),
				odemeYap(StatusFailed, 500, "p2", testT0, time.Time{}),
				odemeYap(StatusTimedOut, 600, "p3", testT0, time.Time{}),
			},
			due: 1000,
		},
		{
			ad:    "karisik durumlar",
			total: 1000,
			odemeler: []Payment{
				odemeYap(StatusSucceeded, 300, "p1", testT0, zamanEkle(time.Minute)),
				odemeYap(StatusPending, 1000, "p2", testT0, time.Time{}),
				odemeYap(StatusFailed, 200, "p3", testT0, time.Time{}),
				odemeYap(StatusTimedOut, 100, "p4", testT0, time.Time{}),
			},
			paid: 300,
			due:  700,
		},
		{
			ad:    "iki basarili odeme tam",
			total: 1000,
			odemeler: []Payment{
				odemeYap(StatusSucceeded, 400, "p1", testT0, zamanEkle(time.Minute)),
				odemeYap(StatusSucceeded, 600, "p2", testT0, zamanEkle(2*time.Minute)),
			},
			paid: 1000,
		},
		{
			ad:    "negatif toplam",
			total: -1,
			hata:  tariff.ErrNegativeAmount,
		},
		{
			ad:       "gecersiz odeme",
			total:    1000,
			odemeler: []Payment{odemeYap(StatusSucceeded, 0, "p1", testT0, zamanEkle(time.Minute))},
			hata:     ErrInvalidAmount,
		},
		{
			ad:       "negatif odeme tutari",
			total:    1000,
			odemeler: []Payment{odemeYap(StatusSucceeded, -1, "p1", testT0, zamanEkle(time.Minute))},
			hata:     tariff.ErrNegativeAmount,
		},
		{
			ad:    "toplamda tasma",
			total: 0,
			odemeler: []Payment{
				odemeYap(StatusSucceeded, tariff.Kurus(math.MaxInt64/2+1), "p1", testT0, zamanEkle(time.Minute)),
				odemeYap(StatusSucceeded, tariff.Kurus(math.MaxInt64/2+1), "p2", testT0, zamanEkle(2*time.Minute)),
			},
			hata: tariff.ErrOverflow,
		},
	}

	for _, tt := range testler {
		s, err := Settle(tt.total, tt.odemeler)
		if tt.hata != nil {
			if !errors.Is(err, tt.hata) {
				t.Errorf("%s: hata %v, bekle %v", tt.ad, err, tt.hata)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: beklenmeyen hata: %v", tt.ad, err)
			continue
		}
		if s.Total != tt.total || s.Paid != tt.paid || s.Due != tt.due || s.Overpaid != tt.overpaid {
			t.Errorf("%s: dokum %+v, bekle total=%v paid=%v due=%v overpaid=%v",
				tt.ad, s, tt.total, tt.paid, tt.due, tt.overpaid)
		}
		if s.RefundRequired() != (tt.overpaid > 0) {
			t.Errorf("%s: RefundRequired %v, bekle %v", tt.ad, s.RefundRequired(), tt.overpaid > 0)
		}
	}
}

func TestDue(t *testing.T) {
	due, err := Due(1000, []Payment{odemeYap(StatusSucceeded, 400, "p1", testT0, zamanEkle(time.Minute))})
	if err != nil {
		t.Fatalf("beklenmeyen hata: %v", err)
	}
	if due != 600 {
		t.Errorf("Due %v, bekle 600", due)
	}

	if _, err := Due(-1, nil); !errors.Is(err, tariff.ErrNegativeAmount) {
		t.Errorf("negatif toplam hatasi %v, bekle %v", err, tariff.ErrNegativeAmount)
	}
}

// TestGecOnaySonrasiFazlaOdeme: once baska odeme borcu kapatir, sonra gec onay gelir.
func TestGecOnaySonrasiFazlaOdeme(t *testing.T) {
	erken := odemeYap(StatusSucceeded, 1000, "p1", testT0, zamanEkle(5*time.Minute))
	gec := odemeYap(StatusTimedOut, 1000, "p2", testT0, time.Time{})

	gecSonra, tr, err := Apply(gec, Notification{
		ProviderRef: "p2",
		Outcome:     StatusSucceeded,
		Amount:      1000,
		ProviderAt:  zamanEkle(10 * time.Minute),
		ReceivedAt:  zamanEkle(11 * time.Minute),
	})
	if err != nil {
		t.Fatalf("gec onay uygulanamadi: %v", err)
	}
	if !tr.LateApproval {
		t.Fatalf("LateApproval true olmaliydi")
	}
	if gecSonra.Status != StatusSucceeded {
		t.Fatalf("durum %q, bekle succeeded", gecSonra.Status)
	}

	s, err := Settle(1000, []Payment{erken, gecSonra})
	if err != nil {
		t.Fatalf("Settle hatasi: %v", err)
	}
	if s.Paid != 2000 {
		t.Errorf("Paid %v, bekle 2000", s.Paid)
	}
	if s.Due != 0 {
		t.Errorf("Due %v, bekle 0", s.Due)
	}
	if s.Overpaid != 1000 {
		t.Errorf("Overpaid %v, bekle 1000", s.Overpaid)
	}
	if !s.RefundRequired() {
		t.Errorf("iade gerekli olmaliydi")
	}
}
