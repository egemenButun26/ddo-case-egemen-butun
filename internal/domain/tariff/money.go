package tariff

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// Kurus: kurus cinsinden para. Float hicbir yerde kullanilmaz.
type Kurus int64

var (
	// ErrOverflow: tutar int64 sinirini asiyor.
	ErrOverflow = errors.New("tutar tasmasi")
	// ErrNegativeAmount: tutar ya da birim negatif.
	ErrNegativeAmount = errors.New("negatif tutar ya da birim")
)

// String: "1.234,50 TL" bicimi (binlik nokta, ondalik virgul).
func (k Kurus) String() string {
	negatif := k < 0
	var u uint64
	if negatif {
		u = uint64(-(k + 1)) + 1 // en kucuk int64 degerinde de tasmaz
	} else {
		u = uint64(k)
	}

	tam := strconv.FormatUint(u/100, 10)
	var b strings.Builder
	if negatif {
		b.WriteByte('-')
	}
	for i := 0; i < len(tam); i++ {
		if i > 0 && (len(tam)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteByte(tam[i])
	}
	kurus := u % 100
	b.WriteByte(',')
	b.WriteByte(byte('0' + kurus/10))
	b.WriteByte(byte('0' + kurus%10))
	b.WriteString(" TL")
	return b.String()
}

// mulKurus: birim sayisi ile birim fiyatini tasma denetimiyle carpar.
func mulKurus(units int64, price Kurus) (Kurus, error) {
	if units < 0 || price < 0 {
		return 0, ErrNegativeAmount
	}
	if units == 0 || price == 0 {
		return 0, nil
	}
	p := int64(price)
	if p > math.MaxInt64/units {
		return 0, ErrOverflow
	}
	return Kurus(units * p), nil
}

// addKurus: iki tutari tasma denetimiyle toplar.
func addKurus(a, b Kurus) (Kurus, error) {
	if b > 0 && int64(a) > math.MaxInt64-int64(b) {
		return 0, ErrOverflow
	}
	if b < 0 && int64(a) < math.MinInt64-int64(b) {
		return 0, ErrOverflow
	}
	return a + b, nil
}
