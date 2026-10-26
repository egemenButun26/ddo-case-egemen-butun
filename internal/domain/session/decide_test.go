package session

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"parkakil/internal/domain/plate"
)

var temel = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

func olay(d Direction, plaka string) Event {
	return Event{
		LotID:         1,
		CameraID:      "kamera-1",
		CameraEventID: "olay-1",
		Direction:     d,
		PlateRaw:      plaka,
		CapturedAt:    temel,
	}
}

func TestDefaultConfig(t *testing.T) {
	if err := DefaultConfig.Validate(); err != nil {
		t.Fatalf("varsayilan ayar gecerli olmali: %v", err)
	}
	if DefaultConfig.DuplicateWindow != 5*time.Minute {
		t.Fatalf("tekrar penceresi 5 dk olmali: %v", DefaultConfig.DuplicateWindow)
	}
	if DefaultConfig.Thresholds != plate.DefaultThresholds {
		t.Fatalf("varsayilan esikler plate varsayilanlari olmali")
	}
}

func TestConfigValidateHatalari(t *testing.T) {
	testler := []struct {
		ad  string
		cfg Config
	}{
		{"pencere sifir", Config{Thresholds: DefaultConfig.Thresholds, DuplicateWindow: 0}},
		{"pencere negatif", Config{Thresholds: DefaultConfig.Thresholds, DuplicateWindow: -time.Minute}},
		{"esik negatif", Config{Thresholds: plate.Thresholds{Auto: 0, Review: -1, Margin: 0}, DuplicateWindow: time.Minute}},
		{"review auto ustunde", Config{Thresholds: plate.Thresholds{Auto: 800, Review: 900, Margin: 100}, DuplicateWindow: time.Minute}},
	}
	for _, test := range testler {
		t.Run(test.ad, func(t *testing.T) {
			if err := test.cfg.Validate(); err == nil {
				t.Fatalf("hata bekleniyordu")
			}
		})
	}
	if err := DefaultConfig.Validate(); err != nil {
		t.Fatalf("varsayilan ayar gecerli olmali: %v", err)
	}
}

func TestDecideEntry(t *testing.T) {
	testler := []struct {
		ad      string
		plaka   string
		open    []OpenSession
		bekle   Decision
		kontrol func(*testing.T, Decision)
	}{
		{
			ad:    "bos okuma incelemeye duser",
			plaka: "   ",
			bekle: Decision{Outcome: OutcomeReview, Plate: "", Review: &Review{Reason: ReasonEmptyRead}},
		},
		{
			ad:    "bosluklu okuma normalize edilir",
			plaka: " 34 abc 123 ",
			bekle: Decision{Outcome: OutcomeOpened, Plate: "34ABC123"},
		},
		{
			ad:    "gecersiz bicim 3OABC12",
			plaka: "3OABC12",
			bekle: Decision{Outcome: OutcomeReview, Plate: "3OABC12", Review: &Review{Reason: ReasonInvalidPlate}},
		},
		{
			ad:    "gecersiz bicim ABC",
			plaka: "ABC",
			bekle: Decision{Outcome: OutcomeReview, Plate: "ABC", Review: &Review{Reason: ReasonInvalidPlate}},
		},
		{
			ad:    "acik oturum yoksa yeni oturum",
			plaka: "34ABC123",
			bekle: Decision{Outcome: OutcomeOpened, Plate: "34ABC123"},
		},
		{
			ad:    "ayni plaka aciksa tekrar giris",
			plaka: "34ABC123",
			open:  []OpenSession{{ID: 7, Plate: "34ABC123", EnteredAt: temel.Add(-time.Hour)}},
			bekle: Decision{Outcome: OutcomeDuplicateEntry, Plate: "34ABC123", SessionID: 7},
		},
		{
			ad:    "bicimi bozan B/8 okumasi plaka incelemesine duser",
			plaka: "34A8C123",
			open:  []OpenSession{{ID: 7, Plate: "34ABC123", EnteredAt: temel.Add(-time.Hour)}},
			bekle: Decision{Outcome: OutcomeReview, Plate: "34A8C123", Review: &Review{Reason: ReasonInvalidPlate}},
		},
		{
			ad:    "tek eksik karakter olasi tekrar giris",
			plaka: "34ABC12",
			open:  []OpenSession{{ID: 7, Plate: "34ABC123", EnteredAt: temel.Add(-time.Hour)}},
			bekle: Decision{
				Outcome: OutcomeReview,
				Plate:   "34ABC12",
				Review:  &Review{Reason: ReasonPossibleDuplicateEntry, Confidence: 875, SuggestedSessionID: 7},
			},
		},
		{
			ad:    "farkli plaka yeni oturum acar",
			plaka: "06ZZ9999",
			open:  []OpenSession{{ID: 7, Plate: "34ABC123", EnteredAt: temel.Add(-time.Hour)}},
			bekle: Decision{Outcome: OutcomeOpened, Plate: "06ZZ9999"},
		},
	}

	for _, test := range testler {
		t.Run(test.ad, func(t *testing.T) {
			karar, err := DecideEntry(DefaultConfig, olay(Entry, test.plaka), 100, test.open, nil)
			if err != nil {
				t.Fatalf("hata beklenmiyordu: %v", err)
			}
			kararKontrol(t, karar, test.bekle)
		})
	}
}

