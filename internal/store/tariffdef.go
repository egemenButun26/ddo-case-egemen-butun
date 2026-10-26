package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"parkakil/internal/domain/tariff"
)

// JSON bicimi yalniz tanimi tasir; ID ve gecerlilik tarihleri tablo sutunlarindadir.

type tariffDefinition struct {
	Location    string             `json:"location"`
	FreeMinutes int                `json:"free_minutes"`
	Weekday     dayRuleDefinition  `json:"weekday"`
	Weekend     *dayRuleDefinition `json:"weekend"`
	Events      []eventDefinition  `json:"events"`
}

type dayRuleDefinition struct {
	Name     string           `json:"name"`
	Bands    []bandDefinition `json:"bands"`
	Night    *nightDefinition `json:"night"`
	DailyCap tariff.Kurus     `json:"daily_cap_kurus"`
}

type bandDefinition struct {
	UpToMinutes    int          `json:"up_to_minutes"`
	UnitMinutes    int          `json:"unit_minutes"`
	UnitPriceKurus tariff.Kurus `json:"unit_price_kurus"`
}

type nightDefinition struct {
	StartMinute    int          `json:"start_minute"`
	EndMinute      int          `json:"end_minute"`
	UnitMinutes    int          `json:"unit_minutes"`
	UnitPriceKurus tariff.Kurus `json:"unit_price_kurus"`
}

type eventDefinition struct {
	Date string            `json:"date"`
	Name string            `json:"name"`
	Rule dayRuleDefinition `json:"rule"`
}

// EncodeTariffDefinition, tarife surumunu JSON tanimina cevirir.
func EncodeTariffDefinition(v tariff.Version) ([]byte, error) {
	if v.Location == nil {
		return nil, fmt.Errorf("tarife %q: saat dilimi yok", v.ID)
	}
	ad := v.Location.String()
	if ad == "" || ad == "Local" {
		return nil, fmt.Errorf("tarife %q: gecersiz saat dilimi %q", v.ID, ad)
	}

	d := tariffDefinition{
		Location:    ad,
		FreeMinutes: v.FreeMinutes,
		Weekday:     encodeDayRule(v.Weekday),
	}
	if v.Weekend != nil {
		w := encodeDayRule(*v.Weekend)
		d.Weekend = &w
	}
	if v.Events != nil {
		d.Events = make([]eventDefinition, 0, len(v.Events))
		for _, e := range v.Events {
			tarih, err := formatDate(e.Date)
			if err != nil {
				return nil, fmt.Errorf("tarife %q: %w", v.ID, err)
			}
			d.Events = append(d.Events, eventDefinition{
				Date: tarih,
				Name: e.Name,
				Rule: encodeDayRule(e.Rule),
			})
		}
	}

	veri, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("tarife %q: tanim kodlanamadi: %w", v.ID, err)
	}
	return veri, nil
}

// DecodeTariffDefinition, JSON tanimini tarife surumune cevirir ve dogrular.
func DecodeTariffDefinition(id string, validFrom time.Time, validTo *time.Time, def []byte) (tariff.Version, error) {
	var d tariffDefinition
	dec := json.NewDecoder(bytes.NewReader(def))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return tariff.Version{}, fmt.Errorf("tarife %q: tanim cozulemedi: %w", id, err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return tariff.Version{}, fmt.Errorf("tarife %q: tanimdan sonra fazladan veri var", id)
		}
		return tariff.Version{}, fmt.Errorf("tarife %q: tanim cozulemedi: %w", id, err)
	}

	if strings.TrimSpace(d.Location) == "" || d.Location == "Local" {
		return tariff.Version{}, fmt.Errorf("tarife %q: gecersiz saat dilimi %q", id, d.Location)
	}
	konum, err := time.LoadLocation(d.Location)
	if err != nil {
		return tariff.Version{}, fmt.Errorf("tarife %q: saat dilimi %q: %w", id, d.Location, err)
	}

	v := tariff.Version{
		ID:          id,
		ValidFrom:   validFrom,
		Location:    konum,
		FreeMinutes: d.FreeMinutes,
		Weekday:     decodeDayRule(d.Weekday),
	}
	if validTo != nil {
		v.ValidTo = *validTo
	}
	if d.Weekend != nil {
		w := decodeDayRule(*d.Weekend)
		v.Weekend = &w
	}
	if d.Events != nil {
		v.Events = make([]tariff.Event, 0, len(d.Events))
		for _, e := range d.Events {
			tarih, err := parseDate(e.Date)
			if err != nil {
				return tariff.Version{}, fmt.Errorf("tarife %q: etkinlik %q: %w", id, e.Name, err)
			}
			v.Events = append(v.Events, tariff.Event{
				Date: tarih,
				Name: e.Name,
				Rule: decodeDayRule(e.Rule),
			})
		}
	}

	if err := v.Validate(); err != nil {
		return tariff.Version{}, err
	}
	return v, nil
}

func encodeDayRule(r tariff.DayRule) dayRuleDefinition {
	d := dayRuleDefinition{
		Name:     r.Name,
		Bands:    make([]bandDefinition, 0, len(r.Bands)),
		DailyCap: r.DailyCap,
	}
	for _, b := range r.Bands {
		d.Bands = append(d.Bands, bandDefinition{
			UpToMinutes:    b.UpToMinutes,
			UnitMinutes:    b.UnitMinutes,
			UnitPriceKurus: b.UnitPrice,
		})
	}
	if r.Night != nil {
		d.Night = &nightDefinition{
			StartMinute:    r.Night.StartMinute,
			EndMinute:      r.Night.EndMinute,
			UnitMinutes:    r.Night.UnitMinutes,
			UnitPriceKurus: r.Night.UnitPrice,
		}
	}
	return d
}

func decodeDayRule(d dayRuleDefinition) tariff.DayRule {
	r := tariff.DayRule{
		Name:     d.Name,
		Bands:    make([]tariff.Band, 0, len(d.Bands)),
		DailyCap: d.DailyCap,
	}
	for _, b := range d.Bands {
		r.Bands = append(r.Bands, tariff.Band{
			UpToMinutes: b.UpToMinutes,
			UnitMinutes: b.UnitMinutes,
			UnitPrice:   b.UnitPriceKurus,
		})
	}
	if d.Night != nil {
		r.Night = &tariff.Night{
			StartMinute: d.Night.StartMinute,
			EndMinute:   d.Night.EndMinute,
			UnitMinutes: d.Night.UnitMinutes,
			UnitPrice:   d.Night.UnitPriceKurus,
		}
	}
	return r
}

func formatDate(d tariff.Date) (string, error) {
	s := fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
	if _, err := parseDate(s); err != nil {
		return "", fmt.Errorf("gecersiz tarih %q", s)
	}
	return s, nil
}

func parseDate(s string) (tariff.Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return tariff.Date{}, fmt.Errorf("gecersiz tarih %q: %w", s, err)
	}
	if t.Format("2006-01-02") != s {
		return tariff.Date{}, fmt.Errorf("gecersiz tarih %q", s)
	}
	return tariff.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}, nil
}
