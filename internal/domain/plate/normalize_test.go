package plate

import "testing"

func TestNormalize(t *testing.T) {
	testler := []struct {
		ad    string
		girdi string
		bekle string
	}{
		{"boş", "", ""},
		{"sadece boşluk", "   ", ""},
		{"boşluk tire nokta", " 34 abc-123. ", "34ABC123"},
		{"alt çizgi ve sekme", "\t34_ab.c9\n", "34ABC9"},
		{"küçük harf", "34abc123", "34ABC123"},
		{"türkçe i", "i", "I"},
		{"türkçe ı", "ı", "I"},
		{"türkçe İ", "İ", "I"},
		{"ı ş k", "06 ışk 01", "06IK01"},
		{"ş ç ğ atılır", "ŞÇĞ", ""},
		{"ö ü atılır", "ÖÜ", ""},
		{"emoji atılır", "34🚗ABC🚦123", "34ABC123"},
		{"diğer unicode atılır", "34\u00e9ABC", "34ABC"},
		{"zaten büyük", "34ABC123", "34ABC123"},
	}
	for _, tst := range testler {
		t.Run(tst.ad, func(t *testing.T) {
			if got := Normalize(tst.girdi); got != tst.bekle {
				t.Fatalf("Normalize(%q) = %q, beklenen %q", tst.girdi, got, tst.bekle)
			}
		})
	}
}

func TestValidTR(t *testing.T) {
	testler := []struct {
		plaka string
		bekle bool
	}{
		{"34ABC123", true},
		{"06A1234", true},
		{"00ABC12", false},
		{"82AB12", false},
		{"34ABCD12", false},
		{"34AB1", false},
		{"3OABC12", false},
		{"01ABC12", true},
		{"81ABC12", true},
		{"81ABC1234", true},
		{"34ABC1234", true},
		{"34AB123", true},
		{"34A12", true},
		{"34A123", true},
		{"34A1", false},
		{"6ABC123", false},
		{"34ABC12345", false},
		{"", false},
		{"34abc123", false},
	}
	for _, tst := range testler {
		t.Run(tst.plaka, func(t *testing.T) {
			if got := ValidTR(tst.plaka); got != tst.bekle {
				t.Fatalf("ValidTR(%q) = %v, beklenen %v", tst.plaka, got, tst.bekle)
			}
		})
	}
}