func olayAt(plaka string, at time.Time) Event {
	ev := olay(Entry, plaka)
	ev.CapturedAt = at
	return ev
}

func TestDecideEntryKapaliOturum(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	pencere := DefaultConfig.DuplicateWindow
	kapali := ClosedSpan{ID: 9, Plate: "34ABC123", EnteredAt: t0, ExitedAt: t0.Add(40 * time.Minute)}

	testler := []struct {
		ad     string
		plaka  string
		at     time.Time
		open   []OpenSession
		closed []ClosedSpan
		bekle  Decision
	}{
		{
			ad:     "kapanmis oturumun icinde gec giris tekrar giris",
			plaka:  "34ABC123",
			at:     t0.Add(2 * time.Second),
			closed: []ClosedSpan{kapali},
			bekle:  Decision{Outcome: OutcomeDuplicateEntry, Plate: "34ABC123", SessionID: 9},
		},
		{
			ad:     "giris penceresi siniri dahil tekrar giris",
			plaka:  "34ABC123",
			at:     t0.Add(-pencere),
			closed: []ClosedSpan{kapali},
			bekle:  Decision{Outcome: OutcomeDuplicateEntry, Plate: "34ABC123", SessionID: 9},
		},
		{
			ad:     "cikis siniri dahil tekrar giris",
			plaka:  "34ABC123",
			at:     kapali.ExitedAt,
			closed: []ClosedSpan{kapali},
			bekle:  Decision{Outcome: OutcomeDuplicateEntry, Plate: "34ABC123", SessionID: 9},
		},
		{
			ad:    "cikistan bir saniye sonra aday yok yeni oturum",
			plaka: "34ABC123",
			at:    kapali.ExitedAt.Add(time.Second),
			bekle: Decision{Outcome: OutcomeOpened, Plate: "34ABC123"},
		},
		{
			ad:     "giris penceresi bir saniye disi gec giris incelemesi",
			plaka:  "34ABC123",
			at:     t0.Add(-pencere - time.Second),
			closed: []ClosedSpan{kapali},
			bekle: Decision{
				Outcome: OutcomeReview,
				Plate:   "34ABC123",
				Review:  &Review{Reason: ReasonLateEntry, SuggestedSessionID: 9},
			},
		},
		{
			ad:     "kapanmis oturumdan onceki gec giris incelemesi",
			plaka:  "34ABC123",
			at:     t0.Add(-2 * time.Hour),
			closed: []ClosedSpan{kapali},
			bekle: Decision{
				Outcome: OutcomeReview,
				Plate:   "34ABC123",
				Review:  &Review{Reason: ReasonLateEntry, SuggestedSessionID: 9},
			},
		},
		{
			ad:     "U/V karisikligi gec okuma tekrar giris",
			plaka:  "34ABV123",
			at:     t0.Add(2 * time.Minute),
			closed: []ClosedSpan{{ID: 9, Plate: "34ABU123", EnteredAt: t0, ExitedAt: t0.Add(40 * time.Minute)}},
			bekle:  Decision{Outcome: OutcomeDuplicateEntry, Plate: "34ABV123", SessionID: 9},
		},
		{
			ad:     "tek eksik karakter kapali oturumda inceleme",
			plaka:  "34ABC12",
			at:     t0.Add(2 * time.Minute),
			closed: []ClosedSpan{kapali},
			bekle: Decision{
				Outcome: OutcomeReview,
				Plate:   "34ABC12",
				Review:  &Review{Reason: ReasonPossibleDuplicateEntry, Confidence: 875, SuggestedSessionID: 9},
			},
		},
		{
			ad:    "acik oturumdan cok onceki giris gec giris incelemesi",
			plaka: "34ABC123",
			at:    t0.Add(-4 * time.Hour),
			open:  []OpenSession{{ID: 7, Plate: "34ABC123", EnteredAt: t0}},
			bekle: Decision{
				Outcome: OutcomeReview,
				Plate:   "34ABC123",
				Review:  &Review{Reason: ReasonLateEntry, SuggestedSessionID: 7},
			},
		},
		{
			ad:    "acik oturum penceresi icinde giris tekrar giris",
			plaka: "34ABC123",
			at:    t0.Add(-2 * time.Minute),
			open:  []OpenSession{{ID: 7, Plate: "34ABC123", EnteredAt: t0}},
			bekle: Decision{Outcome: OutcomeDuplicateEntry, Plate: "34ABC123", SessionID: 7},
		},
		{
			ad:     "acik oturum eslesmese de kapsayan kapali yakalanir",
			plaka:  "34ABC123",
			at:     t0.Add(2 * time.Minute),
			open:   []OpenSession{{ID: 7, Plate: "06ZZ9999", EnteredAt: t0}},
			closed: []ClosedSpan{kapali},
			bekle:  Decision{Outcome: OutcomeDuplicateEntry, Plate: "34ABC123", SessionID: 9},
		},
	}

	for _, test := range testler {
		t.Run(test.ad, func(t *testing.T) {
			karar, err := DecideEntry(DefaultConfig, olayAt(test.plaka, test.at), 100, test.open, test.closed)
			if err != nil {
				t.Fatalf("hata beklenmiyordu: %v", err)
			}
			kararKontrol(t, karar, test.bekle)
		})
	}
}

