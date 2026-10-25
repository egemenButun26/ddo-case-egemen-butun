package plate

import "testing"

func TestConfusionCost(t *testing.T) {
	ciftler := []struct {
		x, y    rune
		maliyet int
	}{
		{'O', '0', 20},
		{'D', '0', 40},
		{'Q', '0', 40},
		{'I', '1', 20},
		{'L', '1', 50},
		{'T', '1', 60},
		{'B', '8', 20},
		{'S', '5', 30},
		{'Z', '2', 30},
		{'G', '6', 40},
		{'A', '4', 60},
		{'E', '3', 60},
		{'T', '7', 50},
		{'U', 'V', 50},
		{'M', 'N', 60},
		{'H', 'N', 70},
	}
	for _, c := range ciftler {
		if got := ConfusionCost(c.x, c.y); got != c.maliyet {
			t.Fatalf("ConfusionCost(%q,%q) = %d, beklenen %d", c.x, c.y, got, c.maliyet)
		}
		if got := ConfusionCost(c.y, c.x); got != c.maliyet {
			t.Fatalf("ConfusionCost(%q,%q) = %d, beklenen %d (simetri)", c.y, c.x, got, c.maliyet)
		}
	}
	if got := ConfusionCost('A', 'A'); got != 0 {
		t.Fatalf("aynı karakter maliyeti %d, beklenen 0", got)
	}
	if got := ConfusionCost('A', 'B'); got != 100 {
		t.Fatalf("tablo dışı çift maliyeti %d, beklenen 100", got)
	}
	if got := ConfusionCost('O', 'D'); got != 100 {
		t.Fatalf("tabloda olmayan çift 100 olmalı, gelen %d", got)
	}
}

func TestDistance(t *testing.T) {
	testler := []struct {
		ad    string
		a, b  string
		bekle int
	}{
		{"eşit", "34ABC123", "34ABC123", 0},
		{"boş eşit", "", "", 0},
		{"O/0", "34ABC12O", "34ABC120", 20},
		{"B/8", "34A8C123", "34ABC123", 20},
		{"8/B", "34ABC123", "34A8C123", 20},
		{"tek eksik karakter", "34ABC123", "34ABC12", 100},
		{"tek fazla karakter", "34ABC12", "34ABC123", 100},
		{"boş ile ABC", "", "ABC", 300},
		{"ABC ile boş", "ABC", "", 300},
	}
	for _, tst := range testler {
		t.Run(tst.ad, func(t *testing.T) {
			if got := Distance(tst.a, tst.b); got != tst.bekle {
				t.Fatalf("Distance(%q,%q) = %d, beklenen %d", tst.a, tst.b, got, tst.bekle)
			}
			if got := Distance(tst.b, tst.a); got != tst.bekle {
				t.Fatalf("Distance(%q,%q) = %d, beklenen %d", tst.b, tst.a, got, tst.bekle)
			}
		})
	}

	if got := Distance("34ABC123", "99XYZ987"); got <= 500 {
		t.Fatalf("tamamen farklı plakalar için mesafe büyük olmalı, gelen %d", got)
	}

	simetri := [][2]string{
		{"34ABC123", "34A8C123"},
		{"06IK01", "06I K 01"},
		{"34ABC123", "34ABC12"},
		{"34AB8123", "34ABBI23"},
		{"O0I1B8", "0OI18B"},
	}
	for _, s := range simetri {
		if Distance(s[0], s[1]) != Distance(s[1], s[0]) {
			t.Fatalf("simetri bozuk: %q / %q", s[0], s[1])
		}
	}
}

func TestDistanceKarışıklıkUcuz(t *testing.T) {
	// B/8 karışıklığı normal karakter değişiminden daha ucuz olmalı.
	if Distance("B", "8") >= Distance("B", "C") {
		t.Fatalf("B/8 maliyeti normal değişimden ucuz değil")
	}
	// O/0 da aynı şekilde.
	if Distance("O", "0") >= Distance("O", "Q") {
		t.Fatalf("O/0 maliyeti normal değişimden ucuz değil")
	}
}

func TestConfidence(t *testing.T) {
	testler := []struct {
		a, b  string
		bekle int
	}{
		{"34ABC123", "34A8C123", 975},
		{"34ABC12", "34ABC123", 875},
		{"34ABC123", "34ABC123", 1000},
		{"34AB8123", "34ABB123", 975},
		{"34ABC12O", "34ABC120", 975},
		{"", "", 0},
		{"A", "ZZZZZZZZ", 0},
	}
	for _, tst := range testler {
		if got := Confidence(tst.a, tst.b); got != tst.bekle {
			t.Fatalf("Confidence(%q,%q) = %d, beklenen %d", tst.a, tst.b, got, tst.bekle)
		}
	}
}

func TestConfidenceDortKarışıklık(t *testing.T) {
	// Dört ayrı 20'lik karışıklık (toplam 80), maxLen 8 -> 900.
	a := "O0I1ABCD"
	b := "0O1IABCD"
	if got := Distance(a, b); got != 80 {
		t.Fatalf("dört karışıklık mesafesi %d, beklenen 80", got)
	}
	if got := Confidence(a, b); got != 900 {
		t.Fatalf("dört karışıklık güveni %d, beklenen 900", got)
	}
}
