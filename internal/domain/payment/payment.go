// Package payment: odeme ve saglayici bildirimi durum gecisleri ile kapi karari.
// Saf domain paketidir; veritabani, HTTP ve saat erisimi yoktur, zaman hep parametredir.
// Abonelik bu adimda yoktur.
package payment

import (
	"errors"
	"fmt"
	"time"

	"parkakil/internal/domain/tariff"
)

// Status: odemenin saglayicidaki durumu.
type Status string

const (
	StatusPending   Status = "pending"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusTimedOut  Status = "timed_out"
)

// Payment: saglayici islemine karsilik gelen odeme.
type Payment struct {
	ProviderRef string       // saglayici islem kimligi, bos olamaz
	Amount      tariff.Kurus // > 0
	Status      Status
	RequestedAt time.Time // sifir olamaz
	// PaidAt: yalniz succeeded iken dolu; saglayicinin onay anidir.
	// PaidAt saglayicinin onay anidir; cikis izni PaidAt'ten baslar (gec onay dahil).
	// ConfirmedAt yalniz kayit/denetim icindir.
	PaidAt time.Time
	// ConfirmedAt: yalniz succeeded iken dolu; basari bildiriminin sisteme alindigi an.
	// PaidAt saglayicinin onay anidir; cikis izni PaidAt'ten baslar (gec onay dahil).
	// ConfirmedAt yalniz kayit/denetim icindir.
	ConfirmedAt time.Time
}

// Notification: saglayicidan gelen sonuc bildirimi.
type Notification struct {
	ProviderRef string
	Outcome     Status // yalniz succeeded ya da failed
	Amount      tariff.Kurus
	// ProviderAt: saglayicinin sonucu verdigi an (basarida onay, basarisizlikta red ani).
	ProviderAt time.Time
	// ReceivedAt: bildirimin sisteme ulastigi an.
	ReceivedAt time.Time
}

// Transition: uygulanan bildirimin yan etkisi.
type Transition struct {
	Duplicate    bool // ayni bildirim tekrar geldi, durum degismedi
	LateApproval bool // timed_out -> succeeded
}

var (
	// ErrInvalidAmount: tutar sifir.
	ErrInvalidAmount = errors.New("gecersiz tutar")
	// ErrInvalidStatus: bilinmeyen odeme durumu.
	ErrInvalidStatus = errors.New("gecersiz odeme durumu")
	// ErrInvalidPayment: odemenin diger alanlari gecersiz.
	ErrInvalidPayment = errors.New("gecersiz odeme")
	// ErrInvalidNotification: bildirim alanlari ya da sonucu gecersiz.
	ErrInvalidNotification = errors.New("gecersiz bildirim")
	// ErrNotificationConflict: bildirim mevcut durumla celisiyor.
	ErrNotificationConflict = errors.New("bildirim durumla celisiyor")
	// ErrProviderRefMismatch: bildirim baska bir odemeye ait.
	ErrProviderRefMismatch = errors.New("saglayici referansi uyusmuyor")
	// ErrAmountMismatch: basarili bildirimin tutari odemeyle uyusmuyor.
	ErrAmountMismatch = errors.New("bildirim tutari odemeyle uyusmuyor")
	// ErrInvalidTimeout: zaman asimi suresi pozitif degil.
	ErrInvalidTimeout = errors.New("gecersiz zaman asimi")
	// ErrInvalidGateInput: kapi karari girdisi tutarsiz.
	ErrInvalidGateInput = errors.New("gecersiz kapi girdisi")
)

// Validate: odemenin butun alanlarini denetler.
func (p Payment) Validate() error {
	if p.ProviderRef == "" {
		return fmt.Errorf("%w: saglayici referansi bos", ErrInvalidPayment)
	}
	if p.Amount < 0 {
		return fmt.Errorf("%w: odeme tutari negatif (%d)", tariff.ErrNegativeAmount, p.Amount)
	}
	if p.Amount == 0 {
		return fmt.Errorf("%w: odeme tutari sifir", ErrInvalidAmount)
	}
	switch p.Status {
	case StatusPending, StatusSucceeded, StatusFailed, StatusTimedOut:
	default:
		return fmt.Errorf("%w: %q", ErrInvalidStatus, p.Status)
	}
	if p.RequestedAt.IsZero() {
		return fmt.Errorf("%w: istek zamani bos", ErrInvalidPayment)
	}
	if p.Status == StatusSucceeded {
		if p.PaidAt.IsZero() {
			return fmt.Errorf("%w: basarili odemede odeme zamani bos", ErrInvalidPayment)
		}
		if p.ConfirmedAt.IsZero() {
			return fmt.Errorf("%w: basarili odemede onay zamani bos", ErrInvalidPayment)
		}
		if p.PaidAt.Before(p.RequestedAt) {
			return fmt.Errorf("%w: odeme zamani istekten once", ErrInvalidPayment)
		}
		if p.ConfirmedAt.Before(p.PaidAt) {
			return fmt.Errorf("%w: onay zamani odeme zamanindan once", ErrInvalidPayment)
		}
		return nil
	}
	if !p.PaidAt.IsZero() {
		return fmt.Errorf("%w: basarili olmayan odemede odeme zamani dolu", ErrInvalidPayment)
	}
	if !p.ConfirmedAt.IsZero() {
		return fmt.Errorf("%w: basarili olmayan odemede onay zamani dolu", ErrInvalidPayment)
	}
	return nil
}

