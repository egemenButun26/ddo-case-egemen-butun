package tariff

import (
	"fmt"
	"math"
	"time"

	// Gomulu saat dilimi veritabani: distroless imajda da Europe/Istanbul calissin.
	_ "time/tzdata"
)

const maxOturumGun = 400

// DayCharge: tek takvim gununun ucret dokumu.
type DayCharge struct {
	Date         Date
	RuleName     string
	DayMinutes   int64 // bilgi amacli, yukari yuvarlanmis dakika
	DaySeconds   int64
	NightSeconds int64
	DayAmount    Kurus
	NightAmount  Kurus
	Capped       bool
	Amount       Kurus
}

// Quote: bir oturumun tam ucret dokumu.
type Quote struct {
	VersionID       string
	Entry           time.Time
	Exit            time.Time
	DurationSeconds int64
	Free            bool
	Days            []DayCharge
	Total           Kurus
}

// Calculate: secilmis surumle oturumu hesaplar. Surum secmez, disaridan verilir.
func Calculate(v Version, entry, exit time.Time) (Quote, error) {
	if err := v.Validate(); err != nil {
		return Quote{}, err
	}

	q := Quote{VersionID: v.ID, Entry: entry, Exit: exit}
	if exit.Before(entry) {
		return Quote{}, ErrNegativeDuration
	}
	sure := exit.Sub(entry)
	if sure > maxOturumGun*24*time.Hour {
		return Quote{}, ErrTooLong
	}
	q.DurationSeconds = int64(sure / time.Second)

	ucretsizSaniye := int64(v.FreeMinutes) * 60
	if q.DurationSeconds <= ucretsizSaniye {
		q.Free = true
		return q, nil
	}

	imlec := entry
	for imlec.Before(exit) {
		yerel := imlec.In(v.Location)
		gun := Date{Year: yerel.Year(), Month: yerel.Month(), Day: yerel.Day()}
		geceYarisi := time.Date(gun.Year, gun.Month, gun.Day+1, 0, 0, 0, 0, v.Location)
		if !geceYarisi.After(imlec) {
			return Quote{}, fmt.Errorf("tarife %q: gun siniri ilerlemedi (%d-%02d-%02d)", v.ID, gun.Year, gun.Month, gun.Day)
		}
		parcaSonu := exit
		if geceYarisi.Before(exit) {
			parcaSonu = geceYarisi
		}

		dc, err := gunHesapla(v, gun, yerel.Weekday(), imlec, parcaSonu)
		if err != nil {
			return Quote{}, err
		}
		q.Days = append(q.Days, dc)
		toplam, err := addKurus(q.Total, dc.Amount)
		if err != nil {
			return Quote{}, err
		}
		q.Total = toplam
		imlec = parcaSonu
	}
	return q, nil
}

func gunHesapla(v Version, gun Date, haftaGunu time.Weekday, bas, son time.Time) (DayCharge, error) {
	kural, ad := kuralSec(v, gun, haftaGunu)

	toplamSaniye := int64(son.Sub(bas) / time.Second)
	var geceSaniye int64
	if n := kural.Night; n != nil {
		geceSaniye = geceSaniyeleri(gun, v.Location, n, bas, son)
	}
	gunduzSaniye := toplamSaniye - geceSaniye

	gunduzTutar, err := gunduzTutari(kural.Bands, gunduzSaniye)
	if err != nil {
		return DayCharge{}, err
	}
	var geceTutar Kurus
	if n := kural.Night; n != nil {
		geceTutar, err = birimTutari(geceSaniye, int64(n.UnitMinutes)*60, n.UnitPrice)
		if err != nil {
			return DayCharge{}, err
		}
	}

	tutar, err := addKurus(gunduzTutar, geceTutar)
	if err != nil {
		return DayCharge{}, err
	}
	tavanlandi := false
	if kural.DailyCap > 0 && tutar > kural.DailyCap {
		tutar = kural.DailyCap
		tavanlandi = true
	}

	return DayCharge{
		Date:         gun,
		RuleName:     ad,
		DayMinutes:   (toplamSaniye + 59) / 60,
		DaySeconds:   gunduzSaniye,
		NightSeconds: geceSaniye,
		DayAmount:    gunduzTutar,
		NightAmount:  geceTutar,
		Capped:       tavanlandi,
		Amount:       tutar,
	}, nil
}

