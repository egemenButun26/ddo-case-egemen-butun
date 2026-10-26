package simulator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"testing"
	"time"

	"parkakil/internal/domain/plate"
)

func testStart(t *testing.T) time.Time {
	t.Helper()
	start, err := time.Parse(time.RFC3339, "2026-10-27T06:00:00+03:00")
	if err != nil {
		t.Fatalf("baslangic zamani ayristirilamadi: %v", err)
	}
	return start
}

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		Seed: 20261026,
		Lots: []Lot{
			{ID: 1, Code: "KADIKOY", Capacity: 120},
			{ID: 2, Code: "BESIKTAS", Capacity: 80},
			{ID: 3, Code: "SISLI", Capacity: 150},
			{ID: 4, Code: "USKUDAR", Capacity: 60},
			{ID: 5, Code: "BAKIRKOY", Capacity: 100},
		},
		Start:        testStart(t),
		Duration:     24 * time.Hour,
		EventsPerDay: 20000,
		Rates:        DefaultRates(),
	}
}

func hashEvents(events []Event) string {
	h := sha256.New()
	for _, ev := range events {
		fmt.Fprintf(h, "%d|%s|%s|%s|%s|%d|%d|%s\n",
			ev.LotID, ev.CameraID, ev.CameraEventID, ev.Direction,
			ev.PlateRaw, ev.CapturedAt.UnixNano(), ev.SendAt.UnixNano(), ev.corruption)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestSameSeedSameEvents(t *testing.T) {
	cfg := testConfig(t)
	first := Generate(cfg)
	second := Generate(cfg)
	if len(first) < 2000 {
		t.Fatalf("beklenen en az 2000 olay, bulunan %d", len(first))
	}
	if !reflect.DeepEqual(first[:2000], second[:2000]) {
		t.Fatal("ayni tohum ilk 2000 olayda ayni diziyi vermedi")
	}
	if got, want := hashEvents(first[:2000]), hashEvents(second[:2000]); got != want {
		t.Fatalf("ozet hash farkli: %s != %s", got, want)
	}
}

func TestDifferentSeedDifferentEvents(t *testing.T) {
	a := testConfig(t)
	a.Seed = 1
	b := testConfig(t)
	b.Seed = 2

	ea := Generate(a)
	eb := Generate(b)
	if hashEvents(ea) == hashEvents(eb) {
		t.Fatal("farkli tohumlar ayni diziyi verdi")
	}
}

func TestEventsPerDayTolerance(t *testing.T) {
	events := Generate(testConfig(t))
	if len(events) < 19000 || len(events) > 21000 {
		t.Fatalf("gunluk olay sayisi %d, beklenen 20000 +/- yuzde 5", len(events))
	}
}

func TestCorruptionRates(t *testing.T) {
	events := Generate(testConfig(t))
	base := 0
	var ocr, missing, empty, duplicate, doubleRead, outOfOrder int
	for _, ev := range events {
		switch {
		case ev.duplicate:
			duplicate++
		case ev.doubleRead:
			doubleRead++
		default:
			base++
			switch ev.corruption {
			case corruptionOCR:
				ocr++
			case corruptionMissing:
				missing++
			case corruptionEmpty:
				empty++
			}
			if ev.outOfOrder {
				outOfOrder++
			}
		}
	}
	if base == 0 {
		t.Fatal("temel olay yok")
	}
	checkRate := func(ad string, sayi, toplam int, min, max float64) {
		t.Helper()
		oran := float64(sayi) / float64(toplam)
		if oran < min || oran > max {
			t.Fatalf("%s orani %.4f, beklenen %.2f..%.2f", ad, oran, min, max)
		}
	}
	checkRate("OCR", ocr, base, 0.06, 0.10)
	checkRate("eksik karakter", missing, base, 0.01, 0.05)
	checkRate("bos okuma", empty, base, 0.00, 0.03)
	checkRate("tekrar olay", duplicate, base, 0.01, 0.05)
	checkRate("cift okuma", doubleRead, base, 0.01, 0.05)
	checkRate("sirasiz gonderim", outOfOrder, base, 0.00, 0.04)
}

func TestEventsSortedBySendAt(t *testing.T) {
	events := Generate(testConfig(t))
	for i := 1; i < len(events); i++ {
		if events[i].SendAt.Before(events[i-1].SendAt) {
			t.Fatalf("%d. olay SendAt sirasini bozuyor: %s < %s",
				i, events[i].SendAt, events[i-1].SendAt)
		}
	}
}

func TestDuplicateEventsSameContent(t *testing.T) {
	events := Generate(testConfig(t))
	temel := make(map[string]Event)
	for _, ev := range events {
		if !ev.duplicate && !ev.doubleRead {
			temel[ev.CameraEventID] = ev
		}
	}
	bulunan := 0
	for _, ev := range events {
		if !ev.duplicate {
			continue
		}
		bulunan++
		asil, ok := temel[ev.CameraEventID]
		if !ok {
			t.Fatalf("tekrar olayin temeli yok: %s", ev.CameraEventID)
		}
		if ev.LotID != asil.LotID || ev.CameraID != asil.CameraID ||
			ev.Direction != asil.Direction || ev.PlateRaw != asil.PlateRaw ||
			!ev.CapturedAt.Equal(asil.CapturedAt) {
			t.Fatalf("tekrar olay icerigi farkli: %s", ev.CameraEventID)
		}
	}
	if bulunan == 0 {
		t.Fatal("tekrar olay uretilmedi")
	}
}

func TestCleanPlatesAreValidTR(t *testing.T) {
	events := Generate(testConfig(t))
	for _, ev := range events {
		if !plate.ValidTR(ev.cleanPlate) {
			t.Fatalf("gecerli olmayan temiz plaka: %q", ev.cleanPlate)
		}
	}
}

func TestAllLotsReceiveEvents(t *testing.T) {
	cfg := testConfig(t)
	events := Generate(cfg)
	gorulen := make(map[int64]bool)
	for _, ev := range events {
		gorulen[ev.LotID] = true
	}
	for _, lot := range cfg.Lots {
		if !gorulen[lot.ID] {
			t.Fatalf("%d numarali otopark olay almadi", lot.ID)
		}
	}
}

func TestInvalidConfigReturnsEmpty(t *testing.T) {
	if got := Generate(Config{}); got != nil {
		t.Fatalf("gecersiz ayar bos dizi dondurmeliydi, %d olay", len(got))
	}
	cfg := testConfig(t)
	cfg.EventsPerDay = 0
	if got := Generate(cfg); got != nil {
		t.Fatalf("sifir olay/gun bos dizi dondurmeliydi, %d olay", len(got))
	}
}

func TestDirectionAndCameraID(t *testing.T) {
	events := Generate(testConfig(t))
	for _, ev := range events {
		if ev.Direction != Entry && ev.Direction != Exit {
			t.Fatalf("gecersiz yon: %q", ev.Direction)
		}
		want := "CAM-" + lotCode(ev.LotID) + "-IN"
		if ev.Direction == Exit {
			want = "CAM-" + lotCode(ev.LotID) + "-OUT"
		}
		if ev.CameraID != want {
			t.Fatalf("kamera kimligi %q, beklenen %q", ev.CameraID, want)
		}
	}
}

func lotCode(lotID int64) string {
	kodlar := map[int64]string{
		1: "KADIKOY", 2: "BESIKTAS", 3: "SISLI", 4: "USKUDAR", 5: "BAKIRKOY",
	}
	return kodlar[lotID]
}
