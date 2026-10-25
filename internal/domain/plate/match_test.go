package plate

import "testing"

func aday(id, plaka string) Candidate {
	return Candidate{ID: id, Plate: plaka}
}

type beklenen struct {
	karar      Decision
	neden      string
	okuma      string
	enIyiID    string
	conf       int
	ikinciID   string
	ikinciConf int
}

func kontrol(t *testing.T, ad string, read string, adaylar []Candidate, esik Thresholds, b beklenen) {
	t.Helper()
	sonuc, err := Match(read, adaylar, esik)
	if err != nil {
		t.Fatalf("%s: beklenmeyen hata: %v", ad, err)
	}
	if sonuc.Decision != b.karar {
		t.Fatalf("%s: karar %q, beklenen %q", ad, sonuc.Decision, b.karar)
	}
	if sonuc.Reason != b.neden {
		t.Fatalf("%s: neden %q, beklenen %q", ad, sonuc.Reason, b.neden)
	}
	if sonuc.Read != b.okuma {
		t.Fatalf("%s: okuma %q, beklenen %q", ad, sonuc.Read, b.okuma)
	}
	if b.enIyiID == "" {
		if sonuc.Best != nil {
			t.Fatalf("%s: en iyi aday nil olmalı, gelen %q", ad, sonuc.Best.ID)
		}
	} else {
		if sonuc.Best == nil || sonuc.Best.ID != b.enIyiID {
			t.Fatalf("%s: en iyi aday %v, beklenen %q", ad, sonuc.Best, b.enIyiID)
		}
	}
	if b.ikinciID == "" {
		if sonuc.RunnerUp != nil {
			t.Fatalf("%s: ikinci aday nil olmalı, gelen %q", ad, sonuc.RunnerUp.ID)
		}
	} else {
		if sonuc.RunnerUp == nil || sonuc.RunnerUp.ID != b.ikinciID {
			t.Fatalf("%s: ikinci aday %v, beklenen %q", ad, sonuc.RunnerUp, b.ikinciID)
		}
	}
	if b.conf != 0 || b.enIyiID != "" {
		if sonuc.Confidence != b.conf {
			t.Fatalf("%s: güven %d, beklenen %d", ad, sonuc.Confidence, b.conf)
		}
	}
	if sonuc.RunnerUpConfidence != b.ikinciConf {
		t.Fatalf("%s: ikinci güven %d, beklenen %d", ad, sonuc.RunnerUpConfidence, b.ikinciConf)
	}
}

func TestMatchTemelKararlar(t *testing.T) {
	varsayilan := DefaultThresholds

	kontrol(t, "tam eşleşme", "34 abc 123", []Candidate{aday("c1", "34ABC123")}, varsayilan,
		beklenen{karar: DecisionExact, neden: "exact", okuma: "34ABC123", enIyiID: "c1", conf: 1000})

	kontrol(t, "tek B/8 karışıklığı", "34ABB123", []Candidate{aday("c1", "34AB8123")}, varsayilan,
		beklenen{karar: DecisionAuto, neden: "auto", okuma: "34ABB123", enIyiID: "c1", conf: 975})

	kontrol(t, "tek eksik karakter", "34ABC12", []Candidate{aday("c1", "34ABC123")}, varsayilan,
		beklenen{karar: DecisionReview, neden: "low_confidence_review", okuma: "34ABC12", enIyiID: "c1", conf: 875})

	kontrol(t, "farklı plaka", "34ABC123", []Candidate{aday("c1", "99XYZ987")}, varsayilan,
		beklenen{karar: DecisionNone, neden: "low_confidence", okuma: "34ABC123", enIyiID: "c1", conf: 0})

	kontrol(t, "boş okuma", "  -  ", []Candidate{aday("c1", "34ABC123")}, varsayilan,
		beklenen{karar: DecisionNone, neden: "empty_read", okuma: ""})

	kontrol(t, "aday yok", "34ABC123", nil, varsayilan,
		beklenen{karar: DecisionNone, neden: "no_candidates", okuma: "34ABC123"})
}

func TestMatchIkinciAday(t *testing.T) {
	varsayilan := DefaultThresholds

	kontrol(t, "iki aday yakın", "34ABC123",
		[]Candidate{aday("c1", "34ABC128"), aday("c2", "34ABC129")}, varsayilan,
		beklenen{karar: DecisionReview, neden: "ambiguous", okuma: "34ABC123", enIyiID: "c1", conf: 875,
			ikinciID: "c2", ikinciConf: 875})

	kontrol(t, "iki aday aynı plakaya tam eşit", "34ABC123",
		[]Candidate{aday("c1", "34ABC123"), aday("c2", "34 abc 123")}, varsayilan,
		beklenen{karar: DecisionReview, neden: "ambiguous_exact", okuma: "34ABC123", enIyiID: "c1", conf: 1000,
			ikinciID: "c2", ikinciConf: 1000})

	kontrol(t, "biçimi bozuk okuma yüksek güvenle", "34ABC12345",
		[]Candidate{aday("c1", "34ABC1234")}, varsayilan,
		beklenen{karar: DecisionReview, neden: "invalid_format", okuma: "34ABC12345", enIyiID: "c1", conf: 900})

	kontrol(t, "eşit güvende ID sırası", "34ABC123",
		[]Candidate{aday("b", "34ABC128"), aday("a", "34ABC129")},
		Thresholds{Auto: 1000, Review: 700, Margin: 0},
		beklenen{karar: DecisionReview, neden: "low_confidence_review", okuma: "34ABC123", enIyiID: "a", conf: 875,
			ikinciID: "b", ikinciConf: 875})

	kontrol(t, "aday yokken güven yok", "34ABC123", []Candidate{aday("c1", "34ABC12")},
		Thresholds{Auto: 1000, Review: 900, Margin: 100},
		beklenen{karar: DecisionNone, neden: "low_confidence", okuma: "34ABC123", enIyiID: "c1", conf: 875})
}

