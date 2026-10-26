// Package simulator, park kameralarindan gelen plaka olaylarini
// deterministik olarak uretir. Ag, veritabani ve time.Now kullanmaz.
package simulator

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"time"
)

// Direction bir kamera olayinin yonudur.
type Direction string

const (
	Entry Direction = "entry"
	Exit  Direction = "exit"
)

// Lot, simule edilecek otoparktir.
type Lot struct {
	ID       int64
	Code     string
	Capacity int
}

// Rates, uretimde kullanilacak bozulma oranlaridir. Oranlar tek tek
// uygulanir; OCR, eksik karakter ve bos okuma birbirini dislar.
type Rates struct {
	OCR        float64
	Missing    float64
	Empty      float64
	Duplicate  float64
	DoubleRead float64
	OutOfOrder float64
}

// DefaultRates varsayilan bozulma oranlaridir.
func DefaultRates() Rates {
	return Rates{
		OCR:        0.08,
		Missing:    0.03,
		Empty:      0.01,
		Duplicate:  0.03,
		DoubleRead: 0.03,
		OutOfOrder: 0.02,
	}
}

// Config uretim ayarlaridir. Ayni Seed ve ayni ayarlar bayt bayt ayni
// olay dizisini verir.
type Config struct {
	Seed         int64
	Lots         []Lot
	Start        time.Time
	Duration     time.Duration
	EventsPerDay int
	Rates        Rates
}

// Event, gonderilecek tek bir kamera olayidir.
type Event struct {
	LotID         int64
	CameraID      string
	CameraEventID string
	Direction     Direction
	PlateRaw      string
	CapturedAt    time.Time
	SendAt        time.Time

	cleanPlate string
	corruption corruptionKind
	duplicate  bool
	doubleRead bool
	outOfOrder bool
}

type corruptionKind string

const (
	corruptionNone    corruptionKind = "none"
	corruptionOCR     corruptionKind = "ocr"
	corruptionMissing corruptionKind = "missing"
	corruptionEmpty   corruptionKind = "empty"
)

const seedMix = 0x9e3779b97f4a7c15

