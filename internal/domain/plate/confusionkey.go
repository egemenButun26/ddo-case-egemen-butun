package plate

import "strings"

// confusionTemsilcileri her karisiklik kumesinin veritabaniyla ayni
// temsilcisini belirler. Kume uyeleri confusion tablosundan turetilir;
// bu liste yalnizca hangi uyenin temsilci olacagini secer.
var confusionTemsilcileri = []rune{'0', '1', '8', '5', '2', '6', '4', '3', 'U', 'M'}

var confusionAnahtari = confusionAnahtariKur()

func confusionAnahtariKur() map[rune]rune {
	parent := make(map[rune]rune)
	var kokBul func(rune) rune
	kokBul = func(x rune) rune {
		p, ok := parent[x]
		if !ok || p == x {
			parent[x] = x
			return x
		}
		k := kokBul(p)
		parent[x] = k
		return k
	}
	birlestir := func(a, b rune) {
		ra, rb := kokBul(a), kokBul(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	for cift := range confusion {
		birlestir(cift[0], cift[1])
	}

	kumeler := make(map[rune][]rune)
	for x := range parent {
		k := kokBul(x)
		kumeler[k] = append(kumeler[k], x)
	}

	anahtar := make(map[rune]rune)
	for _, uyeler := range kumeler {
		temsilci := temsilciSec(uyeler)
		for _, u := range uyeler {
			anahtar[u] = temsilci
		}
	}
	return anahtar
}

func temsilciSec(uyeler []rune) rune {
	for _, aday := range confusionTemsilcileri {
		for _, u := range uyeler {
			if u == aday {
				return aday
			}
		}
	}
	enKucuk := uyeler[0]
	for _, u := range uyeler[1:] {
		if u < enKucuk {
			enKucuk = u
		}
	}
	return enKucuk
}

// ConfusionKey plakadaki her karakteri karisiklik kumesinin temsilcisine
// cevirir; kumede olmayan karakterler oldugu gibi kalir. Veritabanindaki
// translate(plate, 'ODQILT7BSZGAEVHN', '0001111852643UMM') ile ayni sonucu verir.
func ConfusionKey(p string) string {
	var b strings.Builder
	for _, r := range p {
		if t, ok := confusionAnahtari[r]; ok {
			b.WriteRune(t)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
