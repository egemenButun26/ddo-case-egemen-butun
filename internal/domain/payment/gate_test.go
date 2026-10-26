package payment

import (
	"errors"
	"math"
	"testing"
	"time"

	"parkakil/internal/domain/plate"
	"parkakil/internal/domain/tariff"
)

func sabitFiyat(v tariff.Kurus) func(time.Time) (tariff.Kurus, error) {
	return func(time.Time) (tariff.Kurus, error) {
		return v, nil
	}
}

func fiyatEgrisi(esik time.Time, once, sonra tariff.Kurus) func(time.Time) (tariff.Kurus, error) {
	return func(at time.Time) (tariff.Kurus, error) {
		if at.After(esik) {
			return sonra, nil
		}
		return once, nil
	}
}

func kademeliFiyat(adim time.Duration, tutar tariff.Kurus) func(time.Time) (tariff.Kurus, error) {
	return func(at time.Time) (tariff.Kurus, error) {
		gecen := at.Sub(testT0)
		dilim := tariff.Kurus(gecen / adim)
		if gecen%adim != 0 {
			dilim++
		}
		return dilim * tutar, nil
	}
}

func temelGirdi() GateInput {
	return GateInput{
		Match:     plate.DecisionExact,
		Found:     true,
		EnteredAt: testT0,
		Now:       zamanEkle(30 * time.Minute),
		Quote:     sabitFiyat(1000),
	}
}