// Generate, istenen sure boyunca plaka olaylarini uretir. Dizi SendAt'e
// gore sirali doner. Gecersiz ayarlarda bos dizi doner.
func Generate(cfg Config) []Event {
	rates := cfg.Rates
	if rates == (Rates{}) {
		rates = DefaultRates()
	}
	if len(cfg.Lots) == 0 || cfg.Duration <= 0 || cfg.EventsPerDay <= 0 {
		return nil
	}

	days := cfg.Duration.Hours() / 24
	factor := 1 + rates.Duplicate + rates.DoubleRead
	visitsPerDay := int(math.Round(float64(cfg.EventsPerDay) / (2 * factor)))
	if visitsPerDay < 1 {
		visitsPerDay = 1
	}
	visits := int(math.Round(float64(visitsPerDay) * days))
	if visits < len(cfg.Lots) {
		visits = len(cfg.Lots)
	}

	rng := rand.New(rand.NewPCG(uint64(cfg.Seed), uint64(cfg.Seed)^seedMix))
	poolSize := visits / 6
	if poolSize < 200 {
		poolSize = 200
	}
	if poolSize > 5000 {
		poolSize = 5000
	}
	plates := makePlatePool(rng, poolSize)
	regulars := len(plates) / 6
	if regulars < 1 {
		regulars = 1
	}

	totalCapacity := 0
	for _, lot := range cfg.Lots {
		if lot.Capacity > 0 {
			totalCapacity += lot.Capacity
		}
	}
	if totalCapacity <= 0 {
		totalCapacity = len(cfg.Lots)
	}

	seq := 0
	events := make([]Event, 0, visits*2+visits/8)

	makeEvent := func(lot Lot, dir Direction, raw, clean string,
		capturedAt, sendAt time.Time, kind corruptionKind, outOfOrder bool,
	) Event {
		id := fmt.Sprintf("sim-%d-%08d", cfg.Seed, seq)
		seq++
		return Event{
			LotID:         lot.ID,
			CameraID:      cameraID(lot, dir),
			CameraEventID: id,
			Direction:     dir,
			PlateRaw:      raw,
			CapturedAt:    capturedAt,
			SendAt:        sendAt,
			cleanPlate:    clean,
			corruption:    kind,
			outOfOrder:    outOfOrder,
		}
	}

	emit := func(ev Event) {
		events = append(events, ev)
		if rng.Float64() < rates.Duplicate {
			dup := ev
			dup.SendAt = ev.SendAt.Add(time.Duration(20+rng.IntN(100)) * time.Second)
			dup.duplicate = true
			events = append(events, dup)
		}
		if rng.Float64() < rates.DoubleRead {
			extra := ev
			extra.CameraEventID = fmt.Sprintf("sim-%d-%08d", cfg.Seed, seq)
			seq++
			extra.CapturedAt = ev.CapturedAt.Add(time.Duration(1+rng.IntN(5)) * time.Second)
			extra.SendAt = extra.CapturedAt.Add(time.Duration(rng.IntN(3)) * time.Second)
			extra.doubleRead = true
			events = append(events, extra)
		}
	}

	for i := 0; i < visits; i++ {
		lot := pickLot(rng, cfg.Lots, totalCapacity, i)
		clean := pickPlate(rng, plates, regulars)
		entryOffset := time.Duration(rng.Int64N(int64(cfg.Duration)))
		entryAt := cfg.Start.Add(entryOffset)
		exitAt := entryAt.Add(stayDuration(rng))

		entrySend := entryAt.Add(time.Duration(rng.Int64N(int64(3 * time.Second))))
		exitSend := exitAt.Add(time.Duration(rng.Int64N(int64(3 * time.Second))))
		outOfOrder := rng.Float64() < rates.OutOfOrder
		if outOfOrder {
			entrySend = exitAt.Add(time.Duration(5+rng.IntN(20)) * time.Second)
			exitSend = exitAt.Add(time.Duration(rng.IntN(3)) * time.Second)
		}

		entryRaw, entryKind := corrupt(rng, clean, rates)
		emit(makeEvent(lot, Entry, entryRaw, clean, entryAt, entrySend, entryKind, outOfOrder))

		exitRaw, exitKind := corrupt(rng, clean, rates)
		emit(makeEvent(lot, Exit, exitRaw, clean, exitAt, exitSend, exitKind, outOfOrder))
	}

	sort.SliceStable(events, func(i, j int) bool {
		if events[i].SendAt.Equal(events[j].SendAt) {
			if events[i].CameraEventID == events[j].CameraEventID {
				return events[i].CapturedAt.Before(events[j].CapturedAt)
			}
			return events[i].CameraEventID < events[j].CameraEventID
		}
		return events[i].SendAt.Before(events[j].SendAt)
	})
	return events
}

func cameraID(lot Lot, dir Direction) string {
	suffix := "IN"
	if dir == Exit {
		suffix = "OUT"
	}
	return fmt.Sprintf("CAM-%s-%s", lot.Code, suffix)
}

func pickLot(rng *rand.Rand, lots []Lot, totalCapacity, index int) Lot {
	if index < len(lots) {
		return lots[index]
	}
	esik := rng.IntN(totalCapacity)
	for _, lot := range lots {
		capacity := lot.Capacity
		if capacity <= 0 {
			continue
		}
		if esik < capacity {
			return lot
		}
		esik -= capacity
	}
	return lots[len(lots)-1]
}

func pickPlate(rng *rand.Rand, plates []string, regulars int) string {
	if rng.Float64() < 0.35 {
		return plates[rng.IntN(regulars)]
	}
	return plates[rng.IntN(len(plates))]
}

