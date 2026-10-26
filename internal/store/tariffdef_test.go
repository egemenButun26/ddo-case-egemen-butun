package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"parkakil/internal/domain/tariff"
)

func standartSurum(t *testing.T) tariff.Version {
	t.Helper()
	konum, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		t.Fatalf("saat dilimi yuklenemedi: %v", err)
	}
	return tariff.Version{
		ID:          "v-standart",
		ValidFrom:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ValidTo:     time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		Location:    konum,
		FreeMinutes: 15,
		Weekday: tariff.DayRule{
			Name: "hafta_ici",
			Bands: []tariff.Band{
				{UpToMinutes: 60, UnitMinutes: 15, UnitPrice: 1000},
				{UnitMinutes: 60, UnitPrice: 3000},
			},
			Night:    &tariff.Night{StartMinute: 1320, EndMinute: 420, UnitMinutes: 60, UnitPrice: 1500},
			DailyCap: 25000,
		},
		Weekend: &tariff.DayRule{
			Name: "hafta_sonu",
			Bands: []tariff.Band{
				{UnitMinutes: 30, UnitPrice: 700},
			},
			DailyCap: 15000,
		},
		Events: []tariff.Event{
			{
				Date: tariff.Date{Year: 2026, Month: 11, Day: 1},
				Name: "derbi",
				Rule: tariff.DayRule{
					Name:  "etkinlik",
					Bands: []tariff.Band{{UnitMinutes: 60, UnitPrice: 5000}},
				},
			},
		},
	}
}

func kuralKarsilastir(t *testing.T, ad string, want, got tariff.DayRule) {
	t.Helper()
	if want.Name != got.Name {
		t.Fatalf("%s: Name = %q, beklenen %q", ad, got.Name, want.Name)
	}
	if want.DailyCap != got.DailyCap {
		t.Fatalf("%s: DailyCap = %d, beklenen %d", ad, got.DailyCap, want.DailyCap)
	}
	if len(want.Bands) != len(got.Bands) {
		t.Fatalf("%s: dilim sayisi = %d, beklenen %d", ad, len(got.Bands), len(want.Bands))
	}
	for i := range want.Bands {
		if want.Bands[i] != got.Bands[i] {
			t.Fatalf("%s: dilim %d = %+v, beklenen %+v", ad, i, got.Bands[i], want.Bands[i])
		}
	}
	switch {
	case want.Night == nil && got.Night == nil:
	case want.Night == nil || got.Night == nil:
		t.Fatalf("%s: gece penceresi nil uyusmuyor: %+v / %+v", ad, want.Night, got.Night)
	default:
		if *want.Night != *got.Night {
			t.Fatalf("%s: gece = %+v, beklenen %+v", ad, *got.Night, *want.Night)
		}
	}
}

func surumKarsilastir(t *testing.T, want, got tariff.Version) {
	t.Helper()
	if want.ID != got.ID {
		t.Fatalf("ID = %q, beklenen %q", got.ID, want.ID)
	}
	if !want.ValidFrom.Equal(got.ValidFrom) {
		t.Fatalf("ValidFrom = %v, beklenen %v", got.ValidFrom, want.ValidFrom)
	}
	if !want.ValidTo.Equal(got.ValidTo) {
		t.Fatalf("ValidTo = %v, beklenen %v", got.ValidTo, want.ValidTo)
	}
	if want.Location.String() != got.Location.String() {
		t.Fatalf("Location = %q, beklenen %q", got.Location, want.Location)
	}
	if want.FreeMinutes != got.FreeMinutes {
		t.Fatalf("FreeMinutes = %d, beklenen %d", got.FreeMinutes, want.FreeMinutes)
	}
	kuralKarsilastir(t, "hafta ici", want.Weekday, got.Weekday)
	if (want.Weekend == nil) != (got.Weekend == nil) {
		t.Fatalf("Weekend nil uyusmuyor: %+v / %+v", want.Weekend, got.Weekend)
	}
	if want.Weekend != nil {
		kuralKarsilastir(t, "hafta sonu", *want.Weekend, *got.Weekend)
	}
	if len(want.Events) != len(got.Events) {
		t.Fatalf("etkinlik sayisi = %d, beklenen %d", len(got.Events), len(want.Events))
	}
	for i := range want.Events {
		if want.Events[i].Date != got.Events[i].Date {
			t.Fatalf("etkinlik %d tarihi = %+v, beklenen %+v", i, got.Events[i].Date, want.Events[i].Date)
		}
		if want.Events[i].Name != got.Events[i].Name {
			t.Fatalf("etkinlik %d adi = %q, beklenen %q", i, got.Events[i].Name, want.Events[i].Name)
		}
		kuralKarsilastir(t, "etkinlik "+want.Events[i].Name, want.Events[i].Rule, got.Events[i].Rule)
	}
}

func TestTariffDefinitionGidisDonus(t *testing.T) {
	want := standartSurum(t)

	veri, err := EncodeTariffDefinition(want)
	if err != nil {
		t.Fatalf("kodlama hatasi: %v", err)
	}
	for _, anahtar := range []string{`"location"`, `"free_minutes"`, `"weekday"`, `"bands"`, `"night"`, `"daily_cap_kurus"`, `"events"`, `"date"`} {
		if !strings.Contains(string(veri), anahtar) {
			t.Fatalf("kodlanan JSON %q alanini icermiyor: %s", anahtar, veri)
		}
	}

	vt := want.ValidTo
	got, err := DecodeTariffDefinition(want.ID, want.ValidFrom, &vt, veri)
	if err != nil {
		t.Fatalf("cozme hatasi: %v", err)
	}
	surumKarsilastir(t, want, got)

	giris := time.Date(2026, 6, 2, 8, 0, 0, 0, time.UTC)
	cikis := time.Date(2026, 6, 2, 20, 30, 0, 0, time.UTC)
	qWant, err := tariff.Calculate(want, giris, cikis)
	if err != nil {
		t.Fatalf("hesap hatasi: %v", err)
	}
	qGot, err := tariff.Calculate(got, giris, cikis)
	if err != nil {
		t.Fatalf("hesap hatasi: %v", err)
	}
	if qWant.Total != qGot.Total {
		t.Fatalf("gidis-donus sonrasi toplam %d, beklenen %d", qGot.Total, qWant.Total)
	}
}

