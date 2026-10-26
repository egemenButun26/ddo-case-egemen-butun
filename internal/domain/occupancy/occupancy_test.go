package occupancy

import (
	"testing"
	"time"
)

var temel = time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

func TestReservationWindow(t *testing.T) {
	if ReservationWindow != 30*time.Minute {
		t.Fatalf("pencere = %v, beklenen 30dk", ReservationWindow)
	}
	if got := ExpiresAt(temel); !got.Equal(temel.Add(30 * time.Minute)) {
		t.Fatalf("ExpiresAt = %v, beklenen %v", got, temel.Add(30*time.Minute))
	}
}

func TestExpiredSinir(t *testing.T) {
	r := Reservation{ReservedAt: temel, ExpiresAt: ExpiresAt(temel)}

	testler := []struct {
		ad      string
		now     time.Time
		expired bool
	}{
		{"hemen sonra aktif", temel, false},
		{"29:59.999 aktif", temel.Add(30*time.Minute - time.Millisecond), false},
		{"30:00 serbest", temel.Add(30 * time.Minute), true},
		{"30:00.001 serbest", temel.Add(30*time.Minute + time.Millisecond), true},
	}
	for _, test := range testler {
		t.Run(test.ad, func(t *testing.T) {
			if got := Expired(r, test.now); got != test.expired {
				t.Fatalf("Expired(%v) = %v, beklenen %v", test.now, got, test.expired)
			}
		})
	}
}

func TestAvailable(t *testing.T) {
	testler := []struct {
		ad       string
		capacity int
		open     int
		reserved int
		beklenen int
	}{
		{"bos otopark", 10, 0, 0, 10},
		{"acik ve rezerve dolu", 10, 4, 6, 0},
		{"asiri dolu negatif olmaz", 10, 8, 7, 0},
		{"kapasite sifir", 0, 0, 0, 0},
		{"negatif kapasite", -5, 0, 0, 0},
		{"kalan yer", 10, 3, 2, 5},
	}
	for _, test := range testler {
		t.Run(test.ad, func(t *testing.T) {
			if got := Available(test.capacity, test.open, test.reserved); got != test.beklenen {
				t.Fatalf("Available = %d, beklenen %d", got, test.beklenen)
			}
		})
	}
}

func TestCanReserve(t *testing.T) {
	if !CanReserve(10, 3, 6) {
		t.Fatalf("bir yer varken rezervasyon kabul edilmeli")
	}
	if CanReserve(10, 4, 6) {
		t.Fatalf("tam doluyken rezervasyon reddedilmeli")
	}
	if CanReserve(10, 8, 7) {
		t.Fatalf("asiri doluyken rezervasyon reddedilmeli")
	}
}

func TestDrift(t *testing.T) {
	testler := []struct {
		ad         string
		stored     int
		recomputed int
		delta      int
		drifted    bool
	}{
		{"esit", 3, 3, 0, false},
		{"fazla", 10, 3, 7, true},
		{"eksik", 2, 5, -3, true},
		{"sifir", 0, 0, 0, false},
	}
	for _, test := range testler {
		t.Run(test.ad, func(t *testing.T) {
			delta, drifted := Drift(test.stored, test.recomputed)
			if delta != test.delta || drifted != test.drifted {
				t.Fatalf("Drift(%d,%d) = %d,%v; beklenen %d,%v",
					test.stored, test.recomputed, delta, drifted, test.delta, test.drifted)
			}
		})
	}
}
