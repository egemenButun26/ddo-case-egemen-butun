package payment

import (
	"errors"
	"math"
	"testing"
	"time"

	"parkakil/internal/domain/tariff"
)

var testT0 = time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

func zamanEkle(d time.Duration) time.Time {
	return testT0.Add(d)
}

func odemeYap(status Status, amount tariff.Kurus, ref string, requested, paidAt time.Time) Payment {
	p := Payment{
		ProviderRef: ref,
		Amount:      amount,
		Status:      status,
		RequestedAt: requested,
		PaidAt:      paidAt,
	}
	if status == StatusSucceeded {
		p.ConfirmedAt = paidAt
	}
	return p
}

func TestValidate(t *testing.T) {
	pending := odemeYap(StatusPending, 1000, "p1", testT0, time.Time{})
	basarili := odemeYap(StatusSucceeded, 1000, "p1", testT0, zamanEkle(time.Minute))
	basarisiz := odemeYap(StatusFailed, 1000, "p1", testT0, time.Time{})
	zamanAsimi := odemeYap(StatusTimedOut, 1000, "p1", testT0, time.Time{})

	sifirOnay := odemeYap(StatusSucceeded, 1000, "p1", testT0, zamanEkle(time.Minute))
	sifirOnay.ConfirmedAt = time.Time{}

	onayOdemedeOnce := odemeYap(StatusSucceeded, 1000, "p1", testT0, zamanEkle(time.Minute))
	onayOdemedeOnce.ConfirmedAt = zamanEkle(30 * time.Second)

	odemeIstektenOnce := odemeYap(StatusSucceeded, 1000, "p1", testT0, zamanEkle(-time.Minute))
	odemeIstektenOnce.ConfirmedAt = zamanEkle(time.Minute)

	basarisizOnayli := odemeYap(StatusFailed, 1000, "p1", testT0, time.Time{})
	basarisizOnayli.ConfirmedAt = zamanEkle(time.Minute)

	testler := []struct {
		ad   string
		p    Payment
		hata error
	}{
		{"bekleyen gecerli", pending, nil},
		{"basarili gecerli", basarili, nil},
		{"basarisiz gecerli", basarisiz, nil},
		{"zaman asimli gecerli", zamanAsimi, nil},
		{"bos referans", odemeYap(StatusPending, 1000, "", testT0, time.Time{}), ErrInvalidPayment},
		{"negatif tutar", odemeYap(StatusPending, -1, "p1", testT0, time.Time{}), tariff.ErrNegativeAmount},
		{"sifir tutar", odemeYap(StatusPending, 0, "p1", testT0, time.Time{}), ErrInvalidAmount},
		{"bilinmeyen durum", odemeYap(Status("weird"), 1000, "p1", testT0, time.Time{}), ErrInvalidStatus},
		{"sifir istek zamani", odemeYap(StatusPending, 1000, "p1", time.Time{}, time.Time{}), ErrInvalidPayment},
		{"basarilida bos odeme zamani", odemeYap(StatusSucceeded, 1000, "p1", testT0, time.Time{}), ErrInvalidPayment},
		{"basarilida bos onay zamani", sifirOnay, ErrInvalidPayment},
		{"onay zamani odeme zamanindan once", onayOdemedeOnce, ErrInvalidPayment},
		{"odeme zamani istekten once", odemeIstektenOnce, ErrInvalidPayment},
		{"bekleyende dolu odeme zamani", odemeYap(StatusPending, 1000, "p1", testT0, zamanEkle(time.Minute)), ErrInvalidPayment},
		{"basarisizda dolu odeme zamani", odemeYap(StatusFailed, 1000, "p1", testT0, zamanEkle(time.Minute)), ErrInvalidPayment},
		{"zaman asiminda dolu odeme zamani", odemeYap(StatusTimedOut, 1000, "p1", testT0, zamanEkle(time.Minute)), ErrInvalidPayment},
		{"basarisizda dolu onay zamani", basarisizOnayli, ErrInvalidPayment},
	}

	for _, tt := range testler {
		err := tt.p.Validate()
		if tt.hata == nil {
			if err != nil {
				t.Errorf("%s: beklenmeyen hata: %v", tt.ad, err)
			}
			continue
		}
		if !errors.Is(err, tt.hata) {
			t.Errorf("%s: hata %v, bekle %v", tt.ad, err, tt.hata)
		}
	}
}