func TestDecideGateOdemeler(t *testing.T) {
	testler := []struct {
		ad      string
		girdi   func() GateInput
		action  Action
		reason  string
		due     tariff.Kurus
		borc    tariff.Kurus
		izin    bool
		izinAni time.Time
	}{
		{
			ad:     "odeme yok",
			girdi:  temelGirdi,
			action: ActionDeny,
			reason: ReasonPaymentRequired,
			due:    1000,
		},
		{
			ad: "kismi odeme",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Now = zamanEkle(20 * time.Minute)
				in.Payments = []Payment{odemeYap(StatusSucceeded, 400, "p1", testT0, zamanEkle(10*time.Minute))}
				return in
			},
			action: ActionDeny,
			reason: ReasonPaymentRequired,
			due:    600,
		},
		{
			ad: "basarili odeme",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Now = zamanEkle(10 * time.Minute)
				in.Payments = []Payment{odemeYap(StatusSucceeded, 1000, "p1", testT0, zamanEkle(5*time.Minute))}
				return in
			},
			action: ActionOpen,
			reason: ReasonPaid,
		},
		{
			ad: "basarisiz odeme sayilmaz",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Now = zamanEkle(10 * time.Minute)
				in.Payments = []Payment{odemeYap(StatusFailed, 1000, "p1", testT0, time.Time{})}
				return in
			},
			action: ActionDeny,
			reason: ReasonPaymentRequired,
			due:    1000,
		},
		{
			ad: "bekleyen odeme",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Now = zamanEkle(10 * time.Minute)
				in.Payments = []Payment{odemeYap(StatusPending, 1000, "p1", testT0, time.Time{})}
				return in
			},
			action: ActionDeny,
			reason: ReasonPaymentPending,
			due:    1000,
		},
		{
			ad: "zaman asimli odeme sayilmaz",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Now = zamanEkle(10 * time.Minute)
				in.Payments = []Payment{odemeYap(StatusTimedOut, 1000, "p1", testT0, time.Time{})}
				return in
			},
			action: ActionDeny,
			reason: ReasonPaymentRequired,
			due:    1000,
		},
		{
			ad: "ilk on bes dakika ucretsiz",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Now = zamanEkle(10 * time.Minute)
				in.Quote = sabitFiyat(0)
				return in
			},
			action: ActionOpen,
			reason: ReasonNoCharge,
		},
		{
			ad: "izin alt sinirinda acik",
			girdi: func() GateInput {
				odemeAni := zamanEkle(5 * time.Minute)
				in := temelGirdi()
				in.Now = odemeAni
				in.Payments = []Payment{odemeYap(StatusSucceeded, 1000, "p1", testT0, odemeAni)}
				return in
			},
			action: ActionOpen,
			reason: ReasonPaid,
		},
		{
			ad: "izin alt sinirinda odeme aninda tam odendi paid",
			girdi: func() GateInput {
				odemeAni := zamanEkle(15 * time.Minute)
				in := temelGirdi()
				in.Now = odemeAni
				in.Quote = kademeliFiyat(PermitWindow, 1000)
				in.Payments = []Payment{odemeYap(StatusSucceeded, 1000, "p1", testT0, odemeAni)}
				return in
			},
			action: ActionOpen,
			reason: ReasonPaid,
		},
		{
			ad: "izin alt sinirinda artan ucret permit_valid",
			girdi: func() GateInput {
				odemeAni := zamanEkle(15 * time.Minute)
				in := temelGirdi()
				in.Now = odemeAni.Add(time.Nanosecond)
				in.Quote = kademeliFiyat(PermitWindow, 1000)
				in.Payments = []Payment{odemeYap(StatusSucceeded, 1000, "p1", testT0, odemeAni)}
				return in
			},
			action:  ActionOpen,
			reason:  ReasonPermitValid,
			izin:    true,
			izinAni: zamanEkle(15 * time.Minute),
		},
		{
			ad: "izin ust sinirina kadar acik",
			girdi: func() GateInput {
				odemeAni := zamanEkle(5 * time.Minute)
				in := temelGirdi()
				in.Now = odemeAni.Add(PermitWindow - time.Nanosecond)
				in.Quote = fiyatEgrisi(odemeAni, 1000, 2000)
				in.Payments = []Payment{odemeYap(StatusSucceeded, 1000, "p1", testT0, odemeAni)}
				return in
			},
			action: ActionOpen,
			reason: ReasonPermitValid,
			izin:   true,
		},
		{
			ad: "izin tam ust sinirda doldu ek ucret var",
			girdi: func() GateInput {
				odemeAni := zamanEkle(5 * time.Minute)
				in := temelGirdi()
				in.Now = odemeAni.Add(PermitWindow)
				in.Quote = fiyatEgrisi(odemeAni, 1000, 2000)
				in.Payments = []Payment{odemeYap(StatusSucceeded, 1000, "p1", testT0, odemeAni)}
				return in
			},
			action: ActionDeny,
			reason: ReasonPermitExpired,
			due:    1000,
		},
		{
			ad: "izin tam ust sinirda doldu ek ucret yok",
			girdi: func() GateInput {
				odemeAni := zamanEkle(5 * time.Minute)
				in := temelGirdi()
				in.Now = odemeAni.Add(PermitWindow)
				in.Quote = sabitFiyat(1000)
				in.Payments = []Payment{odemeYap(StatusSucceeded, 1000, "p1", testT0, odemeAni)}
				return in
			},
			action: ActionOpen,
			reason: ReasonPaid,
		},
		{
			ad: "gec onay onay ani izin icinde",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Now = zamanEkle(30 * time.Minute)
				onayAni := zamanEkle(20 * time.Minute)
				p := odemeYap(StatusSucceeded, 1000, "p1", testT0, onayAni)
				p.ConfirmedAt = zamanEkle(29 * time.Minute)
				in.Quote = fiyatEgrisi(onayAni, 1000, 2000)
				in.Payments = []Payment{p}
				return in
			},
			action:  ActionOpen,
			reason:  ReasonPermitValid,
			izin:    true,
			izinAni: zamanEkle(20 * time.Minute),
		},
		{
			ad: "gec onay izin onay anindan sayilir",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Now = zamanEkle(30 * time.Minute)
				onayAni := zamanEkle(10 * time.Minute)
				p := odemeYap(StatusSucceeded, 1000, "p1", testT0, onayAni)
				p.ConfirmedAt = zamanEkle(29 * time.Minute)
				in.Quote = fiyatEgrisi(onayAni, 1000, 2000)
				in.Payments = []Payment{p}
				return in
			},
			action: ActionDeny,
			reason: ReasonPermitExpired,
			due:    1000,
		},
		{
			ad: "izin bittikten sonra kismi odeme",
			girdi: func() GateInput {
				odemeAni := zamanEkle(5 * time.Minute)
				in := temelGirdi()
				in.Now = odemeAni.Add(PermitWindow + time.Minute)
				in.Quote = fiyatEgrisi(odemeAni, 1000, 2000)
				in.Payments = []Payment{odemeYap(StatusSucceeded, 500, "p1", testT0, odemeAni)}
				return in
			},
			action: ActionDeny,
			reason: ReasonPermitExpired,
			due:    1500,
		},
		{
			ad: "onceki borc odeme yapilsa bile deny",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Now = zamanEkle(10 * time.Minute)
				in.PriorDebt = 500
				in.Payments = []Payment{odemeYap(StatusSucceeded, 1000, "p1", testT0, zamanEkle(5*time.Minute))}
				return in
			},
			action: ActionDeny,
			reason: ReasonPriorDebt,
			borc:   500,
		},
		{
			ad: "onceki borc odemesiz",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Now = zamanEkle(10 * time.Minute)
				in.PriorDebt = 500
				return in
			},
			action: ActionDeny,
			reason: ReasonPriorDebt,
			due:    1000,
			borc:   500,
		},
		{
			ad: "kismi odeme izin tam ust sinirda doldu",
			girdi: func() GateInput {
				odemeAni := zamanEkle(5 * time.Minute)
				in := temelGirdi()
				in.Now = odemeAni.Add(PermitWindow)
				in.Payments = []Payment{odemeYap(StatusSucceeded, 400, "p1", testT0, odemeAni)}
				return in
			},
			action: ActionDeny,
			reason: ReasonPermitExpired,
			due:    600,
		},
		{
			ad: "kismi odeme izin dolmadan bir nanosaniye once",
			girdi: func() GateInput {
				odemeAni := zamanEkle(5 * time.Minute)
				in := temelGirdi()
				in.Now = odemeAni.Add(PermitWindow - time.Nanosecond)
				in.Payments = []Payment{odemeYap(StatusSucceeded, 400, "p1", testT0, odemeAni)}
				return in
			},
			action: ActionDeny,
			reason: ReasonPaymentRequired,
			due:    600,
		},
		{
			ad: "izin doldu yeni bekleyen odeme",
			girdi: func() GateInput {
				odemeAni := zamanEkle(5 * time.Minute)
				in := temelGirdi()
				in.Now = odemeAni.Add(PermitWindow + time.Minute)
				in.Payments = []Payment{
					odemeYap(StatusSucceeded, 500, "p1", testT0, odemeAni),
					odemeYap(StatusPending, 1000, "p2", testT0, time.Time{}),
				}
				return in
			},
			action: ActionDeny,
			reason: ReasonPaymentPending,
			due:    500,
		},
		{
			ad: "izin doldu yeni zaman asimli odeme",
			girdi: func() GateInput {
				odemeAni := zamanEkle(5 * time.Minute)
				in := temelGirdi()
				in.Now = odemeAni.Add(PermitWindow + time.Minute)
				in.Payments = []Payment{
					odemeYap(StatusSucceeded, 500, "p1", testT0, odemeAni),
					odemeYap(StatusTimedOut, 1000, "p2", testT0, time.Time{}),
				}
				return in
			},
			action: ActionDeny,
			reason: ReasonPermitExpired,
			due:    500,
		},
		{
			ad: "izin doldu yeni basarisiz odeme",
			girdi: func() GateInput {
				odemeAni := zamanEkle(5 * time.Minute)
				in := temelGirdi()
				in.Now = odemeAni.Add(PermitWindow + time.Minute)
				in.Payments = []Payment{
					odemeYap(StatusSucceeded, 500, "p1", testT0, odemeAni),
					odemeYap(StatusFailed, 1000, "p2", testT0, time.Time{}),
				}
				return in
			},
			action: ActionDeny,
			reason: ReasonPermitExpired,
			due:    500,
		},
	}

	for _, tt := range testler {
		d, err := DecideGate(tt.girdi())
		if err != nil {
			t.Errorf("%s: beklenmeyen hata: %v", tt.ad, err)
			continue
		}
		if d.Action != tt.action || d.Reason != tt.reason {
			t.Errorf("%s: karar %q/%q, bekle %q/%q", tt.ad, d.Action, d.Reason, tt.action, tt.reason)
		}
		if d.Due != tt.due {
			t.Errorf("%s: Due %v, bekle %v", tt.ad, d.Due, tt.due)
		}
		if d.PriorDebt != tt.borc {
			t.Errorf("%s: PriorDebt %v, bekle %v", tt.ad, d.PriorDebt, tt.borc)
		}
		if tt.izin {
			izinAni := tt.izinAni
			if izinAni.IsZero() {
				izinAni = zamanEkle(5 * time.Minute)
			}
			bekle := izinAni.Add(PermitWindow)
			if !d.PermitExpiresAt.Equal(bekle) {
				t.Errorf("%s: PermitExpiresAt %v, bekle %v", tt.ad, d.PermitExpiresAt, bekle)
			}
		} else if !d.PermitExpiresAt.IsZero() {
			t.Errorf("%s: izin disinda PermitExpiresAt dolu: %v", tt.ad, d.PermitExpiresAt)
		}
	}
}