func TestDecideEntryHatalari(t *testing.T) {
	if _, err := DecideEntry(DefaultConfig, olay(Exit, "34ABC123"), 100, nil, nil); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("exit yonunde ErrInvalidEvent bekleniyordu: %v", err)
	}

	bozuk := olay(Entry, "34ABC123")
	bozuk.LotID = 0
	if _, err := DecideEntry(DefaultConfig, bozuk, 100, nil, nil); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("gecersiz olayda ErrInvalidEvent bekleniyordu: %v", err)
	}

	bozukAyar := DefaultConfig
	bozukAyar.DuplicateWindow = 0
	if _, err := DecideEntry(bozukAyar, olay(Entry, "34ABC123"), 100, nil, nil); err == nil {
		t.Fatalf("gecersiz ayarda hata bekleniyordu")
	}

	if _, err := DecideEntry(DefaultConfig, olay(Entry, "34ABC123"), 0, nil, nil); err == nil {
		t.Fatalf("kapasite 0 icin hata bekleniyordu")
	}
	if _, err := DecideEntry(DefaultConfig, olay(Entry, "34ABC123"), -1, nil, nil); err == nil {
		t.Fatalf("negatif kapasite icin hata bekleniyordu")
	}
}

func TestDecideEntryKapasite(t *testing.T) {
	dolu := []OpenSession{
		{ID: 1, Plate: "34ABC123", EnteredAt: temel},
		{ID: 2, Plate: "06ZZ9999", EnteredAt: temel},
	}

	karar, err := DecideEntry(DefaultConfig, olay(Entry, "34XYZ123"), 2, dolu, nil)
	if err != nil {
		t.Fatalf("hata beklenmiyordu: %v", err)
	}
	kararKontrol(t, karar, Decision{
		Outcome: OutcomeReview,
		Plate:   "34XYZ123",
		Review:  &Review{Reason: ReasonCapacityFull},
	})

	karar, err = DecideEntry(DefaultConfig, olay(Entry, "06ZZ9999"), 2, dolu, nil)
	if err != nil {
		t.Fatalf("hata beklenmiyordu: %v", err)
	}
	kararKontrol(t, karar, Decision{Outcome: OutcomeDuplicateEntry, Plate: "06ZZ9999", SessionID: 2})

	karar, err = DecideEntry(DefaultConfig, olay(Entry, "34XYZ123"), 3, dolu, nil)
	if err != nil {
		t.Fatalf("hata beklenmiyordu: %v", err)
	}
	kararKontrol(t, karar, Decision{Outcome: OutcomeOpened, Plate: "34XYZ123"})
}

