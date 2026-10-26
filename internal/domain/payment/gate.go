package payment

import (
	"fmt"
	"time"

	"parkakil/internal/domain/plate"
	"parkakil/internal/domain/tariff"
)

// Action: kapi aktorunun yapmasi gereken eylem.
type Action string

const (
	ActionOpen Action = "open"
	ActionDeny Action = "deny"
)

// PermitWindow: basarili odemenin ardindan kapinin acik kaldigi sure.
const PermitWindow = 15 * time.Minute

// Kapi karari nedenleri.
const (
	ReasonNoMatch         = "no_match"
	ReasonLowConfidence   = "low_confidence"
	ReasonPriorDebt       = "prior_debt"
	ReasonNoCharge        = "no_charge"
	ReasonPaid            = "paid"
	ReasonPermitValid     = "permit_valid"
	ReasonPermitExpired   = "permit_expired"
	ReasonPaymentPending  = "payment_pending"
	ReasonPaymentRequired = "payment_required"
)

// GateInput: kapi karari icin gereken butun bilgi.
type GateInput struct {
	Match     plate.Decision // plaka eslestirme karari (cagiran plate.Match ile uretir)
	Found     bool           // eslesen acik oturum var mi
	EnteredAt time.Time
	Now       time.Time
	Quote     func(at time.Time) (tariff.Kurus, error) // oturumun entered_at..at toplam ucreti
	Payments  []Payment
	PriorDebt tariff.Kurus // aracin onceki oturumlardan odenmemis borcu
}

// GateDecision: kapi aktorune verilen karar.
type GateDecision struct {
	Action          Action
	Reason          string
	Due             tariff.Kurus // yalniz bu oturumun kalani
	PriorDebt       tariff.Kurus // aracin ONCEKI oturumlardan odenmemis borcu; Due'ya eklenmez
	PermitExpiresAt time.Time    // gecerli izinle acildiysa
}

// DecideGate: kapi kararini sirayla degerlendirir; ilk uyan kazanir.
// Sira: 1) plaka eslesmesi, 2) girdi dogrulama, 3) onceki borc, 4) ucretsiz oturum,
// 5) odeme tam, 6) gecerli izin, 7) bekleyen odeme, 8) izin doldu, 9) odeme gerekli.
func DecideGate(in GateInput) (GateDecision, error) {
	if in.Match == plate.DecisionNone || !in.Found {
		return GateDecision{Action: ActionDeny, Reason: ReasonNoMatch}, nil
	}
	switch in.Match {
	case plate.DecisionExact, plate.DecisionAuto:
	case plate.DecisionReview:
		return GateDecision{Action: ActionDeny, Reason: ReasonLowConfidence}, nil
	default:
		return GateDecision{}, fmt.Errorf("%w: bilinmeyen plaka karari %q", ErrInvalidGateInput, in.Match)
	}

	if in.Quote == nil {
		return GateDecision{}, fmt.Errorf("%w: fiyat fonksiyonu yok", ErrInvalidGateInput)
	}
	if in.EnteredAt.IsZero() || in.Now.IsZero() {
		return GateDecision{}, fmt.Errorf("%w: giris zamani ya da simdiki zaman bos", ErrInvalidGateInput)
	}
	if in.Now.Before(in.EnteredAt) {
		return GateDecision{}, fmt.Errorf("%w: simdiki zaman giris zamanindan once", ErrInvalidGateInput)
	}
	if in.PriorDebt < 0 {
		return GateDecision{}, fmt.Errorf("%w: onceki borc negatif (%d)", tariff.ErrNegativeAmount, in.PriorDebt)
	}
	for _, p := range in.Payments {
		if err := p.Validate(); err != nil {
			return GateDecision{}, err
		}
		if p.Status == StatusSucceeded && p.ConfirmedAt.After(in.Now) {
			// Validate PaidAt <= ConfirmedAt garantiler; bu yuzden bu kontrol
			// PaidAt > Now durumunu da kapsar ve ayri PaidAt kontrolu gereksizdir.
			return GateDecision{}, fmt.Errorf("%w: onay zamani gelecekte (%s > %s)", ErrInvalidGateInput, p.ConfirmedAt, in.Now)
		}
	}

	if in.PriorDebt > 0 {
		total, err := quoteAt(in.Quote, in.Now)
		if err != nil {
			return GateDecision{}, err
		}
		s, err := Settle(total, in.Payments)
		if err != nil {
			return GateDecision{}, err
		}
		return GateDecision{Action: ActionDeny, Reason: ReasonPriorDebt, Due: s.Due, PriorDebt: in.PriorDebt}, nil
	}

	totalNow, err := quoteAt(in.Quote, in.Now)
	if err != nil {
		return GateDecision{}, err
	}
	if totalNow == 0 {
		return GateDecision{Action: ActionOpen, Reason: ReasonNoCharge}, nil
	}

	s, err := Settle(totalNow, in.Payments)
	if err != nil {
		return GateDecision{}, err
	}
	if s.Due == 0 {
		return GateDecision{Action: ActionOpen, Reason: ReasonPaid}, nil
	}

	latest, varMi := latestSucceeded(in.Payments)
	// PaidAt <= Now dogrulamada garanti edilir.
	if varMi && in.Now.Before(latest.PaidAt.Add(PermitWindow)) {
		odemeAni, err := quoteAt(in.Quote, latest.PaidAt)
		if err != nil {
			return GateDecision{}, err
		}
		oAn, err := Settle(odemeAni, in.Payments)
		if err != nil {
			return GateDecision{}, err
		}
		if oAn.Due == 0 {
			return GateDecision{
				Action:          ActionOpen,
				Reason:          ReasonPermitValid,
				PermitExpiresAt: latest.PaidAt.Add(PermitWindow),
			}, nil
		}
	}

	for _, p := range in.Payments {
		if p.Status == StatusPending {
			return GateDecision{Action: ActionDeny, Reason: ReasonPaymentPending, Due: s.Due}, nil
		}
	}

	if varMi && !in.Now.Before(latest.PaidAt.Add(PermitWindow)) {
		return GateDecision{Action: ActionDeny, Reason: ReasonPermitExpired, Due: s.Due}, nil
	}

	return GateDecision{Action: ActionDeny, Reason: ReasonPaymentRequired, Due: s.Due}, nil
}

// quoteAt: tarife motorunu cagirir ve hatayi sarmalar.
func quoteAt(fn func(time.Time) (tariff.Kurus, error), at time.Time) (tariff.Kurus, error) {
	tutar, err := fn(at)
	if err != nil {
		return 0, fmt.Errorf("kapi karari: ucret hesaplanamadi: %w", err)
	}
	return tutar, nil
}

// latestSucceeded: en son odeme zamanina sahip basarili odemeyi bulur.
func latestSucceeded(payments []Payment) (Payment, bool) {
	var son Payment
	var bulundu bool
	for _, p := range payments {
		if p.Status != StatusSucceeded {
			continue
		}
		if !bulundu || p.PaidAt.After(son.PaidAt) {
			son = p
			bulundu = true
		}
	}
	return son, bulundu
}