func TestDecideGatePlaka(t *testing.T) {
	testler := []struct {
		ad       string
		match    plate.Decision
		found    bool
		payments []Payment
		action   Action
		reason   string
		hata     error
	}{
		{"eslesme yok", plate.DecisionNone, true, nil, ActionDeny, ReasonNoMatch, nil},
		{"oturum bulunamadi", plate.DecisionExact, false, nil, ActionDeny, ReasonNoMatch, nil},
		{"otomatik eslesme", plate.DecisionAuto, true, nil, ActionDeny, ReasonPaymentRequired, nil},
		{"tam eslesme", plate.DecisionExact, true, nil, ActionDeny, ReasonPaymentRequired, nil},
		{"dusuk guven", plate.DecisionReview, true, nil, ActionDeny, ReasonLowConfidence, nil},
		{
			"dusuk guven, tam odemeli",
			plate.DecisionReview,
			true,
			[]Payment{odemeYap(StatusSucceeded, 1000, "p1", testT0, zamanEkle(5*time.Minute))},
			ActionDeny,
			ReasonLowConfidence,
			nil,
		},
		{"bilinmeyen karar", plate.Decision("weird"), true, nil, "", "", ErrInvalidGateInput},
	}

	for _, tt := range testler {
		in := temelGirdi()
		in.Match = tt.match
		in.Found = tt.found
		in.Payments = tt.payments
		d, err := DecideGate(in)
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
		if d.Action != tt.action || d.Reason != tt.reason {
			t.Errorf("%s: karar %q/%q, bekle %q/%q", tt.ad, d.Action, d.Reason, tt.action, tt.reason)
		}
	}
}

