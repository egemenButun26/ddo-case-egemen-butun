package plate

import "testing"

func TestConfusionKey(t *testing.T) {
	kumeler := []string{"0ODQ", "1ILT7", "8B", "5S", "2Z", "6G", "4A", "3E", "UV", "MNH"}
	for _, kume := range kumeler {
		temsilci := string([]rune(kume)[0])
		for _, r := range kume {
			if got := ConfusionKey(string(r)); got != temsilci {
				t.Fatalf("ConfusionKey(%q) = %q, beklenen %q", string(r), got, temsilci)
			}
		}
	}

	for cift := range confusion {
		if ConfusionKey(string(cift[0])) != ConfusionKey(string(cift[1])) {
			t.Fatalf("tablodaki %q/%q cifti ayni anahtara sahip degil", string(cift[0]), string(cift[1]))
		}
	}

	if got := ConfusionKey("34ABC123"); got != "3448C123" {
		t.Fatalf("ConfusionKey(34ABC123) = %q, beklenen 3448C123", got)
	}
	if ConfusionKey("34ABC123") != ConfusionKey("34A8C123") {
		t.Fatalf("A/4 ve B/8 anahtarlari farkli")
	}
	if ConfusionKey("34ABU123") != ConfusionKey("34ABV123") {
		t.Fatalf("U/V anahtarlari farkli")
	}
	if ConfusionKey("34HHH123") != ConfusionKey("34MMM123") {
		t.Fatalf("H/M anahtarlari farkli")
	}
	if ConfusionKey("06IK01") != ConfusionKey("061K07") {
		t.Fatalf("1/I/7 anahtarlari farkli")
	}
	if got := ConfusionKey(""); got != "" {
		t.Fatalf("bos plaka anahtari = %q, beklenen bos", got)
	}
	if got := ConfusionKey("XY9"); got != "XY9" {
		t.Fatalf("kumede olmayan karakterler degisti: %q", got)
	}
}
