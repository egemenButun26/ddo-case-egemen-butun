// Package occupancy kapasite, abone rezervasyonu ve doluluk sayaci
// kurallarini veritabanindan bagimsiz olarak ifade eder.
package occupancy

import "time"

// ReservationWindow bir rezervasyonun gecerli oldugu suredir.
const ReservationWindow = 30 * time.Minute

// Status rezervasyonun yasam dongusundeki durumudur.
type Status string

const (
	StatusActive    Status = "active"
	StatusArrived   Status = "arrived"
	StatusReleased  Status = "released"
	StatusCancelled Status = "cancelled"
)

// Reservation tek bir plaka rezervasyonunu temsil eder.
type Reservation struct {
	ID         int64
	LotID      int64
	Plate      string
	Status     Status
	ReservedAt time.Time
	ExpiresAt  time.Time
	ArrivedAt  *time.Time
	ReleasedAt *time.Time
	SessionID  int64
}

// ExpiresAt rezervasyonun bitis anini doner.
func ExpiresAt(reservedAt time.Time) time.Time {
	return reservedAt.Add(ReservationWindow)
}

// Expired, rezervasyonun now aninda serbest olup olmadigini soyler.
// Sinir dahildir: now >= expires_at ise rezervasyon dolmustur.
func Expired(r Reservation, now time.Time) bool {
	return !now.Before(r.ExpiresAt)
}

// Available, yeni rezervasyonsuz girisler icin kalan yeri doner.
// Sonuc hicbir zaman negatif olmaz.
func Available(capacity, openSessions, activeReservationsForOthers int) int {
	n := capacity - openSessions - activeReservationsForOthers
	if n < 0 {
		return 0
	}
	return n
}

// CanReserve, kapasitede yeni rezervasyon icin yer olup olmadigini soyler.
func CanReserve(capacity, openSessions, activeReservations int) bool {
	return Available(capacity, openSessions, activeReservations) > 0
}

// Drift, saklanan sayac ile olaylardan hesaplanan degeri karsilastirir.
// delta = stored - recomputed; drifted delta sifirdan farkliysa dogrudur.
func Drift(stored, recomputed int) (delta int, drifted bool) {
	delta = stored - recomputed
	return delta, delta != 0
}