func TestDecideGateGecersizGirdi(t *testing.T) {
	testler := []struct {
		ad    string
		girdi func() GateInput
		hata  error
	}{
		{
			ad: "nil fiyat fonksiyonu",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Quote = nil
				return in
			},
			hata: ErrInvalidGateInput,
		},
		{
			ad: "bos giris zamani",
			girdi: func() GateInput {
				in := temelGirdi()
				in.EnteredAt = time.Time{}
				return in
			},
			hata: ErrInvalidGateInput,
		},
		{
			ad: "bos simdiki zaman",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Now = time.Time{}
				return in
			},
			hata: ErrInvalidGateInput,
		},
		{
			ad: "simdiki zaman giristen once",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Now = testT0.Add(-time.Second)
				return in
			},
			hata: ErrInvalidGateInput,
		},
		{
			ad: "negatif onceki borc",
			girdi: func() GateInput {
				in := temelGirdi()
				in.PriorDebt = -1
				return in
			},
			hata: tariff.ErrNegativeAmount,
		},
		{
			ad: "negatif odeme tutari",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Payments = []Payment{odemeYap(StatusSucceeded, -1, "p1", testT0, zamanEkle(5*time.Minute))}
				return in
			},
			hata: tariff.ErrNegativeAmount,
		},
		{
			ad: "sifir odeme tutari",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Payments = []Payment{odemeYap(StatusSucceeded, 0, "p1", testT0, zamanEkle(5*time.Minute))}
				return in
			},
			hata: ErrInvalidAmount,
		},
		{
			ad: "gelecekteki onay zamani",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Now = zamanEkle(10 * time.Minute)
				p := odemeYap(StatusSucceeded, 1000, "p1", testT0, zamanEkle(5*time.Minute))
				p.ConfirmedAt = zamanEkle(11 * time.Minute)
				in.Payments = []Payment{p}
				return in
			},
			hata: ErrInvalidGateInput,
		},
		{
			ad: "gelecekteki odeme zamani",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Now = zamanEkle(10 * time.Minute)
				in.Payments = []Payment{odemeYap(StatusSucceeded, 1000, "p1", testT0, zamanEkle(20*time.Minute))}
				return in
			},
			hata: ErrInvalidGateInput,
		},
		{
			ad: "toplamda tasma",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Now = zamanEkle(10 * time.Minute)
				in.Payments = []Payment{
					odemeYap(StatusSucceeded, tariff.Kurus(math.MaxInt64/2+1), "p1", testT0, zamanEkle(5*time.Minute)),
					odemeYap(StatusSucceeded, tariff.Kurus(math.MaxInt64/2+1), "p2", testT0, zamanEkle(6*time.Minute)),
				}
				return in
			},
			hata: tariff.ErrOverflow,
		},
		{
			ad: "onceki borcta tasma",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Now = zamanEkle(10 * time.Minute)
				in.PriorDebt = 1
				in.Payments = []Payment{
					odemeYap(StatusSucceeded, tariff.Kurus(math.MaxInt64/2+1), "p1", testT0, zamanEkle(5*time.Minute)),
					odemeYap(StatusSucceeded, tariff.Kurus(math.MaxInt64/2+1), "p2", testT0, zamanEkle(6*time.Minute)),
				}
				return in
			},
			hata: tariff.ErrOverflow,
		},
		{
			ad: "negatif teklif",
			girdi: func() GateInput {
				in := temelGirdi()
				in.Quote = sabitFiyat(-1)
				return in
			},
			hata: tariff.ErrNegativeAmount,
		},
	}

	for _, tt := range testler {
		if _, err := DecideGate(tt.girdi()); !errors.Is(err, tt.hata) {
			t.Errorf("%s: hata %v, bekle %v", tt.ad, err, tt.hata)
		}
	}
}