func TestMatchEsikSinirlari(t *testing.T) {
	read := "34ABC12"
	adaylar := []Candidate{aday("c1", "34ABC123")}

	kontrol(t, "best tam Auto", read, adaylar,
		Thresholds{Auto: 875, Review: 700, Margin: 100},
		beklenen{karar: DecisionAuto, neden: "auto", okuma: "34ABC12", enIyiID: "c1", conf: 875})

	kontrol(t, "best Auto-1", read, adaylar,
		Thresholds{Auto: 876, Review: 700, Margin: 100},
		beklenen{karar: DecisionReview, neden: "low_confidence_review", okuma: "34ABC12", enIyiID: "c1", conf: 875})

	kontrol(t, "best tam Review", read, adaylar,
		Thresholds{Auto: 1000, Review: 875, Margin: 100},
		beklenen{karar: DecisionReview, neden: "low_confidence_review", okuma: "34ABC12", enIyiID: "c1", conf: 875})

	kontrol(t, "best Review-1", read, adaylar,
		Thresholds{Auto: 1000, Review: 876, Margin: 100},
		beklenen{karar: DecisionNone, neden: "low_confidence", okuma: "34ABC12", enIyiID: "c1", conf: 875})

	marginAdaylar := []Candidate{aday("c1", "34ABC12E"), aday("c2", "34ABC125")}
	read2 := "34ABC123"

	kontrol(t, "fark tam Margin", read2, marginAdaylar,
		Thresholds{Auto: 900, Review: 700, Margin: 50},
		beklenen{karar: DecisionAuto, neden: "auto", okuma: "34ABC123", enIyiID: "c1", conf: 925,
			ikinciID: "c2", ikinciConf: 875})

	kontrol(t, "fark Margin-1", read2, marginAdaylar,
		Thresholds{Auto: 900, Review: 700, Margin: 51},
		beklenen{karar: DecisionReview, neden: "ambiguous", okuma: "34ABC123", enIyiID: "c1", conf: 925,
			ikinciID: "c2", ikinciConf: 875})
}

func TestMatchGecersizEsikler(t *testing.T) {
	adaylar := []Candidate{aday("c1", "34ABC123")}
	esikler := []Thresholds{
		{Auto: 900, Review: 950, Margin: 100},
		{Auto: 900, Review: -1, Margin: 100},
		{Auto: -1, Review: 700, Margin: 100},
		{Auto: 1001, Review: 700, Margin: 100},
		{Auto: 900, Review: 700, Margin: -1},
		{Auto: 900, Review: 700, Margin: 1001},
	}
	for _, esik := range esikler {
		if _, err := Match("34ABC123", adaylar, esik); err == nil {
			t.Fatalf("geçersiz eşik kabul edildi: %+v", esik)
		}
		if err := esik.Validate(); err == nil {
			t.Fatalf("Validate geçersiz eşiği kabul etti: %+v", esik)
		}
	}
	if err := DefaultThresholds.Validate(); err != nil {
		t.Fatalf("varsayılan eşikler geçersiz: %v", err)
	}
}

func TestMatchGercekciSenaryo(t *testing.T) {
	adaylar := []Candidate{aday("dogru", "34ABC123")}
	varsayilan := DefaultThresholds
	testler := []struct {
		okuma string
		karar Decision
		neden string
	}{
		{"34A8C123", DecisionReview, "invalid_format"},
		{"34ABCI23", DecisionReview, "invalid_format"},
		{"3AABC123", DecisionReview, "invalid_format"},
		{"34ABC12", DecisionReview, "low_confidence_review"},
		{"34 abc 123", DecisionExact, "exact"},
	}
	for _, tst := range testler {
		t.Run(tst.okuma, func(t *testing.T) {
			sonuc, err := Match(tst.okuma, adaylar, varsayilan)
			if err != nil {
				t.Fatalf("beklenmeyen hata: %v", err)
			}
			if sonuc.Decision != tst.karar || sonuc.Reason != tst.neden {
				t.Fatalf("%q: %s/%s, beklenen %s/%s", tst.okuma, sonuc.Decision, sonuc.Reason, tst.karar, tst.neden)
			}
			if sonuc.Best == nil || sonuc.Best.ID != "dogru" {
				t.Fatalf("%q: en iyi aday doldurulmalı", tst.okuma)
			}
		})
	}
}