// kuralSec: etkinlik > hafta sonu > hafta ici sirasi.
func kuralSec(v Version, gun Date, haftaGunu time.Weekday) (DayRule, string) {
	for _, e := range v.Events {
		if e.Date == gun {
			ad := e.Name
			if ad == "" {
				ad = e.Rule.Name
			}
			return e.Rule, ad
		}
	}
	if haftaGunu == time.Saturday || haftaGunu == time.Sunday {
		if v.Weekend != nil {
			return *v.Weekend, v.Weekend.Name
		}
	}
	return v.Weekday, v.Weekday.Name
}

// geceSaniyeleri: gece penceresinin gun parcasinda kalan saniyeleri.
func geceSaniyeleri(gun Date, konum *time.Location, n *Night, bas, son time.Time) int64 {
	gunBas := time.Date(gun.Year, gun.Month, gun.Day, 0, 0, 0, 0, konum)
	gunSonu := time.Date(gun.Year, gun.Month, gun.Day+1, 0, 0, 0, 0, konum)

	var pencereler [][2]time.Time
	if n.StartMinute > n.EndMinute {
		// Pencere gece yarisini sarar: [00:00, End) ve [Start, 24:00).
		pencereler = append(pencereler,
			[2]time.Time{gunBas, time.Date(gun.Year, gun.Month, gun.Day, 0, n.EndMinute, 0, 0, konum)},
			[2]time.Time{time.Date(gun.Year, gun.Month, gun.Day, 0, n.StartMinute, 0, 0, konum), gunSonu},
		)
	} else {
		pencereler = append(pencereler, [2]time.Time{
			time.Date(gun.Year, gun.Month, gun.Day, 0, n.StartMinute, 0, 0, konum),
			time.Date(gun.Year, gun.Month, gun.Day, 0, n.EndMinute, 0, 0, konum),
		})
	}

	var toplam time.Duration
	for _, p := range pencereler {
		a, b := p[0], p[1]
		if a.Before(bas) {
			a = bas
		}
		if b.After(son) {
			b = son
		}
		if b.After(a) {
			toplam += b.Sub(a)
		}
	}
	return int64(toplam / time.Second)
}

// gunduzTutari: gunun gunduz saniyelerini dilimlere sirayla uygular.
func gunduzTutari(bands []Band, saniye int64) (Kurus, error) {
	var toplam Kurus
	var onceki int64
	for _, b := range bands {
		if saniye <= onceki {
			break
		}
		var sinir int64 = math.MaxInt64
		if b.UpToMinutes != 0 {
			sinir = int64(b.UpToMinutes) * 60
		}
		pay := saniye
		if pay > sinir {
			pay = sinir
		}
		pay -= onceki
		if pay > 0 {
			tutar, err := birimTutari(pay, int64(b.UnitMinutes)*60, b.UnitPrice)
			if err != nil {
				return 0, err
			}
			toplam, err = addKurus(toplam, tutar)
			if err != nil {
				return 0, err
			}
		}
		if b.UpToMinutes == 0 {
			break
		}
		onceki = sinir
	}
	return toplam, nil
}

// birimTutari: her baslayan birim icin ucret.
func birimTutari(saniye, birimSaniye int64, fiyat Kurus) (Kurus, error) {
	if saniye <= 0 {
		return 0, nil
	}
	if birimSaniye <= 0 {
		return 0, fmt.Errorf("tarife: birim saniyesi gecersiz")
	}
	birim := (saniye + birimSaniye - 1) / birimSaniye
	return mulKurus(birim, fiyat)
}