func TestDecideGateQuoteHatasi(t *testing.T) {
	errQuote := errors.New("teklif hatasi")

	t.Run("simdiki zaman teklifi", func(t *testing.T) {
		in := temelGirdi()
		in.Quote = func(time.Time) (tariff.Kurus, error) { return 0, errQuote }
		if _, err := DecideGate(in); !errors.Is(err, errQuote) {
			t.Fatalf("hata %v, bekle sarmalanmis %v", err, errQuote)
		}
	})

	t.Run("onceki borc teklifi", func(t *testing.T) {
		in := temelGirdi()
		in.PriorDebt = 1
		in.Quote = func(time.Time) (tariff.Kurus, error) { return 0, errQuote }
		if _, err := DecideGate(in); !errors.Is(err, errQuote) {
			t.Fatalf("hata %v, bekle sarmalanmis %v", err, errQuote)
		}
	})

	t.Run("odeme ani teklifi", func(t *testing.T) {
		odemeAni := zamanEkle(5 * time.Minute)
		in := temelGirdi()
		in.Now = zamanEkle(10 * time.Minute)
		in.Payments = []Payment{odemeYap(StatusSucceeded, 500, "p1", testT0, odemeAni)}
		in.Quote = func(at time.Time) (tariff.Kurus, error) {
			if at.Equal(odemeAni) {
				return 0, errQuote
			}
			return 1000, nil
		}
		if _, err := DecideGate(in); !errors.Is(err, errQuote) {
			t.Fatalf("hata %v, bekle sarmalanmis %v", err, errQuote)
		}
	})

	t.Run("odeme aninda negatif toplam", func(t *testing.T) {
		odemeAni := zamanEkle(5 * time.Minute)
		in := temelGirdi()
		in.Now = zamanEkle(10 * time.Minute)
		in.Payments = []Payment{odemeYap(StatusSucceeded, 500, "p1", testT0, odemeAni)}
		in.Quote = func(at time.Time) (tariff.Kurus, error) {
			if at.Equal(odemeAni) {
				return -1, nil
			}
			return 1000, nil
		}
		if _, err := DecideGate(in); !errors.Is(err, tariff.ErrNegativeAmount) {
			t.Fatalf("hata %v, bekle %v", err, tariff.ErrNegativeAmount)
		}
	})
}