func TestTariffDefinitionNullAlanlar(t *testing.T) {
	veri := []byte(`{"location":"UTC","free_minutes":0,` +
		`"weekday":{"name":"h","bands":[{"up_to_minutes":0,"unit_minutes":60,"unit_price_kurus":100}],` +
		`"night":null,"daily_cap_kurus":0},"weekend":null,"events":[]}`)

	got, err := DecodeTariffDefinition("v-null", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), nil, veri)
	if err != nil {
		t.Fatalf("cozme hatasi: %v", err)
	}
	if got.Weekend != nil {
		t.Fatalf("weekend null idi, %+v dondu", got.Weekend)
	}
	if got.Weekday.Night != nil {
		t.Fatalf("night null idi, %+v dondu", got.Weekday.Night)
	}
	if !got.ValidTo.IsZero() {
		t.Fatalf("validTo nil iken ValidTo = %v", got.ValidTo)
	}
}

func TestTariffDefinitionBilinmeyenAlan(t *testing.T) {
	want := standartSurum(t)
	veri, err := EncodeTariffDefinition(want)
	if err != nil {
		t.Fatalf("kodlama hatasi: %v", err)
	}
	var ham map[string]json.RawMessage
	if err := json.Unmarshal(veri, &ham); err != nil {
		t.Fatalf("test hazirligi: %v", err)
	}
	ham["bilinmeyen"] = json.RawMessage(`1`)
	bozuk, err := json.Marshal(ham)
	if err != nil {
		t.Fatalf("test hazirligi: %v", err)
	}
	if _, err := DecodeTariffDefinition(want.ID, want.ValidFrom, nil, bozuk); err == nil {
		t.Fatal("bilinmeyen alan kabul edildi")
	}
}

func TestTariffDefinitionBozukJSON(t *testing.T) {
	if _, err := DecodeTariffDefinition("v", time.Now(), nil, []byte(`{`)); err == nil {
		t.Fatal("bozuk JSON kabul edildi")
	}
}

func TestTariffDefinitionBozukTarih(t *testing.T) {
	want := standartSurum(t)
	veri, err := EncodeTariffDefinition(want)
	if err != nil {
		t.Fatalf("kodlama hatasi: %v", err)
	}
	bozuk := []byte(strings.Replace(string(veri), `"2026-11-01"`, `"2026-13-01"`, 1))
	if _, err := DecodeTariffDefinition(want.ID, want.ValidFrom, nil, bozuk); err == nil {
		t.Fatal("gecersiz tarih kabul edildi")
	}
	bozuk = []byte(strings.Replace(string(veri), `"2026-11-01"`, `"2026-1-1"`, 1))
	if _, err := DecodeTariffDefinition(want.ID, want.ValidFrom, nil, bozuk); err == nil {
		t.Fatal("kisa tarih bicimi kabul edildi")
	}
}

func TestTariffDefinitionGecersizSaatDilimi(t *testing.T) {
	want := standartSurum(t)
	veri, err := EncodeTariffDefinition(want)
	if err != nil {
		t.Fatalf("kodlama hatasi: %v", err)
	}
	bozuk := []byte(strings.Replace(string(veri), `"Europe/Istanbul"`, `"Mars/Olympus"`, 1))
	if _, err := DecodeTariffDefinition(want.ID, want.ValidFrom, nil, bozuk); err == nil {
		t.Fatal("gecersiz saat dilimi kabul edildi")
	}
	bozuk = []byte(strings.Replace(string(veri), `"Europe/Istanbul"`, `"Local"`, 1))
	if _, err := DecodeTariffDefinition(want.ID, want.ValidFrom, nil, bozuk); err == nil {
		t.Fatal("Local saat dilimi kabul edildi")
	}
	bozuk = []byte(strings.Replace(string(veri), `"Europe/Istanbul"`, `""`, 1))
	if _, err := DecodeTariffDefinition(want.ID, want.ValidFrom, nil, bozuk); err == nil {
		t.Fatal("bos saat dilimi kabul edildi")
	}
}

func TestTariffDefinitionValidateHatasi(t *testing.T) {
	veri := []byte(`{"location":"UTC","free_minutes":0,` +
		`"weekday":{"name":"h","bands":[],"night":null,"daily_cap_kurus":0},` +
		`"weekend":null,"events":[]}`)
	_, err := DecodeTariffDefinition("v-bos", time.Now(), nil, veri)
	if err == nil {
		t.Fatal("dilimsiz tarife kabul edildi")
	}
	if !errors.Is(err, tariff.ErrInvalidTariff) {
		t.Fatalf("hata ErrInvalidTariff degil: %v", err)
	}
}

func TestTariffDefinitionEncodeGecersizKonum(t *testing.T) {
	v := standartSurum(t)
	v.Location = nil
	if _, err := EncodeTariffDefinition(v); err == nil {
		t.Fatal("nil konum kodlandi")
	}
	v.Location = time.FixedZone("Local", 0)
	if _, err := EncodeTariffDefinition(v); err == nil {
		t.Fatal("Local konum kodlandi")
	}
}