func TestApply(t *testing.T) {
	odemeZamani := zamanEkle(time.Minute)
	alinmaZamani := zamanEkle(2 * time.Minute)

	testler := []struct {
		ad             string
		p              Payment
		n              Notification
		bekleStatus    Status
		beklePaidAt    time.Time
		bekleConfirmed time.Time
		duplicate      bool
		lateApproval   bool
		hata           error
	}{
		{
			ad:             "bekleyen basarili bildirim",
			p:              odemeYap(StatusPending, 1000, "p1", testT0, time.Time{}),
			n:              Notification{ProviderRef: "p1", Outcome: StatusSucceeded, Amount: 1000, ProviderAt: odemeZamani, ReceivedAt: alinmaZamani},
			bekleStatus:    StatusSucceeded,
			beklePaidAt:    odemeZamani,
			bekleConfirmed: alinmaZamani,
		},
		{
			ad:          "bekleyen basarisiz bildirim",
			p:           odemeYap(StatusPending, 1000, "p1", testT0, time.Time{}),
			n:           Notification{ProviderRef: "p1", Outcome: StatusFailed, Amount: 1000, ProviderAt: odemeZamani, ReceivedAt: alinmaZamani},
			bekleStatus: StatusFailed,
		},
		{
			ad:             "zaman asimli gec onay",
			p:              odemeYap(StatusTimedOut, 1000, "p1", testT0, time.Time{}),
			n:              Notification{ProviderRef: "p1", Outcome: StatusSucceeded, Amount: 1000, ProviderAt: odemeZamani, ReceivedAt: alinmaZamani},
			bekleStatus:    StatusSucceeded,
			beklePaidAt:    odemeZamani,
			bekleConfirmed: alinmaZamani,
			lateApproval:   true,
		},
		{
			ad:             "gec onay onay ve alinma ayri",
			p:              odemeYap(StatusTimedOut, 1000, "p1", testT0, time.Time{}),
			n:              Notification{ProviderRef: "p1", Outcome: StatusSucceeded, Amount: 1000, ProviderAt: zamanEkle(time.Minute), ReceivedAt: zamanEkle(20 * time.Minute)},
			bekleStatus:    StatusSucceeded,
			beklePaidAt:    zamanEkle(time.Minute),
			bekleConfirmed: zamanEkle(20 * time.Minute),
			lateApproval:   true,
		},
		{
			ad:          "zaman asimli basarisiz bildirim",
			p:           odemeYap(StatusTimedOut, 1000, "p1", testT0, time.Time{}),
			n:           Notification{ProviderRef: "p1", Outcome: StatusFailed, Amount: 1000, ProviderAt: odemeZamani, ReceivedAt: alinmaZamani},
			bekleStatus: StatusFailed,
		},
		{
			ad:             "basarili yinelenen bildirim farkli alinma",
			p:              odemeYap(StatusSucceeded, 1000, "p1", testT0, odemeZamani),
			n:              Notification{ProviderRef: "p1", Outcome: StatusSucceeded, Amount: 1000, ProviderAt: odemeZamani, ReceivedAt: alinmaZamani},
			bekleStatus:    StatusSucceeded,
			beklePaidAt:    odemeZamani,
			bekleConfirmed: odemeZamani,
			duplicate:      true,
		},
		{
			ad:          "basarisiz yinelenen bildirim",
			p:           odemeYap(StatusFailed, 1000, "p1", testT0, time.Time{}),
			n:           Notification{ProviderRef: "p1", Outcome: StatusFailed, Amount: 1000, ProviderAt: odemeZamani, ReceivedAt: alinmaZamani},
			bekleStatus: StatusFailed,
			duplicate:   true,
		},
		{
			ad:   "basarili yinelenen bildirim farkli onay",
			p:    odemeYap(StatusSucceeded, 1000, "p1", testT0, odemeZamani),
			n:    Notification{ProviderRef: "p1", Outcome: StatusSucceeded, Amount: 1000, ProviderAt: odemeZamani.Add(time.Minute), ReceivedAt: alinmaZamani},
			hata: ErrNotificationConflict,
		},
		{
			ad:   "basariliya basarisiz bildirim celiski",
			p:    odemeYap(StatusSucceeded, 1000, "p1", testT0, odemeZamani),
			n:    Notification{ProviderRef: "p1", Outcome: StatusFailed, Amount: 1000, ProviderAt: odemeZamani.Add(time.Minute), ReceivedAt: alinmaZamani},
			hata: ErrNotificationConflict,
		},
		{
			ad:   "basarisiza basarili bildirim celiski",
			p:    odemeYap(StatusFailed, 1000, "p1", testT0, time.Time{}),
			n:    Notification{ProviderRef: "p1", Outcome: StatusSucceeded, Amount: 1000, ProviderAt: odemeZamani, ReceivedAt: alinmaZamani},
			hata: ErrNotificationConflict,
		},
		{
			ad:   "farkli referans",
			p:    odemeYap(StatusPending, 1000, "p1", testT0, time.Time{}),
			n:    Notification{ProviderRef: "p2", Outcome: StatusSucceeded, Amount: 1000, ProviderAt: odemeZamani, ReceivedAt: alinmaZamani},
			hata: ErrProviderRefMismatch,
		},
		{
			ad:   "bos bildirim referansi",
			p:    odemeYap(StatusPending, 1000, "p1", testT0, time.Time{}),
			n:    Notification{ProviderRef: "", Outcome: StatusSucceeded, Amount: 1000, ProviderAt: odemeZamani, ReceivedAt: alinmaZamani},
			hata: ErrProviderRefMismatch,
		},
		{
			ad:   "basarili bildirimde tutar uyusmazligi",
			p:    odemeYap(StatusPending, 1000, "p1", testT0, time.Time{}),
			n:    Notification{ProviderRef: "p1", Outcome: StatusSucceeded, Amount: 999, ProviderAt: odemeZamani, ReceivedAt: alinmaZamani},
			hata: ErrAmountMismatch,
		},
		{
			ad:   "yinelenen basarili bildirimde tutar uyusmazligi",
			p:    odemeYap(StatusSucceeded, 1000, "p1", testT0, odemeZamani),
			n:    Notification{ProviderRef: "p1", Outcome: StatusSucceeded, Amount: 999, ProviderAt: odemeZamani, ReceivedAt: alinmaZamani},
			hata: ErrAmountMismatch,
		},
		{
			ad:   "basarisiz odemeye farkli tutarli basarili bildirim",
			p:    odemeYap(StatusFailed, 1000, "p1", testT0, time.Time{}),
			n:    Notification{ProviderRef: "p1", Outcome: StatusSucceeded, Amount: 999, ProviderAt: odemeZamani, ReceivedAt: alinmaZamani},
			hata: ErrAmountMismatch,
		},
		{
			ad:   "bekleyen sonuc bildirimi gecersiz",
			p:    odemeYap(StatusPending, 1000, "p1", testT0, time.Time{}),
			n:    Notification{ProviderRef: "p1", Outcome: StatusPending, Amount: 1000, ProviderAt: odemeZamani, ReceivedAt: alinmaZamani},
			hata: ErrInvalidNotification,
		},
		{
			ad:   "zaman asimli sonuc bildirimi gecersiz",
			p:    odemeYap(StatusPending, 1000, "p1", testT0, time.Time{}),
			n:    Notification{ProviderRef: "p1", Outcome: StatusTimedOut, Amount: 1000, ProviderAt: odemeZamani, ReceivedAt: alinmaZamani},
			hata: ErrInvalidNotification,
		},
		{
			ad:   "saglayici zamani bos",
			p:    odemeYap(StatusPending, 1000, "p1", testT0, time.Time{}),
			n:    Notification{ProviderRef: "p1", Outcome: StatusSucceeded, Amount: 1000, ProviderAt: time.Time{}, ReceivedAt: alinmaZamani},
			hata: ErrInvalidNotification,
		},
		{
			ad:   "alinma zamani bos",
			p:    odemeYap(StatusPending, 1000, "p1", testT0, time.Time{}),
			n:    Notification{ProviderRef: "p1", Outcome: StatusSucceeded, Amount: 1000, ProviderAt: odemeZamani, ReceivedAt: time.Time{}},
			hata: ErrInvalidNotification,
		},
		{
			ad:   "saglayici zamani istekten once",
			p:    odemeYap(StatusPending, 1000, "p1", testT0, time.Time{}),
			n:    Notification{ProviderRef: "p1", Outcome: StatusSucceeded, Amount: 1000, ProviderAt: testT0.Add(-time.Second), ReceivedAt: alinmaZamani},
			hata: ErrInvalidNotification,
		},
		{
			ad:   "alinma zamani saglayici zamanindan once",
			p:    odemeYap(StatusPending, 1000, "p1", testT0, time.Time{}),
			n:    Notification{ProviderRef: "p1", Outcome: StatusSucceeded, Amount: 1000, ProviderAt: alinmaZamani, ReceivedAt: odemeZamani},
			hata: ErrInvalidNotification,
		},
		{
			ad:   "gecersiz odeme",
			p:    odemeYap(StatusPending, 0, "p1", testT0, time.Time{}),
			n:    Notification{ProviderRef: "p1", Outcome: StatusSucceeded, Amount: 0, ProviderAt: odemeZamani, ReceivedAt: alinmaZamani},
			hata: ErrInvalidAmount,
		},
	}

	for _, tt := range testler {
		yeni, tr, err := Apply(tt.p, tt.n)
		if tt.hata != nil {
			if !errors.Is(err, tt.hata) {
				t.Errorf("%s: hata %v, bekle %v", tt.ad, err, tt.hata)
			}
			if errors.Is(tt.hata, ErrAmountMismatch) && errors.Is(err, ErrNotificationConflict) {
				t.Errorf("%s: tutar uyusmazligi bildirim celiskisi olarak dondu: %v", tt.ad, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: beklenmeyen hata: %v", tt.ad, err)
			continue
		}
		if yeni.Status != tt.bekleStatus {
			t.Errorf("%s: durum %q, bekle %q", tt.ad, yeni.Status, tt.bekleStatus)
		}
		if !yeni.PaidAt.Equal(tt.beklePaidAt) {
			t.Errorf("%s: odeme zamani %v, bekle %v", tt.ad, yeni.PaidAt, tt.beklePaidAt)
		}
		if !yeni.ConfirmedAt.Equal(tt.bekleConfirmed) {
			t.Errorf("%s: onay zamani %v, bekle %v", tt.ad, yeni.ConfirmedAt, tt.bekleConfirmed)
		}
		if tr.Duplicate != tt.duplicate {
			t.Errorf("%s: Duplicate %v, bekle %v", tt.ad, tr.Duplicate, tt.duplicate)
		}
		if tr.LateApproval != tt.lateApproval {
			t.Errorf("%s: LateApproval %v, bekle %v", tt.ad, tr.LateApproval, tt.lateApproval)
		}
		if tt.duplicate {
			if !yeni.PaidAt.Equal(tt.p.PaidAt) {
				t.Errorf("%s: yinelenen bildirim PaidAt degerini degistirdi", tt.ad)
			}
			if !yeni.ConfirmedAt.Equal(tt.p.ConfirmedAt) {
				t.Errorf("%s: yinelenen bildirim ConfirmedAt degerini degistirdi", tt.ad)
			}
		}
	}
}

func TestExpire(t *testing.T) {
	bekleyen := odemeYap(StatusPending, 1000, "p1", testT0, time.Time{})
	basarili := odemeYap(StatusSucceeded, 1000, "p1", testT0, zamanEkle(time.Minute))
	basarisiz := odemeYap(StatusFailed, 1000, "p1", testT0, time.Time{})
	zamanAsimi := odemeYap(StatusTimedOut, 1000, "p1", testT0, time.Time{})

	testler := []struct {
		ad          string
		p           Payment
		now         time.Time
		timeout     time.Duration
		bekleStatus Status
		degisti     bool
		hata        error
	}{
		{"sinirdan bir nanosaniye once", bekleyen, testT0.Add(5*time.Minute - time.Nanosecond), 5 * time.Minute, StatusPending, false, nil},
		{"tam sinir zaman asimi", bekleyen, testT0.Add(5 * time.Minute), 5 * time.Minute, StatusTimedOut, true, nil},
		{"sinirdan sonra", bekleyen, testT0.Add(6 * time.Minute), 5 * time.Minute, StatusTimedOut, true, nil},
		{"istekten once", bekleyen, testT0.Add(-time.Minute), 5 * time.Minute, StatusPending, false, nil},
		{"basarili degismez", basarili, testT0.Add(10 * time.Minute), 5 * time.Minute, StatusSucceeded, false, nil},
		{"basarisiz degismez", basarisiz, testT0.Add(10 * time.Minute), 5 * time.Minute, StatusFailed, false, nil},
		{"zaman asimli degismez", zamanAsimi, testT0.Add(10 * time.Minute), 5 * time.Minute, StatusTimedOut, false, nil},
		{"sifir sure", bekleyen, testT0.Add(time.Minute), 0, StatusPending, false, ErrInvalidTimeout},
		{"negatif sure", bekleyen, testT0.Add(time.Minute), -time.Minute, StatusPending, false, ErrInvalidTimeout},
		{"gecersiz odeme", odemeYap(StatusPending, 0, "p1", testT0, time.Time{}), testT0.Add(time.Minute), time.Minute, StatusPending, false, ErrInvalidAmount},
	}

	for _, tt := range testler {
		yeni, degisti, err := Expire(tt.p, tt.now, tt.timeout)
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
		if yeni.Status != tt.bekleStatus {
			t.Errorf("%s: durum %q, bekle %q", tt.ad, yeni.Status, tt.bekleStatus)
		}
		if degisti != tt.degisti {
			t.Errorf("%s: degisti %v, bekle %v", tt.ad, degisti, tt.degisti)
		}
	}
}

func TestAddKurusTasma(t *testing.T) {
	testler := []struct {
		ad    string
		a     tariff.Kurus
		b     tariff.Kurus
		bekle tariff.Kurus
		hata  error
	}{
		{"normal", 100, 200, 300, nil},
		{"negatif normal", 100, -50, 50, nil},
		{"ust sinir tasmasi", tariff.Kurus(math.MaxInt64), 1, 0, tariff.ErrOverflow},
		{"negatif ust sinir tasmasi", tariff.Kurus(math.MinInt64), -1, 0, tariff.ErrOverflow},
		{"ust sinira kadar", tariff.Kurus(math.MaxInt64 - 1), 1, tariff.Kurus(math.MaxInt64), nil},
	}

	for _, tt := range testler {
		sonuc, err := addKurus(tt.a, tt.b)
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
		if sonuc != tt.bekle {
			t.Errorf("%s: sonuc %v, bekle %v", tt.ad, sonuc, tt.bekle)
		}
	}
}
