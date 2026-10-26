package billing

import (
	"errors"
	"math"
	"time"

	"parkakil/internal/domain/tariff"
)

var (
	ErrInvalidPeriod  = errors.New("gecersiz donem")
	ErrInvalidRange   = errors.New("gecersiz tarih araligi")
	ErrNegativeAmount = errors.New("negatif tutar")
	ErrOverflow       = errors.New("tutar tasmasi")
)

type Plan struct {
	Code         string
	MonthlyPrice tariff.Kurus
	LotID        int64
}

func Prorate(price tariff.Kurus, periodStart, periodEnd, from, to time.Time) (tariff.Kurus, error) {
	if price < 0 {
		return 0, ErrNegativeAmount
	}
	if to.Before(from) {
		return 0, ErrInvalidRange
	}

	loc := periodStart.Location()
	ps := localDay(periodStart, loc)
	pe := localDay(periodEnd, loc)
	if !pe.After(ps) {
		return 0, ErrInvalidPeriod
	}

	f := localDay(from, loc)
	t := localDay(to, loc)
	if f.Before(ps) {
		f = ps
	}
	if f.After(pe) {
		f = pe
	}
	if t.Before(ps) {
		t = ps
	}
	if t.After(pe) {
		t = pe
	}

	totalDays := dayIndex(pe) - dayIndex(ps)
	usedDays := int64(0)
	if t.After(f) {
		usedDays = dayIndex(t) - dayIndex(f)
	}
	if usedDays <= 0 || price == 0 {
		return 0, nil
	}
	if uint64(price) > uint64(math.MaxInt64)/uint64(usedDays) {
		return 0, ErrOverflow
	}
	return tariff.Kurus(int64(price) * usedDays / totalDays), nil
}

func CancelCredit(price tariff.Kurus, periodStart, periodEnd, at time.Time) (tariff.Kurus, error) {
	used, err := Prorate(price, periodStart, periodEnd, periodStart, at)
	if err != nil {
		return 0, err
	}
	return price - used, nil
}

type PlanChange struct {
	Charge tariff.Kurus
	Credit tariff.Kurus
	Net    tariff.Kurus
}

func (c PlanChange) Direction() string {
	switch {
	case c.Net > 0:
		return "upgrade"
	case c.Net < 0:
		return "downgrade"
	default:
		return "even"
	}
}

func ChangePlans(oldPrice, newPrice tariff.Kurus, periodStart, periodEnd, at time.Time) (PlanChange, error) {
	credit, err := Prorate(oldPrice, periodStart, periodEnd, at, periodEnd)
	if err != nil {
		return PlanChange{}, err
	}
	charge, err := Prorate(newPrice, periodStart, periodEnd, at, periodEnd)
	if err != nil {
		return PlanChange{}, err
	}
	return PlanChange{Charge: charge, Credit: credit, Net: charge - credit}, nil
}

func localDay(t time.Time, loc *time.Location) time.Time {
	l := t.In(loc)
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC)
}

func dayIndex(t time.Time) int64 {
	return int64(t.Sub(time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)) / (24 * time.Hour))
}
