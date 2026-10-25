package tariff

import (
	"errors"
	"fmt"
	"time"
)

// Date: saat dilimsiz takvim gunu.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// Band: dakika siniri, birim dakikasi ve birim fiyati. UpToMinutes 0 = sinirsiz son dilim.
type Band struct {
	UpToMinutes int
	UnitMinutes int
	UnitPrice   Kurus
}

// Night: gecerli gunun basindan dakika cinsinden gece penceresi (0..1439).
type Night struct {
	StartMinute int
	EndMinute   int
	UnitMinutes int
	UnitPrice   Kurus
}

// DayRule: bir gunun dilimleri, gece penceresi ve gunluk tavani.
type DayRule struct {
	Name     string
	Bands    []Band
	Night    *Night
	DailyCap Kurus
}

// Event: belirli bir takvim gunune ozel kural.
type Event struct {
	Date Date
	Name string
	Rule DayRule
}

// Version: zaman gecerlilikli tarife surumu.
type Version struct {
	ID          string
	ValidFrom   time.Time
	ValidTo     time.Time // sifir = acik uclu
	Location    *time.Location
	FreeMinutes int
	Weekday     DayRule
	Weekend     *DayRule // nil ise Weekday kullanilir
	Events      []Event
}

// MaxTariffMinutes: tarife alanlari icin ust sinir; en uzun oturum siniri olan
// 400 gunle aynidir (400*24*60 = 576000 dakika).
const MaxTariffMinutes = 400 * 24 * 60

var (
	ErrNoVersion        = errors.New("gecerli tarife surumu yok")
	ErrAmbiguousVersion = errors.New("birden fazla gecerli tarife surumu")
	ErrNegativeDuration = errors.New("cikis giristen once")
	ErrTooLong          = errors.New("oturum suresi 400 gunden uzun")
	ErrInvalidTariff    = errors.New("gecersiz tarife")
)

// Validate: surumun butun alanlarini denetler.
func (v Version) Validate() error {
	if v.ID == "" {
		return fmt.Errorf("%w: tarife: ID bos", ErrInvalidTariff)
	}
	if v.Location == nil {
		return fmt.Errorf("%w: tarife %q: saat dilimi yok", ErrInvalidTariff, v.ID)
	}
	if v.FreeMinutes < 0 {
		return fmt.Errorf("%w: tarife %q: FreeMinutes negatif (%d)", ErrInvalidTariff, v.ID, v.FreeMinutes)
	}
	if v.FreeMinutes > MaxTariffMinutes {
		return fmt.Errorf("%w: tarife %q: FreeMinutes cok buyuk (%d > %d)", ErrInvalidTariff, v.ID, v.FreeMinutes, MaxTariffMinutes)
	}
	if !v.ValidTo.IsZero() && !v.ValidTo.After(v.ValidFrom) {
		return fmt.Errorf("%w: tarife %q: gecerlilik araligi ters ya da bos", ErrInvalidTariff, v.ID)
	}
	if err := validateRule(v.ID, "hafta ici", v.Weekday); err != nil {
		return err
	}
	if v.Weekend != nil {
		if err := validateRule(v.ID, "hafta sonu", *v.Weekend); err != nil {
			return err
		}
	}
	gorulen := make(map[Date]bool, len(v.Events))
	for _, e := range v.Events {
		if gorulen[e.Date] {
			return fmt.Errorf("%w: tarife %q: ayni tarihe iki etkinlik: %d-%02d-%02d", ErrInvalidTariff, v.ID, e.Date.Year, e.Date.Month, e.Date.Day)
		}
		gorulen[e.Date] = true
		if err := validateRule(v.ID, "etkinlik "+e.Name, e.Rule); err != nil {
			return err
		}
	}
	return nil
}