// stayDuration kisa/orta/uzun kalislari karistirir. Ortalama kalis ~60
// dakikadir; 24 saatte 20.000 olay 510 birimlik kapasiteye dengeli
// dagitildiginda kapasitenin altinda kalir. Bu tam bir garanti degildir;
// servis yine de capacity_full incelemesi uretebilir.
func stayDuration(rng *rand.Rand) time.Duration {
	r := rng.Float64()
	var minutes int
	switch {
	case r < 0.82:
		minutes = 15 + rng.IntN(31)
	case r < 0.94:
		minutes = 45 + rng.IntN(80)
	case r < 0.985:
		minutes = 180 + rng.IntN(300)
	default:
		minutes = 480 + rng.IntN(480)
	}
	return time.Duration(minutes) * time.Minute
}

func makePlatePool(rng *rand.Rand, size int) []string {
	seen := make(map[string]struct{}, size)
	plates := make([]string, 0, size)
	for len(plates) < size {
		p := randomPlate(rng)
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		plates = append(plates, p)
	}
	return plates
}

func randomPlate(rng *rand.Rand) string {
	il := 1 + rng.IntN(81)
	letters := 1 + rng.IntN(3)
	digits := 2 + rng.IntN(3)

	var b strings.Builder
	fmt.Fprintf(&b, "%02d", il)
	for i := 0; i < letters; i++ {
		b.WriteByte(byte('A' + rng.IntN(26)))
	}
	for i := 0; i < digits; i++ {
		b.WriteByte(byte('0' + rng.IntN(10)))
	}
	return b.String()
}

func corrupt(rng *rand.Rand, clean string, rates Rates) (string, corruptionKind) {
	r := rng.Float64()
	switch {
	case r < rates.OCR:
		return corruptOCR(rng, clean), corruptionOCR
	case r < rates.OCR+rates.Missing:
		return corruptMissing(rng, clean), corruptionMissing
	case r < rates.OCR+rates.Missing+rates.Empty:
		return "", corruptionEmpty
	default:
		return clean, corruptionNone
	}
}

func corruptOCR(rng *rand.Rand, clean string) string {
	runes := []rune(clean)
	type aday struct {
		pos  int
		repl rune
	}
	candidates := make([]aday, 0, len(runes))
	for i, r := range runes {
		for _, partner := range confusionPartners(r) {
			candidates = append(candidates, aday{pos: i, repl: partner})
		}
	}
	if len(candidates) == 0 {
		if len(runes) == 0 {
			return clean
		}
		pos := rng.IntN(len(runes))
		repl := runes[pos]
		if repl >= '0' && repl <= '9' {
			repl = '0' + rune(rng.IntN(10))
		} else {
			repl = 'A' + rune(rng.IntN(26))
		}
		if repl == runes[pos] {
			repl = 'A' + (repl-'A'+1)%26
		}
		runes[pos] = repl
		return string(runes)
	}
	secim := candidates[rng.IntN(len(candidates))]
	runes[secim.pos] = secim.repl
	return string(runes)
}

func corruptMissing(rng *rand.Rand, clean string) string {
	runes := []rune(clean)
	if len(runes) < 2 {
		return ""
	}
	pos := rng.IntN(len(runes))
	return string(append(runes[:pos:pos], runes[pos+1:]...))
}

var confusionPairs = [][2]rune{
	{'0', 'O'}, {'0', 'D'}, {'0', 'Q'},
	{'1', 'I'}, {'1', 'L'}, {'1', 'T'},
	{'8', 'B'}, {'5', 'S'}, {'2', 'Z'},
	{'6', 'G'}, {'4', 'A'}, {'3', 'E'},
	{'7', 'T'}, {'U', 'V'}, {'M', 'N'},
	{'H', 'N'},
}

func confusionPartners(c rune) []rune {
	var partners []rune
	for _, pair := range confusionPairs {
		switch c {
		case pair[0]:
			partners = append(partners, pair[1])
		case pair[1]:
			partners = append(partners, pair[0])
		}
	}
	return partners
}