func TestDecideEntryKarisiklikAnahtari(t *testing.T) {
	acik := []OpenSession{{ID: 4, Plate: "34HHH123", EnteredAt: temel}}

	karar, err := DecideEntry(DefaultConfig, olay(Entry, "34MMM123"), 100, acik, nil)
	if err != nil {
		t.Fatalf("hata beklenmiyordu: %v", err)
	}
	kararKontrol(t, karar, Decision{
		Outcome: OutcomeReview,
		Plate:   "34MMM123",
		Review:  &Review{Reason: ReasonPossibleDuplicateEntry, SuggestedSessionID: 4},
	})

	karar, err = DecideEntry(DefaultConfig, olay(Entry, "34ABC123"), 100, acik, nil)
	if err != nil {
		t.Fatalf("hata beklenmiyordu: %v", err)
	}
	kararKontrol(t, karar, Decision{Outcome: OutcomeOpened, Plate: "34ABC123"})
}

func TestDecideExit(t *testing.T) {
	testler := []struct {
		ad      string
		plaka   string
		open    []OpenSession
		closed  []ClosedSession
		bekle   Decision
		kontrol func(*testing.T, Decision)
	}{
		{
			ad:    "tam eslesme oturumu kapatir",
			plaka: "34ABC123",
			open:  []OpenSession{{ID: 1, Plate: "34ABC123", EnteredAt: temel.Add(-2 * time.Hour)}},
			bekle: Decision{Outcome: OutcomeClosed, Plate: "34ABC123", SessionID: 1},
		},
		{
			ad:    "U/V karisikligi otomatik kapatir",
			plaka: "34VV123",
			open:  []OpenSession{{ID: 3, Plate: "34UV123", EnteredAt: temel.Add(-time.Hour)}},
			bekle: Decision{Outcome: OutcomeClosed, Plate: "34VV123", SessionID: 3},
		},
		{
			ad:    "tek eksik karakter dusuk guven incelemesi",
			plaka: "34ABC12",
			open:  []OpenSession{{ID: 1, Plate: "34ABC123", EnteredAt: temel.Add(-2 * time.Hour)}},
			bekle: Decision{
				Outcome: OutcomeReview,
				Plate:   "34ABC12",
				Review:  &Review{Reason: ReasonLowConfidenceReview, Confidence: 875, SuggestedSessionID: 1},
			},
		},
		{
			ad:    "iki yakin oturum belirsiz",
			plaka: "34ABC123",
			open: []OpenSession{
				{ID: 1, Plate: "34ABC124", EnteredAt: temel.Add(-2 * time.Hour)},
				{ID: 2, Plate: "34ABC125", EnteredAt: temel.Add(-time.Hour)},
			},
			bekle: Decision{
				Outcome: OutcomeReview,
				Plate:   "34ABC123",
				Review: &Review{
					Reason:             ReasonAmbiguous,
					Confidence:         875,
					SuggestedSessionID: 1,
					RunnerUpSessionID:  2,
					RunnerUpConfidence: 875,
				},
			},
		},
		{
			ad:    "giristen once cikis incelemeye duser",
			plaka: "34ABC123",
			open:  []OpenSession{{ID: 4, Plate: "34ABC123", EnteredAt: temel.Add(time.Minute)}},
			bekle: Decision{
				Outcome: OutcomeReview,
				Plate:   "34ABC123",
				Review: &Review{
					Reason:             ReasonExitBeforeEntry,
					Confidence:         1000,
					SuggestedSessionID: 4,
				},
			},
		},
		{
			ad:     "iki dakika once kapanan ayni plaka tekrar cikis",
			plaka:  "34ABC123",
			closed: []ClosedSession{{ID: 5, Plate: "34ABC123", ExitedAt: temel.Add(-2 * time.Minute)}},
			bekle:  Decision{Outcome: OutcomeDuplicateExit, Plate: "34ABC123", SessionID: 5},
		},
		{
			ad:     "U/V karisikligiyla tekrar cikis",
			plaka:  "34VV123",
			closed: []ClosedSession{{ID: 6, Plate: "34UV123", ExitedAt: temel.Add(-time.Minute)}},
			bekle:  Decision{Outcome: OutcomeDuplicateExit, Plate: "34VV123", SessionID: 6},
		},
		{
			ad:     "on dakika once kapanan plaka beklemede cikis",
			plaka:  "34ABC123",
			closed: []ClosedSession{{ID: 5, Plate: "34ABC123", ExitedAt: temel.Add(-10 * time.Minute)}},
			bekle:  Decision{Outcome: OutcomeOrphanExit, Plate: "34ABC123"},
		},
		{
			ad:    "acik yok eslesme yok gecerli plaka",
			plaka: "34ABC123",
			bekle: Decision{Outcome: OutcomeOrphanExit, Plate: "34ABC123"},
		},
		{
			ad:    "acik yok gecersiz plaka incelemeye duser",
			plaka: "3OABC12",
			bekle: Decision{Outcome: OutcomeReview, Plate: "3OABC12", Review: &Review{Reason: ReasonInvalidPlate}},
		},
		{
			ad:    "bos okuma cikista incelemeye duser",
			plaka: "  ",
			bekle: Decision{Outcome: OutcomeReview, Plate: "", Review: &Review{Reason: ReasonEmptyRead}},
		},
		{
			ad:     "tam bes dakika sinirda tekrar cikis",
			plaka:  "34ABC123",
			closed: []ClosedSession{{ID: 5, Plate: "34ABC123", ExitedAt: temel.Add(-5 * time.Minute)}},
			bekle:  Decision{Outcome: OutcomeDuplicateExit, Plate: "34ABC123", SessionID: 5},
		},
		{
			ad:     "bes dakika bir saniye beklemede cikis",
			plaka:  "34ABC123",
			closed: []ClosedSession{{ID: 5, Plate: "34ABC123", ExitedAt: temel.Add(-5*time.Minute - time.Second)}},
			bekle:  Decision{Outcome: OutcomeOrphanExit, Plate: "34ABC123"},
		},
		{
			ad:    "bicimi bozan B/8 okumasi bicim incelemesi",
			plaka: "34A8C123",
			open:  []OpenSession{{ID: 1, Plate: "34ABC123", EnteredAt: temel.Add(-2 * time.Hour)}},
			bekle: Decision{
				Outcome: OutcomeReview,
				Plate:   "34A8C123",
				Review: &Review{
					Reason:             ReasonInvalidFormat,
					Confidence:         975,
					SuggestedSessionID: 1,
				},
			},
		},
		{
			ad:     "acik oturum eslesmese de yakin kapanmis oturum yakalanir",
			plaka:  "34ABC123",
			open:   []OpenSession{{ID: 9, Plate: "06ZZ9999", EnteredAt: temel.Add(-time.Hour)}},
			closed: []ClosedSession{{ID: 5, Plate: "34ABC123", ExitedAt: temel.Add(-2 * time.Minute)}},
			bekle:  Decision{Outcome: OutcomeDuplicateExit, Plate: "34ABC123", SessionID: 5},
		},
	}

	for _, test := range testler {
		t.Run(test.ad, func(t *testing.T) {
			karar, err := DecideExit(DefaultConfig, olay(Exit, test.plaka), test.open, test.closed)
			if err != nil {
				t.Fatalf("hata beklenmiyordu: %v", err)
			}
			kararKontrol(t, karar, test.bekle)
			if test.bekle.Outcome == OutcomeReview && karar.Review != nil && test.bekle.Review != nil {
				if karar.Review.Reason == ReasonLowConfidenceReview || karar.Review.Reason == ReasonAmbiguous {
					if karar.SessionID != 0 {
						t.Fatalf("inceleme kararinda oturum kapatilmamali, SessionID: %d", karar.SessionID)
					}
				}
			}
		})
	}
}

