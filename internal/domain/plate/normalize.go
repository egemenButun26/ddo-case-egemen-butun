package plate

import (
	"strings"
	"unicode"
)

// Normalize plakayı karşılaştırmaya hazırlar: Türkçe i/ı/İ harfleri I olur,
// kalan harfler büyütülür, A-Z ve 0-9 dışındaki her karakter atılır.
func Normalize(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch r {
		case 'i', 'ı', 'İ':
			b.WriteRune('I')
			continue
		}
		u := unicode.ToUpper(r)
		if (u >= 'A' && u <= 'Z') || (u >= '0' && u <= '9') {
			b.WriteRune(u)
		}
	}
	return b.String()
}

// ValidTR normalize edilmiş plakanın TR biçimine uyup uymadığını söyler:
// il kodu 01..81, 1-3 harf, 2-4 rakam.
func ValidTR(p string) bool {
	if len(p) < 5 || len(p) > 9 {
		return false
	}
	if !isRakam(p[0]) || !isRakam(p[1]) {
		return false
	}
	il := int(p[0]-'0')*10 + int(p[1]-'0')
	if il < 1 || il > 81 {
		return false
	}
	i := 2
	harf := 0
	for i < len(p) && isHarf(p[i]) {
		harf++
		i++
	}
	if harf < 1 || harf > 3 {
		return false
	}
	rakam := 0
	for i < len(p) && isRakam(p[i]) {
		rakam++
		i++
	}
	if i != len(p) {
		return false
	}
	return rakam >= 2 && rakam <= 4
}

func isRakam(c byte) bool {
	return c >= '0' && c <= '9'
}

func isHarf(c byte) bool {
	return c >= 'A' && c <= 'Z'
}
