package payment

import (
	"fmt"
	"math"

	"parkakil/internal/domain/tariff"
)

// Settlement: odemelerin oturum toplamina gore dokumu.
type Settlement struct {
	Total    tariff.Kurus // oturumun o anki toplam ucreti
	Paid     tariff.Kurus // yalniz succeeded odemelerin toplami
	Due      tariff.Kurus // max(Total-Paid, 0)
	Overpaid tariff.Kurus // max(Paid-Total, 0); > 0 ise iade gerekir
}

// RefundRequired: fazla odeme varsa iade gerekir.
func (s Settlement) RefundRequired() bool {
	return s.Overpaid > 0
}

// Settle: yalniz succeeded odemeleri sayarak kalan ve fazla tutari hesaplar.
func Settle(total tariff.Kurus, payments []Payment) (Settlement, error) {
	if total < 0 {
		return Settlement{}, fmt.Errorf("%w: oturum toplami negatif (%d)", tariff.ErrNegativeAmount, total)
	}

	var paid tariff.Kurus
	for i, p := range payments {
		if err := p.Validate(); err != nil {
			return Settlement{}, fmt.Errorf("odeme %d: %w", i, err)
		}
		if p.Status != StatusSucceeded {
			continue
		}
		toplam, err := addKurus(paid, p.Amount)
		if err != nil {
			return Settlement{}, err
		}
		paid = toplam
	}

	s := Settlement{Total: total, Paid: paid}
	if paid > total {
		s.Overpaid = paid - total
	} else {
		s.Due = total - paid
	}
	return s, nil
}

// Due: kalan tutar; Settle(...).Due kisayolu.
func Due(total tariff.Kurus, payments []Payment) (tariff.Kurus, error) {
	s, err := Settle(total, payments)
	if err != nil {
		return 0, err
	}
	return s.Due, nil
}

// addKurus: iki tutari tasma denetimiyle toplar.
func addKurus(a, b tariff.Kurus) (tariff.Kurus, error) {
	if b > 0 && a > tariff.Kurus(math.MaxInt64-int64(b)) {
		return 0, tariff.ErrOverflow
	}
	if b < 0 && a < tariff.Kurus(math.MinInt64-int64(b)) {
		return 0, tariff.ErrOverflow
	}
	return a + b, nil
}