func TestDecideExitHatalari(t *testing.T) {
	if _, err := DecideExit(DefaultConfig, olay(Entry, "34ABC123"), nil, nil); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("entry yonunde ErrInvalidEvent bekleniyordu: %v", err)
	}

	bozuk := olay(Exit, "34ABC123")
	bozuk.CapturedAt = time.Time{}
	if _, err := DecideExit(DefaultConfig, bozuk, nil, nil); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("gecersiz olayda ErrInvalidEvent bekleniyordu: %v", err)
	}

	bozukAyar := DefaultConfig
	bozukAyar.Thresholds.Review = 1000
	bozukAyar.Thresholds.Auto = 500
	if _, err := DecideExit(bozukAyar, olay(Exit, "34ABC123"), nil, nil); err == nil {
		t.Fatalf("gecersiz ayarda hata bekleniyordu")
	}
}

func TestMatchOrphanExit(t *testing.T) {
	adaylar := []OrphanExit{
		{EventID: 2, Plate: "34ABC123", CapturedAt: temel.Add(2 * time.Hour)},
		{EventID: 1, Plate: "34ABC123", CapturedAt: temel.Add(time.Hour)},
	}
	secili, bulundu, err := MatchOrphanExit(DefaultConfig, "34ABC123", temel, adaylar)
	if err != nil {
		t.Fatalf("hata beklenmiyordu: %v", err)
	}
	if !bulundu {
		t.Fatalf("eslesme bekleniyordu")
	}
	if secili.EventID != 1 {
		t.Fatalf("en erken bekleyen cikis secilmeliydi, secilen: %d", secili.EventID)
	}

	ayniZaman := []OrphanExit{
		{EventID: 8, Plate: "34ABC123", CapturedAt: temel.Add(time.Hour)},
		{EventID: 3, Plate: "34ABC123", CapturedAt: temel.Add(time.Hour)},
	}
	secili, bulundu, err = MatchOrphanExit(DefaultConfig, "34ABC123", temel, ayniZaman)
	if err != nil || !bulundu {
		t.Fatalf("esit zamanda eslesme bekleniyordu: %v", err)
	}
	if secili.EventID != 3 {
		t.Fatalf("esitlikte kucuk EventID secilmeliydi, secilen: %d", secili.EventID)
	}

	gecmis := []OrphanExit{{EventID: 1, Plate: "34ABC123", CapturedAt: temel.Add(-time.Hour)}}
	if _, bulundu, err = MatchOrphanExit(DefaultConfig, "34ABC123", temel, gecmis); err != nil || bulundu {
		t.Fatalf("giristen onceki cikis secilmemeli: bulundu=%v err=%v", bulundu, err)
	}

	karisik := []OrphanExit{{EventID: 4, Plate: "34VV123", CapturedAt: temel.Add(time.Hour)}}
	secili, bulundu, err = MatchOrphanExit(DefaultConfig, "34UV123", temel, karisik)
	if err != nil || !bulundu {
		t.Fatalf("U/V karisikligi eslesmeliydi: bulundu=%v err=%v", bulundu, err)
	}
	if secili.EventID != 4 {
		t.Fatalf("yanlis bekleyen cikis secildi: %d", secili.EventID)
	}

	farkli := []OrphanExit{{EventID: 5, Plate: "06ZZ9999", CapturedAt: temel.Add(time.Hour)}}
	if _, bulundu, err = MatchOrphanExit(DefaultConfig, "34ABC123", temel, farkli); err != nil || bulundu {
		t.Fatalf("farkli plaka eslesmemeli: bulundu=%v err=%v", bulundu, err)
	}

	if _, bulundu, err = MatchOrphanExit(DefaultConfig, "34ABC123", temel, nil); err != nil || bulundu {
		t.Fatalf("bos listede eslesme olmamali: bulundu=%v err=%v", bulundu, err)
	}

	bosPlaka := []OrphanExit{{EventID: 6, Plate: "   ", CapturedAt: temel.Add(time.Hour)}}
	if _, bulundu, err = MatchOrphanExit(DefaultConfig, "34ABC123", temel, bosPlaka); err != nil || bulundu {
		t.Fatalf("bos plaka eslesmemeli: bulundu=%v err=%v", bulundu, err)
	}

	if _, _, err = MatchOrphanExit(Config{}, "34ABC123", temel, nil); err == nil {
		t.Fatalf("gecersiz ayarda hata bekleniyordu")
	}
}

