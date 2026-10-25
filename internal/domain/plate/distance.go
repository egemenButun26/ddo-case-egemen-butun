package plate

// Karışıklık tablosu: OCR'ın sık karıştırdığı çiftler ve tam işlem yerine
// ödenecek maliyet. Anahtarlar küçük rune önce gelecek şekilde sıralıdır.
var confusion = map[[2]rune]int{
	{'0', 'O'}: 20, // O-0
	{'0', 'D'}: 40, // D-0
	{'0', 'Q'}: 40, // Q-0
	{'1', 'I'}: 20, // I-1
	{'1', 'L'}: 50, // L-1
	{'1', 'T'}: 60, // T-1
	{'8', 'B'}: 20, // B-8
	{'5', 'S'}: 30, // S-5
	{'2', 'Z'}: 30, // Z-2
	{'6', 'G'}: 40, // G-6
	{'4', 'A'}: 60, // A-4
	{'3', 'E'}: 60, // E-3
	{'7', 'T'}: 50, // T-7
	{'U', 'V'}: 50, // U-V
	{'M', 'N'}: 60, // M-N
	{'H', 'N'}: 70, // H-N
}

// ConfusionCost iki karakterin birbirine yazılma maliyetidir; tabloda yoksa 100.
func ConfusionCost(x, y rune) int {
	if x == y {
		return 0
	}
	a, b := x, y
	if a > b {
		a, b = b, a
	}
	if c, ok := confusion[[2]rune{a, b}]; ok {
		return c
	}
	return 100
}

// Distance normalize edilmiş iki plaka arasındaki ağırlıklı Levenshtein
// mesafesini döndürür. Tam işlem 100 birimdir; karışıklık çiftleri daha ucuz.
func Distance(a, b string) int {
	ar := []rune(a)
	br := []rune(b)
	if len(ar) == 0 {
		return len(br) * 100
	}
	if len(br) == 0 {
		return len(ar) * 100
	}

	onceki := make([]int, len(br)+1)
	simdiki := make([]int, len(br)+1)
	for j := 0; j <= len(br); j++ {
		onceki[j] = j * 100
	}
	for i := 1; i <= len(ar); i++ {
		simdiki[0] = i * 100
		for j := 1; j <= len(br); j++ {
			yerine := onceki[j-1] + ConfusionCost(ar[i-1], br[j-1])
			silme := onceki[j] + 100
			ekleme := simdiki[j-1] + 100
			simdiki[j] = enKucuk(yerine, silme, ekleme)
		}
		onceki, simdiki = simdiki, onceki
	}
	return onceki[len(br)]
}

// Confidence binde (0..1000) güven skorudur. Girdiler normalize edilmiş olmalıdır.
func Confidence(a, b string) int {
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	if maxLen == 0 {
		return 0
	}
	conf := 1000 - (Distance(a, b)*10)/maxLen
	if conf < 0 {
		conf = 0
	}
	return conf
}

func enKucuk(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