// Apply: bildirimi odemeye uygular. Girdiyi degistirmez, yeni deger dondurur.
// Tutar uyusmazligi durum kontrolunden once denetlenir.
func Apply(p Payment, n Notification) (Payment, Transition, error) {
	var tr Transition

	if err := p.Validate(); err != nil {
		return Payment{}, tr, err
	}
	if n.ProviderRef == "" || n.ProviderRef != p.ProviderRef {
		return Payment{}, tr, fmt.Errorf("%w: odeme %q, bildirim %q", ErrProviderRefMismatch, p.ProviderRef, n.ProviderRef)
	}
	if n.Outcome != StatusSucceeded && n.Outcome != StatusFailed {
		return Payment{}, tr, fmt.Errorf("%w: sonuc %q", ErrInvalidNotification, n.Outcome)
	}
	if n.ProviderAt.IsZero() || n.ReceivedAt.IsZero() {
		return Payment{}, tr, fmt.Errorf("%w: bildirim zamani bos", ErrInvalidNotification)
	}
	if n.ProviderAt.Before(p.RequestedAt) {
		return Payment{}, tr, fmt.Errorf("%w: saglayici zamani istekten once", ErrInvalidNotification)
	}
	if n.ReceivedAt.Before(n.ProviderAt) {
		return Payment{}, tr, fmt.Errorf("%w: bildirim saglayici zamanindan once alindi", ErrInvalidNotification)
	}
	if n.Outcome == StatusSucceeded && n.Amount != p.Amount {
		return Payment{}, tr, fmt.Errorf("%w: odeme %v, bildirim %v", ErrAmountMismatch, p.Amount, n.Amount)
	}

	switch p.Status {
	case StatusPending:
		switch n.Outcome {
		case StatusSucceeded:
			p.Status = StatusSucceeded
			p.PaidAt = n.ProviderAt
			p.ConfirmedAt = n.ReceivedAt
		case StatusFailed:
			p.Status = StatusFailed
		}
	case StatusTimedOut:
		switch n.Outcome {
		case StatusSucceeded:
			p.Status = StatusSucceeded
			p.PaidAt = n.ProviderAt
			p.ConfirmedAt = n.ReceivedAt
			tr.LateApproval = true
		case StatusFailed:
			p.Status = StatusFailed
		}
	case StatusSucceeded:
		switch n.Outcome {
		case StatusSucceeded:
			if !n.ProviderAt.Equal(p.PaidAt) {
				return Payment{}, tr, fmt.Errorf("%w: saglayici ayni islem icin farkli onay ani bildirdi", ErrNotificationConflict)
			}
			tr.Duplicate = true
		case StatusFailed:
			return Payment{}, tr, fmt.Errorf("%w: basarili odeme basarisiz bildirim aldi", ErrNotificationConflict)
		}
	case StatusFailed:
		switch n.Outcome {
		case StatusFailed:
			tr.Duplicate = true
		case StatusSucceeded:
			return Payment{}, tr, fmt.Errorf("%w: basarisiz odeme basarili bildirim aldi", ErrNotificationConflict)
		}
	}
	return p, tr, nil
}

// Expire: pending odemeyi, zaman asimi dolduysa timed_out yapar. Sinir dahildir.
func Expire(p Payment, now time.Time, timeout time.Duration) (Payment, bool, error) {
	if err := p.Validate(); err != nil {
		return Payment{}, false, err
	}
	if timeout <= 0 {
		return Payment{}, false, fmt.Errorf("%w: sure %s", ErrInvalidTimeout, timeout)
	}
	if p.Status != StatusPending {
		return p, false, nil
	}
	if now.Sub(p.RequestedAt) >= timeout {
		p.Status = StatusTimedOut
		return p, true, nil
	}
	return p, false, nil
}
