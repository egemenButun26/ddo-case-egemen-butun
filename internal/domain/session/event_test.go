package session

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func gecerliOlay() Event {
	return Event{
		LotID:         1,
		CameraID:      "kamera-1",
		CameraEventID: "olay-1",
		Direction:     Entry,
		PlateRaw:      "34 ABC 123",
		CapturedAt:    time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}
}

func TestValidateGecerli(t *testing.T) {
	ev := gecerliOlay()
	if err := ev.Validate(); err != nil {
		t.Fatalf("gecerli olay hata vermemeli: %v", err)
	}

	ev.Direction = Exit
	if err := ev.Validate(); err != nil {
		t.Fatalf("exit yonu gecerli olmali: %v", err)
	}

	ev.PlateRaw = ""
	if err := ev.Validate(); err != nil {
		t.Fatalf("bos plaka olay duzeyinde hata olmamali: %v", err)
	}
}

func TestValidateAlanHatalari(t *testing.T) {
	testler := []struct {
		ad    string
		alan  string
		degis func(*Event)
	}{
		{"lot_id sifir", "lot_id", func(e *Event) { e.LotID = 0 }},
		{"lot_id negatif", "lot_id", func(e *Event) { e.LotID = -5 }},
		{"camera_id bos", "camera_id", func(e *Event) { e.CameraID = "" }},
		{"camera_event_id bos", "camera_event_id", func(e *Event) { e.CameraEventID = "" }},
		{"direction bos", "direction", func(e *Event) { e.Direction = "" }},
		{"direction bilinmeyen", "direction", func(e *Event) { e.Direction = Direction("giris") }},
		{"captured_at sifir", "captured_at", func(e *Event) { e.CapturedAt = time.Time{} }},
	}
	for _, test := range testler {
		t.Run(test.ad, func(t *testing.T) {
			ev := gecerliOlay()
			test.degis(&ev)
			err := ev.Validate()
			if err == nil {
				t.Fatalf("hata bekleniyordu")
			}
			if !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("ErrInvalidEvent bekleniyordu: %v", err)
			}
			if !strings.Contains(err.Error(), test.alan) {
				t.Fatalf("hata %q alanini icermeli: %v", test.alan, err)
			}
		})
	}
}

func TestValidateUzunlukBaytSiniri(t *testing.T) {
	ev := gecerliOlay()
	ev.CameraID = strings.Repeat("ğ", 64)
	if len(ev.CameraID) != 128 {
		t.Fatalf("test verisi 128 bayt olmali: %d", len(ev.CameraID))
	}
	if err := ev.Validate(); err != nil {
		t.Fatalf("64 Turkce karakter (128 bayt) sinirda gecerli olmali: %v", err)
	}

	ev.CameraID = strings.Repeat("ğ", 65)
	err := ev.Validate()
	if err == nil {
		t.Fatalf("130 bayt icin hata bekleniyordu")
	}
	if !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("ErrInvalidEvent bekleniyordu: %v", err)
	}
	if !strings.Contains(err.Error(), "bayt") {
		t.Fatalf("hata mesaji bayt birimini icermeli: %v", err)
	}
	if strings.Contains(err.Error(), "karakter") {
		t.Fatalf("hata mesaji karakter birimini icermemeli: %v", err)
	}
}

func TestValidateUzunlukSinirlari(t *testing.T) {
	ev := gecerliOlay()

	ev.CameraID = strings.Repeat("a", 128)
	ev.CameraEventID = strings.Repeat("b", 128)
	ev.PlateRaw = strings.Repeat("c", 64)
	if err := ev.Validate(); err != nil {
		t.Fatalf("128/128/64 sinirda gecerli olmali: %v", err)
	}

	testler := []struct {
		ad    string
		degis func(*Event)
	}{
		{"camera_id 129", func(e *Event) { e.CameraID = strings.Repeat("a", 129) }},
		{"camera_event_id 129", func(e *Event) { e.CameraEventID = strings.Repeat("b", 129) }},
		{"plate_raw 65", func(e *Event) { e.PlateRaw = strings.Repeat("c", 65) }},
	}
	for _, test := range testler {
		t.Run(test.ad, func(t *testing.T) {
			ev := gecerliOlay()
			test.degis(&ev)
			err := ev.Validate()
			if err == nil {
				t.Fatalf("hata bekleniyordu")
			}
			if !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("ErrInvalidEvent bekleniyordu: %v", err)
			}
		})
	}
}