func TestOutcomeDegerleriMigrasyonlaAyni(t *testing.T) {
	veri, err := os.ReadFile("../../../migrations/00002_oturum.sql")
	if err != nil {
		t.Fatalf("migrasyon okunamadi: %v", err)
	}
	var satir string
	for _, s := range strings.Split(string(veri), "\n") {
		if strings.Contains(s, "outcome IN (") {
			satir = s
			break
		}
	}
	if satir == "" {
		t.Fatalf("plate_events.outcome CHECK satiri bulunamadi")
	}
	outcomeListesi := []Outcome{
		OutcomeOpened,
		OutcomeDuplicateEntry,
		OutcomeClosed,
		OutcomeDuplicateExit,
		OutcomeOrphanExit,
		OutcomeReview,
	}
	for _, o := range outcomeListesi {
		if !strings.Contains(satir, "'"+string(o)+"'") {
			t.Fatalf("migrasyonda %q outcome degeri yok: %s", o, satir)
		}
	}
}

func kararKontrol(t *testing.T, elde, bekle Decision) {
	t.Helper()
	if elde.Outcome != bekle.Outcome {
		t.Fatalf("outcome: elde %q, bekle %q", elde.Outcome, bekle.Outcome)
	}
	if elde.Plate != bekle.Plate {
		t.Fatalf("plaka: elde %q, bekle %q", elde.Plate, bekle.Plate)
	}
	if elde.SessionID != bekle.SessionID {
		t.Fatalf("session id: elde %d, bekle %d", elde.SessionID, bekle.SessionID)
	}
	if bekle.Review == nil {
		if elde.Review != nil {
			t.Fatalf("inceleme beklenmiyordu: %+v", elde.Review)
		}
		return
	}
	if elde.Review == nil {
		t.Fatalf("inceleme bekleniyordu: %+v", bekle.Review)
	}
	if *elde.Review != *bekle.Review {
		t.Fatalf("inceleme: elde %+v, bekle %+v", *elde.Review, *bekle.Review)
	}
}
