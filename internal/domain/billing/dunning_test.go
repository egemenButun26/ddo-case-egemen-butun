package billing

import (
	"errors"
	"testing"
	"time"
)

func TestDunningSchedule(t *testing.T) {
	if len(DunningSchedule) != 3 {
		t.Fatalf("DunningSchedule uzunlugu = %d, beklenen 3", len(DunningSchedule))
	}
	beklenen := []time.Duration{24 * time.Hour, 72 * time.Hour, 7 * 24 * time.Hour}
	for i, d := range beklenen {
		if DunningSchedule[i] != d {
			t.Fatalf("DunningSchedule[%d] = %v, beklenen %v", i, DunningSchedule[i], d)
		}
	}

	ilk := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	for i, d := range beklenen {
		got, ok := NextRetry(i+1, ilk)
		if !ok {
			t.Fatalf("NextRetry(%d) ok = false", i+1)
		}
		if !got.Equal(ilk.Add(d)) {
			t.Fatalf("NextRetry(%d) = %v, beklenen %v", i+1, got, ilk.Add(d))
		}
	}

	for _, attempts := range []int{-1, 0, 4, 5} {
		got, ok := NextRetry(attempts, ilk)
		if ok {
			t.Fatalf("NextRetry(%d) ok = true", attempts)
		}
		if !got.IsZero() {
			t.Fatalf("NextRetry(%d) = %v, sifir beklenirdi", attempts, got)
		}
	}
}

func TestSubscriptionTransitions(t *testing.T) {
	for _, s := range []Status{StatusActive, StatusPastDue, StatusSuspended, StatusCancelled} {
		if !s.Valid() {
			t.Fatalf("%q gecerli olmaliydi", s)
		}
	}
	if Status("baska").Valid() {
		t.Fatal("bilinmeyen durum gecerli olmamaliydi")
	}

	gecerli := [][2]Status{
		{StatusActive, StatusActive},
		{StatusActive, StatusPastDue},
		{StatusActive, StatusSuspended},
		{StatusActive, StatusCancelled},
		{StatusPastDue, StatusActive},
		{StatusPastDue, StatusPastDue},
		{StatusPastDue, StatusSuspended},
		{StatusPastDue, StatusCancelled},
		{StatusSuspended, StatusActive},
		{StatusSuspended, StatusSuspended},
		{StatusSuspended, StatusCancelled},
		{StatusCancelled, StatusCancelled},
	}
	for _, pair := range gecerli {
		got, err := Transition(pair[0], pair[1])
		if err != nil {
			t.Fatalf("Transition(%q, %q): %v", pair[0], pair[1], err)
		}
		if got != pair[1] {
			t.Fatalf("Transition(%q, %q) = %q", pair[0], pair[1], got)
		}
	}

	gecersiz := [][2]Status{
		{StatusCancelled, StatusActive},
		{StatusCancelled, StatusPastDue},
		{StatusCancelled, StatusSuspended},
		{StatusSuspended, StatusPastDue},
	}
	for _, pair := range gecersiz {
		if _, err := Transition(pair[0], pair[1]); !errors.Is(err, ErrInvalidTransition) {
			t.Fatalf("Transition(%q, %q) icin ErrInvalidTransition beklenirdi, gelen %v", pair[0], pair[1], err)
		}
	}

	if _, err := Transition("baska", StatusActive); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("gecersiz kaynak icin ErrInvalidStatus beklenirdi, gelen %v", err)
	}
	if _, err := Transition(StatusActive, "baska"); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("gecersiz hedef icin ErrInvalidStatus beklenirdi, gelen %v", err)
	}
}