func validateRule(versionID, ad string, r DayRule) error {
	if len(r.Bands) == 0 {
		return fmt.Errorf("%w: tarife %q: %s dilimi yok", ErrInvalidTariff, versionID, ad)
	}
	onceki := 0
	for i, b := range r.Bands {
		if b.UnitMinutes <= 0 {
			return fmt.Errorf("%w: tarife %q: %s UnitMinutes gecersiz (%d)", ErrInvalidTariff, versionID, ad, b.UnitMinutes)
		}
		if b.UnitMinutes > MaxTariffMinutes {
			return fmt.Errorf("%w: tarife %q: %s UnitMinutes cok buyuk (%d > %d)", ErrInvalidTariff, versionID, ad, b.UnitMinutes, MaxTariffMinutes)
		}
		if b.UnitPrice < 0 {
			return fmt.Errorf("%w: tarife %q: %s birim fiyati negatif", ErrInvalidTariff, versionID, ad)
		}
		son := i == len(r.Bands)-1
		if son {
			if b.UpToMinutes != 0 {
				return fmt.Errorf("%w: tarife %q: %s son dilim sinirsiz olmali", ErrInvalidTariff, versionID, ad)
			}
			continue
		}
		if b.UpToMinutes <= 0 {
			return fmt.Errorf("%w: tarife %q: %s UpToMinutes gecersiz (%d)", ErrInvalidTariff, versionID, ad, b.UpToMinutes)
		}
		if b.UpToMinutes > MaxTariffMinutes {
			return fmt.Errorf("%w: tarife %q: %s UpToMinutes cok buyuk (%d > %d)", ErrInvalidTariff, versionID, ad, b.UpToMinutes, MaxTariffMinutes)
		}
		if i > 0 && b.UpToMinutes <= onceki {
			return fmt.Errorf("%w: tarife %q: %s dilimleri artan degil", ErrInvalidTariff, versionID, ad)
		}
		onceki = b.UpToMinutes
	}
	if n := r.Night; n != nil {
		if n.StartMinute < 0 || n.StartMinute > 1439 {
			return fmt.Errorf("%w: tarife %q: %s gece baslangici gecersiz", ErrInvalidTariff, versionID, ad)
		}
		if n.EndMinute < 0 || n.EndMinute > 1439 {
			return fmt.Errorf("%w: tarife %q: %s gece bitisi gecersiz", ErrInvalidTariff, versionID, ad)
		}
		if n.StartMinute == n.EndMinute {
			return fmt.Errorf("%w: tarife %q: %s gece penceresi bos", ErrInvalidTariff, versionID, ad)
		}
		if n.UnitMinutes <= 0 {
			return fmt.Errorf("%w: tarife %q: %s Night.UnitMinutes gecersiz (%d)", ErrInvalidTariff, versionID, ad, n.UnitMinutes)
		}
		if n.UnitMinutes > MaxTariffMinutes {
			return fmt.Errorf("%w: tarife %q: %s Night.UnitMinutes cok buyuk (%d > %d)", ErrInvalidTariff, versionID, ad, n.UnitMinutes, MaxTariffMinutes)
		}
		if n.UnitPrice < 0 {
			return fmt.Errorf("%w: tarife %q: %s gece birim fiyati negatif", ErrInvalidTariff, versionID, ad)
		}
	}
	if r.DailyCap < 0 {
		return fmt.Errorf("%w: tarife %q: %s gunluk tavan negatif", ErrInvalidTariff, versionID, ad)
	}
	return nil
}

// ValidateCatalog: butun surumleri, ID tekrarini ve gecerlilik cakismasini denetler.
// [ValidFrom, ValidTo) yari acik aralik; bitisik araliklar cakisma degildir.
func ValidateCatalog(vs []Version) error {
	gorulen := make(map[string]bool, len(vs))
	for i, v := range vs {
		if err := v.Validate(); err != nil {
			return err
		}
		if gorulen[v.ID] {
			return fmt.Errorf("tarife: ID tekrari: %q", v.ID)
		}
		gorulen[v.ID] = true
		for j := 0; j < i; j++ {
			if overlaps(vs[j], v) {
				return fmt.Errorf("tarife: %q ve %q gecerlilik araliklari cakisiyor", vs[j].ID, v.ID)
			}
		}
	}
	return nil
}

func overlaps(a, b Version) bool {
	if !a.ValidTo.IsZero() && !b.ValidFrom.Before(a.ValidTo) {
		return false
	}
	if !b.ValidTo.IsZero() && !a.ValidFrom.Before(b.ValidTo) {
		return false
	}
	return true
}

// SelectVersion: at aninda gecerli surumu secer. ValidFrom <= at < ValidTo.
func SelectVersion(vs []Version, at time.Time) (Version, error) {
	var secili *Version
	sayac := 0
	for i := range vs {
		if !at.Before(vs[i].ValidFrom) && (vs[i].ValidTo.IsZero() || at.Before(vs[i].ValidTo)) {
			sayac++
			if sayac == 1 {
				secili = &vs[i]
			}
		}
	}
	if sayac == 0 {
		return Version{}, ErrNoVersion
	}
	if sayac > 1 {
		return Version{}, ErrAmbiguousVersion
	}
	return *secili, nil
}
